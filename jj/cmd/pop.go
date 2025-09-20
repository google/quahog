// Copyright 2025 Google LLC
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
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
)

var Pop = &cobra.Command{
	Use:   "pop",
	Short: "Pop quilt patches into jj commits",
	Long: `Pop converts quilt patch files into jj commits with [PATCH] descriptions.

The pop operation:
1. Reads patch files from the end of the series file
2. Applies each patch in reverse to remove its changes
3. Creates new commits with [PATCH] descriptions
4. Removes patch files and updates the series file`,
	RunE: runPop,
}

var (
	popRoot  string
	popFrom  string
	popCount int
	popAll   bool
)

func init() {
	Root.AddCommand(Pop)

	Pop.Flags().StringVar(&popRoot, "root", "", "directory containing patches/ subdirectory")
	Pop.Flags().StringVar(&popFrom, "from", "", "base commit to pop from")
	Pop.Flags().IntVar(&popCount, "count", 1, "number of patches to pop")
	Pop.Flags().BoolVar(&popAll, "all", false, "pop all patches")

	Pop.MarkFlagRequired("root")
}

func runPop(cmd *cobra.Command, args []string) (err error) {
	ctx := cmd.Context()
	// Resolve root path
	rootUserpath := filepath.Clean(popRoot)
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
	if popFrom != "" {
		popFromRev, err = jj.Rev(ctx, popFrom)
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
		patches, err := patchManager.GetPatchesToPop(popCount, popAll)
		if err != nil {
			return fmt.Errorf("failed to get patches to pop: %w", err)
		}
		if len(patches) == 0 {
			fmt.Fprintf(cmd.OutOrStderr(), "No patches to pop")
			return nil
		}
		fmt.Fprintf(cmd.OutOrStderr(), "Popping %d patch%s from \"%s\"\n", len(patches), pluralize(patches, "es"), rootUserpath)
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
			fmt.Fprintf(cmd.OutOrStderr(), "Popping patch \"%s\"\n", patchInfo.Name)
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
		fmt.Fprintf(cmd.OutOrStderr(), "Successfully popped %d patch%s\n", len(patches), pluralize(patches, "es"))
		return nil
	}()
	if err != nil {
		fmt.Fprint(cmd.OutOrStderr(), "encountered error. rolling back... ")
		if _, rollbackErr := jj.Run(ctx, "op", "restore", baseOp); rollbackErr != nil {
			fmt.Fprintf(cmd.OutOrStderr(), "failed: %v\n", rollbackErr)
		} else {
			fmt.Fprintln(cmd.OutOrStderr(), "done")
		}
	}
	return err
}
