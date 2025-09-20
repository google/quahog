// Copyright 2025 Google LLC
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"github.com/spf13/cobra"
)

// GlobalConfig holds configuration set by persistent flags on the Root command
type GlobalConfig struct {
	Repository string
}

var root = &cobra.Command{
	Use:   "quahog",
	Short: "Quahog - Quilt-style patch management for Jujutsu",
	Long: `Quahog enables managing Quilt-style patch sets using the Jujutsu version control system.

Quahog models each patch as a commit which allows editing, rebasing, reordering,
and all the other operations that Jujutsu supports.

Key features:
- Convert jj commits into quilt patches (fold)
- Convert quilt patches into jj commits (pop)
- Rebase patch sets onto new versions`,
}

func Root() *cobra.Command {
	var globalCfg GlobalConfig
	cmd := *root
	cmd.PersistentFlags().StringVarP(&globalCfg.Repository, "repository", "R", "", "Path to repository to operate on")
	cmd.AddCommand(Fold(&globalCfg), Pop(&globalCfg))
	return &cmd
}
