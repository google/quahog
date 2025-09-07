// Copyright 2025 Google LLC
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var Root = &cobra.Command{
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

func Execute() {
	err := Root.Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
