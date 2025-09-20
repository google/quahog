// Copyright 2025 Google LLC
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/google/quahog/jj/internal/jjvcs"
	"github.com/google/quahog/jj/internal/quahog"
	"github.com/google/quahog/jj/internal/quilt"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var foldCommand = &cobra.Command{
	Use:   "fold",
	Short: "Fold jj commits into quilt patches",
	Long: `Fold converts jj commits with [PATCH] descriptions into quilt patch files.

The fold operation:
1. Identifies commits to fold based on [PATCH] descriptions
2. Generates diff for each commit 
3. Creates patch files in patches/ directory
4. Updates the series file
5. Squashes patch commits into the base commit`,
}

func Fold(globalCfg *GlobalConfig) *cobra.Command {
	cfg := FoldConfig{GlobalConfig: globalCfg}
	cmd := *foldCommand
	cmd.Flags().AddFlagSet(foldFlags(foldCommand.Name(), &cfg))
	cmd.MarkFlagRequired("root")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		cio := IO{Out: cmd.OutOrStdout(), Err: cmd.OutOrStderr()}
		return runFold(cmd.Context(), cio, cfg)
	}
	return &cmd
}

func foldFlags(name string, cfg *FoldConfig) *pflag.FlagSet {
	set := pflag.NewFlagSet(name, pflag.ContinueOnError)
	set.StringVar(&cfg.Root, "root", "", "directory containing patches/ subdirectory")
	set.StringVar(&cfg.To, "to", "", "base commit to fold into")
	set.IntVar(&cfg.Count, "count", 1, "number of patches to fold")
	set.BoolVar(&cfg.All, "all", false, "fold all patches")
	set.StringVar(&cfg.Rev, "rev", "", "specific revisions to fold")
	return set
}

type FoldConfig struct {
	*GlobalConfig
	Root  string
	To    string
	Count int
	All   bool
	Rev   string
}

