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

var popCommand = &cobra.Command{
	Use:   "pop",
	Short: "Pop quilt patches into jj commits",
	Long: `Pop converts quilt patch files into jj commits with [PATCH] descriptions.

The pop operation:
1. Reads patch files from the end of the series file
2. Applies each patch in reverse to remove its changes
3. Creates new commits with [PATCH] descriptions
4. Removes patch files and updates the series file`,
}

// PopConfig holds the configuration for the pop command
type PopConfig struct {
	Root  string
	From  string
	Count int
	All   bool
}

// Pop creates a new cobra.Command for the pop operation
func Pop() *cobra.Command {
	var cfg PopConfig
	cmd := *popCommand
	cmd.Flags().AddFlagSet(popFlags(cmd.Name(), &cfg))
	cmd.MarkFlagRequired("root")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		cio := IO{Out: cmd.OutOrStdout(), Err: cmd.OutOrStderr()}
		return runPop(cmd.Context(), cio, cfg)
	}
	return &cmd
}

// popFlags creates a new FlagSet for the pop command
func popFlags(name string, cfg *PopConfig) *pflag.FlagSet {
	set := pflag.NewFlagSet(name, pflag.ContinueOnError)
	set.StringVar(&cfg.Root, "root", "", "directory containing patches/ subdirectory")
	set.StringVar(&cfg.From, "from", "", "base commit to pop from")
	set.IntVar(&cfg.Count, "count", 1, "number of patches to pop")
	set.BoolVar(&cfg.All, "all", false, "pop all patches")
	return set
}

func runPop(ctx context.Context, cio IO, cfg PopConfig) (err error) {
	// Resolve root path
	rootUserpath := filepath.Clean(cfg.Root)
	rootAbspath, _ := filepath.Abs(rootUserpath)
	if _, err := os.Stat(rootAbspath); err != nil {
		return err
	}
	// Verify patches directory exists
	patchesDir := filepath.Join(rootAbspath, "patches")
	if _, err := os.Stat(patchesDir); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s: does not contain patches/ subdirectory", rootAbspath)
	}
	seriesFile := filepath.Join(patchesDir, "series")
	if _, err := os.Stat(seriesFile); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s: no such file", seriesFile)
	}
	jj := jjvcs.NewClient()
	repoRoot, err := jj.Root(ctx)
	if err != nil {
		return err
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
	patchManager := quilt.NewManager(rootAbspath)
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
	var popFromRev *jjvcs.Change
	if cfg.From != "" {
		popFromRev, err = jj.Rev(ctx, cfg.From)
		if err != nil {
			return err
		}
		if !quahog.IsBase(popFromRev) {
			return fmt.Errorf("--from commit %s is not a valid base commit (must contain QUAHOG marker)", popFromRev.ID)
		}
	}
	var rev string
	if popFromRev != nil {
		rev = popFromRev.ID
	} else {
		rev = originRev.ID
	}
	// Must rollback from this point forward
	err = func() error {
		// TODO: We should be able to non-desctructively construct patch chain i.e. make base commit separately.
		chain, err := quahog.NewPatchChain(ctx, jj, quahog.ChainOptions{OriginRev: rev, RootRelpath: rootRelRepo})
		if err != nil {
			return fmt.Errorf("failed to build patch chain: %w", err)
		}
		if popFromRev != nil && popFromRev.ID != chain.Base.ID {
			return fmt.Errorf("unexpected patch chain base: %s", chain.Base.ID)
		}
		// Get patches to pop
		patches, err := patchManager.GetPatchesToPop(cfg.Count, cfg.All)
		if err != nil {
			return fmt.Errorf("failed to get patches to pop: %w", err)
		}
		if len(patches) == 0 {
			fmt.Fprintln(cio.Err, "No patches to pop")
			return nil
		}
		fmt.Fprintf(cio.Err, "Popping %d patch%s from \"%s\"\n", len(patches), pluralize(patches, "es"), rootUserpath)
		// Process each patch in reverse order (last patch first)
		patchContent := make([]string, len(patches))
		patchDescription := make([]string, len(patches))
		for i, patchInfo := range slices.Backward(patches) {
			patchContent[i], patchDescription[i], err = patchManager.ReadPatch(patchInfo.Name)
			if err != nil {
				return fmt.Errorf("failed to read patch %s: %w", patchInfo.Name, err)
			}
			if err := quilt.ApplyPatchReverse(patchContent[i], rootAbspath); err != nil {
				return fmt.Errorf("failed to reverse patch %s: %w", patchInfo.Name, err)
			}
			if err := patchManager.RemovePatch(patchInfo.Name); err != nil {
				return fmt.Errorf("failed to remove patch %s: %w", patchInfo.Name, err)
			}
			fmt.Fprintf(cio.Err, "Popping patch \"%s\"\n", patchInfo.Name)
		}
		_, err = jj.Run(ctx, "new", chain.Base.ID)
		if err != nil {
			return err
		}
		{
			// Move working copy change before the patch so subsequnt patches are created at the start of the chain.
			var workingCopy *jjvcs.Change
			if workingCopy, err = jj.Rev(ctx, "@"); err != nil {
				return err
			}
			if len(chain.Patches) > 0 {
				_, err = jj.Run(ctx, "rebase", "-r", workingCopy.ID, "--insert-before", chain.Patches[0].ID)
				if err != nil {
					return err
				}
			}
		}
		for i, patchInfo := range patches {
			// Apply patch in reverse to remove changes
			if err = quilt.ApplyPatch(patchContent[i], rootAbspath); err != nil {
				return fmt.Errorf("failed to apply patch %s: %w", patchInfo.Name, err)
			}
			commitMsg := fmt.Sprintf("[PATCH] %s", patchInfo.Name)
			if description := patchDescription[i]; description != "" {
				commitMsg += "\n\n" + description
			}
			_, err = jj.Run(ctx, "commit", "--message", commitMsg)
			if err != nil {
				return fmt.Errorf("failed to commit patch %s: %w", patchInfo.Name, err)
			}
		}
		// Restore commit position
		var restoreArgs []string
		if originRev.ID == chain.Base.ID && originChild {
			restoreArgs = []string{"edit", "@"} // cursor should be in the correct spot already
		} else if originChild {
			restoreArgs = []string{"new", originRev.ID}
		} else {
			restoreArgs = []string{"edit", originRev.ID}
		}
		if _, err := jj.Run(ctx, restoreArgs...); err != nil {
			return fmt.Errorf("failed to restore commit position: %w", err)
		}
		fmt.Fprintf(cio.Err, "Successfully popped %d patch%s\n", len(patches), pluralize(patches, "es"))
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
