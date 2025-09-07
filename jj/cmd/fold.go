// Copyright 2025 Google LLC
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/google/quahog/jj/internal/jjvcs"
	"github.com/google/quahog/jj/internal/quahog"
	"github.com/google/quahog/jj/internal/quilt"
	"github.com/spf13/cobra"
)

var Fold = &cobra.Command{
	Use:   "fold",
	Short: "Fold jj commits into quilt patches",
	Long: `Fold converts jj commits with [PATCH] descriptions into quilt patch files.

The fold operation:
1. Identifies commits to fold based on [PATCH] descriptions
2. Generates diff for each commit 
3. Creates patch files in patches/ directory
4. Updates the series file
5. Squashes patch commits into the base commit`,
	RunE: runFold,
}

var (
	foldRoot  string
	foldTo    string
	foldCount int
	foldAll   bool
	foldRev   string
)

func init() {
	Root.AddCommand(Fold)

	Fold.Flags().StringVar(&foldRoot, "root", "", "directory containing patches/ subdirectory")
	Fold.Flags().StringVar(&foldTo, "to", "", "base commit to fold into")
	Fold.Flags().IntVar(&foldCount, "count", 1, "number of patches to fold")
	Fold.Flags().BoolVar(&foldAll, "all", false, "fold all patches")
	Fold.Flags().StringVar(&foldRev, "rev", "", "specific revisions to fold")

	Fold.MarkFlagRequired("root")
}

func runFold(cmd *cobra.Command, args []string) error {
	// Resolve root path
	rootUserpath := filepath.Clean(foldRoot)
	rootAbspath, err := filepath.Abs(rootUserpath)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path for %s: %w", rootUserpath, err)
	}
	if _, err := os.Stat(rootAbspath); err != nil {
		return err
	}
	// Verify patches directory exists
	patchesDir := filepath.Join(rootAbspath, "patches")
	if _, err := os.Stat(patchesDir); os.IsNotExist(err) {
		return fmt.Errorf("%s: does not contain patches/ subdirectory", rootAbspath)
	}
	jj := jjvcs.NewClient()
	repoRoot, err := jj.Root()
	if err != nil {
		return err
	}
	rootRelRepo, err := filepath.Rel(repoRoot, rootAbspath)
	if err != nil {
		return fmt.Errorf("failed to determine repo relative path: %w", err)
	}
	baseOp, err := jj.Run("op", "log", "--template", "id", "--limit", "1", "--no-graph")
	if err != nil {
		return fmt.Errorf("failed to determine base op: %w", err)
	}
	baseOp = strings.TrimSpace(baseOp)
	var originRev *jjvcs.Change
	var originChild bool
	if originRevs, err := jj.Revs("@|@-"); err != nil {
		return err
	} else if !originRevs[0].IsEmpty || len(originRevs[0].Parents) != 1 {
		originRev = originRevs[0]
	} else {
		originRev = originRevs[1]
		originChild = true
	}
	var foldToRev *jjvcs.Change
	if foldTo != "" {
		foldToRev, err = jj.Rev(foldTo)
		if err != nil {
			return err
		}
		if !quahog.IsBase(foldToRev) {
			return fmt.Errorf("--to commit %s is not a valid base commit (must contain QUAHOG marker)", foldToRev.ID)
		}
	}
	var foldRevs []*jjvcs.Change
	if foldRev != "" {
		foldRevs, err = jj.Revs(foldRev)
		if err != nil {
			return err
		}
		if len(foldRevs) == 0 {
			return fmt.Errorf("revset empty: %s", foldRev)
		}
	}
	// Prefer the origin revision closest to the base for building the chain
	var rev string
	if foldTo != "" {
		rev = foldToRev.ID
	} else if foldRev != "" {
		rev = foldRevs[0].ID
	} else {
		rev = originRev.ID
	}
	err = func() error {
		chain, err := quahog.NewPatchChain(jj, quahog.ChainOptions{OriginRev: rev, RootRelpath: rootRelRepo})
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
		} else if foldAll {
			toFold = len(chain.Patches)
		} else if foldCount > 0 {
			if foldCount > len(chain.Patches) {
				return fmt.Errorf("--count %d greater than patch chain length %d", foldCount, len(chain.Patches))
			}
			toFold = foldCount
		} else {
			fmt.Fprintln(cmd.OutOrStderr(), "No patches to fold")
			return nil
		}
		commits := chain.Patches[:toFold]
		fmt.Fprintf(cmd.OutOrStderr(), "Folding %d patch%s into \"%s\"\n", len(commits), pluralize(commits, "es"), rootUserpath)
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
				fmt.Fprintf(cmd.OutOrStderr(), "warning: patch %s is empty. excluding from series\n", name)
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
			content, err := quahog.PatchContent(jj, commit, rootAbspath, rootRelRepo)
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
		if err := jj.Squash(commitIDs, chain.Base.ID); err != nil {
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
		if _, err := jj.Run(restoreArgs...); err != nil {
			return fmt.Errorf("failed to restore commit position: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStderr(), "Successfully folded %d patch%s\n", len(commits), pluralize(commits, "es"))
		return nil
	}()
	if err != nil {
		fmt.Fprint(cmd.OutOrStderr(), "encountered error. rolling back... ")
		if _, rollbackErr := jj.Run("op", "restore", baseOp); rollbackErr != nil {
			fmt.Fprintf(cmd.OutOrStderr(), "failed: %v\n", rollbackErr)
		} else {
			fmt.Fprintln(cmd.OutOrStderr(), "done")
		}
	}
	return err
}