func runFold(ctx context.Context, cio IO, cfg FoldConfig) error {
	// Resolve repo path
	jj := jjvcs.NewClient(cfg.Repository)
	repoRoot, err := jj.Root(ctx)
	if err != nil {
		return err
	}
	// Resolve root path
	rootUserpath := filepath.Clean(cfg.Root)
	rootAbspath, err := filepath.Abs(rootUserpath)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path for %s: %w", rootUserpath, err)
	}
	if _, err := os.Stat(rootAbspath); err != nil {
		return err
	}
	// Verify patches directory exists
	patchesDir := filepath.Join(rootAbspath, "patches")
	if _, err := os.Stat(patchesDir); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s: does not contain patches/ subdirectory", rootAbspath)
	}
	rootRelRepo, err := filepath.Rel(repoRoot, rootAbspath)
	if err != nil {
		return fmt.Errorf("failed to determine repo relative path: %w", err)
	}
	baseOp, err := jj.Run(ctx, "op", "log", "--template", "id", "--limit", "1", "--no-graph")
	if err != nil {
		return fmt.Errorf("failed to determine base op: %w", err)
	}
	baseOp = strings.TrimSpace(baseOp)
	var originRev *jjvcs.Change
	var originChild bool
	if originRevs, err := jj.Revs(ctx, "@|@-"); err != nil {
		return err
	} else if !originRevs[0].IsEmpty || len(originRevs[0].Parents) != 1 {
		originRev = originRevs[0]
	} else {
		originRev = originRevs[1]
		originChild = true
	}
	var foldToRev *jjvcs.Change
	if cfg.To != "" {
		foldToRev, err = jj.Rev(ctx, cfg.To)
		if err != nil {
			return err
		}
		if !quahog.IsBase(foldToRev) {
			return fmt.Errorf("--to commit %s is not a valid base commit (must contain QUAHOG marker)", foldToRev.ID)
		}
	}
	var foldRevs []*jjvcs.Change
	if cfg.Rev != "" {
		foldRevs, err = jj.Revs(ctx, cfg.Rev)
		if err != nil {
			return err
		}
		if len(foldRevs) == 0 {
			return fmt.Errorf("revset empty: %s", cfg.Rev)
		}
	}
	// Prefer the origin revision closest to the base for building the chain
	var rev string
	if cfg.To != "" {
		rev = foldToRev.ID
	} else if cfg.Rev != "" {
		rev = foldRevs[0].ID
	} else {
		rev = originRev.ID
	}
	err = func() error {
		chain, err := quahog.NewPatchChain(ctx, jj, quahog.ChainOptions{OriginRev: rev, RootRelpath: rootRelRepo})
		if err != nil {
			return fmt.Errorf("failed to build patch chain: %w", err)
		}
		if foldToRev != nil && foldToRev.ID != chain.Base.ID {
			return fmt.Errorf("unexpected patch chain base: %s", chain.Base.ID)
		}
		var toFold int
		if len(foldRevs) > 0 {
			if len(foldRevs) > len(chain.Patches) {
				return fmt.Errorf("--rev length of %d greater than patch chain length %d", len(foldRevs), len(chain.Patches))
			}
			if !slices.EqualFunc(foldRevs, chain.Patches[:len(foldRevs)], func(l, r *jjvcs.Change) bool { return l.ID == r.ID }) {
				return fmt.Errorf("--rev commits not found at start of patch chain")
			}
			toFold = len(foldRevs)
		} else if cfg.All {
			toFold = len(chain.Patches)
		} else if cfg.Count > 0 {
			if cfg.Count > len(chain.Patches) {
				return fmt.Errorf("--count %d greater than patch chain length %d", cfg.Count, len(chain.Patches))
			}
			toFold = cfg.Count
		} else {
			fmt.Fprintln(cio.Err, "No patches to fold")
			return nil
		}
		commits := chain.Patches[:toFold]
		fmt.Fprintf(cio.Err, "Folding %d patch%s into \"%s\"\n", len(commits), pluralize(commits, "es"), rootRelRepo)
		// Generate patches from commits
		patchManager := quilt.NewManager(rootAbspath)
		var patchNames, patchContent []string
		for _, commit := range commits {
			name, _, err := quahog.PatchMetadata(commit)
			if err != nil {
				return fmt.Errorf("parsing commit metadata: %w ", err)
			}
			// Validate patch commit
			if commit.IsConflicted {
				return fmt.Errorf("patch commit for %s is conflicted", name)
			}
			if commit.IsDivergent {
				return fmt.Errorf("patch commit for %s is divergent", name)
			}
			if commit.IsEmpty {
				fmt.Fprintf(cio.Err, "warning: patch %s is empty. excluding from series\n", name)
				continue // Skip this commit as a no-op
			}
			// TODO: This is overly-pessimistic but prevents a tricky bug that needs to be properly handled.
			// If a patch has multipl parents, there may be other modifications to
			// the patched files merged in. Since the patch isn't in conflict, they
			// are disjoint changes BUT they can still impact the diff chunk locators
			// which would result in irreversible patch files. To avoid this, we
			// restrict any multi-parent patches.
			if len(commit.Parents) > 1 {
				return fmt.Errorf("[unimplemented] patch commit for %s has multiple parents", name)
			}
			content, err := quahog.PatchContent(ctx, jj, commit, rootAbspath, rootRelRepo)
			if err != nil {
				return fmt.Errorf("generating patch for %s: %w", name, err)
			}
			patchNames = append(patchNames, name)
			patchContent = append(patchContent, content)
		}
		// Write patch files and update series
		if err := patchManager.WritePatchFiles(patchNames, patchContent); err != nil {
			return fmt.Errorf("failed to write patch files: %w", err)
		}
		// Squash commits into the target base commit
		var commitIDs []string
		for _, commit := range commits {
			commitIDs = append(commitIDs, commit.ID)
		}
		if err := jj.Squash(ctx, commitIDs, chain.Base.ID); err != nil {
			return fmt.Errorf("failed to squash commits: %w", err)
		}
		// Restore commit position
		var restoreArgs []string
		if slices.Contains(commitIDs, originRev.ID) {
			restoreArgs = []string{"new", chain.Base.ID}
		} else if originChild {
			restoreArgs = []string{"new", originRev.ID}
		} else {
			restoreArgs = []string{"edit", originRev.ID}
		}
		if _, err := jj.Run(ctx, restoreArgs...); err != nil {
			return fmt.Errorf("failed to restore commit position: %w", err)
		}
		fmt.Fprintf(cio.Err, "Successfully folded %d patch%s\n", len(commits), pluralize(commits, "es"))
		return nil
	}()
	if err != nil {
		fmt.Fprint(cio.Err, "encountered error. rolling back... ")
		if _, rollbackErr := jj.Run(ctx, "op", "restore", baseOp); rollbackErr != nil {
			fmt.Fprintf(cio.Err, "failed: %v\n", rollbackErr)
		} else {
			fmt.Fprintln(cio.Err, "done")
		}
	}
	return err
}
