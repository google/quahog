// Copyright 2025 Google LLC
// SPDX-License-Identifier: Apache-2.0

package quahog

import (
	"fmt"
	"log"
	"slices"
	"strings"

	"github.com/google/quahog/jj/internal/jjvcs"
	"github.com/google/quahog/jj/internal/quilt"
)

const (
	// The keyword to identify a base commit.
	baseCommitKeyword = "QUAHOG"
	// The prefix for a patch commit description.
	patchCommitPrefix = "[PATCH]"
)

// IsBase returns true if the change is a QUAHOG base change.
func IsBase(c *jjvcs.Change) bool {
	return strings.Contains(c.Description, baseCommitKeyword) && c.IsMutable
}

// IsPatch returns true if the change description starts with the patch prefix.
func IsPatch(c *jjvcs.Change) bool {
	return strings.HasPrefix(c.Description, patchCommitPrefix) && c.IsMutable
}

// PatchChain holds the analyzed chain of patch commits.
type PatchChain struct {
	// The sequence of patch commits from the starting revision up to the root.
	Patches []*jjvcs.Change
	// The root change that stopped the chain walk (e.g., a base, immutable, or non-patch change).
	Base *jjvcs.Change
}

type ChainOptions struct {
	OriginRev    string
	RootRelpath  string
	NoCreateBase bool
}

func NewPatchChain(jj jjvcs.Client, opts ChainOptions) (*PatchChain, error) {
	// TODO: Offer a mode where RootRelpath isn't required since `pop --root` is only case we _really_ need to specify it
	if opts.RootRelpath == "" {
		return nil, fmt.Errorf("root relpath required")
	}
	var originID string
	if c, err := jj.Rev(opts.OriginRev); err != nil {
		return nil, err
	} else if !c.IsEmpty || len(c.Parents) != 1 {
		originID = c.ID
	} else {
		originID = c.Parents[0]
	}
	// Revset captures all ancestors and descendents of our target rev, from oldest to newest.
	revset := fmt.Sprintf("descendants(%s)|(heads(immutable())::ancestors(%s))", originID, originID)
	commits, err := jj.Revs(revset)
	if err != nil {
		return nil, err
	}
	allCommits := make(map[string]*jjvcs.Change, len(commits))
	children := make(map[string][]*jjvcs.Change, len(commits))
	// NOTE: Iterate in reverse so we visit parents before children
	for _, commit := range slices.Backward(commits) {
		allCommits[commit.ID] = commit
		for _, parent := range commit.Parents {
			children[parent] = append(children[parent], commit)
		}
	}
	// Overall, the strategy to build the chain is to explore from the origin "inside out":
	// 1) Search towards a base commit and record any patches we find along the way
	// 2) Search towards the leaf for any patches beyond the start commit
	//
	// Notably, we try to detect cases where >1 patch and/or base commit may be
	// adjacent to our chain. These are errors when parents and warnings when
	// children. In both cases, they cease further exploration.
	chain := &PatchChain{
		Patches: []*jjvcs.Change{},
	}
	originCommit := allCommits[originID]
	pathToRoot := []*jjvcs.Change{}
	for current := originCommit; ; {
		if IsPatch(current) {
			pathToRoot = append(pathToRoot, current)
		} else if IsBase(current) {
			if current.IsConflicted {
				return nil, fmt.Errorf("base commit %s is conflicted", current.ID)
			}
			if current.IsDivergent {
				return nil, fmt.Errorf("base commit %s is divergent", current.ID)
			}
			chain.Base = current
			break
		} else {
			break // if starting from normal commit, wait for forward search to set root
		}
		var candidates []*jjvcs.Change
		for _, parentID := range current.Parents {
			if parentCommit, ok := allCommits[parentID]; ok {
				if IsPatch(parentCommit) || IsBase(parentCommit) || !parentCommit.IsMutable {
					candidates = append(candidates, parentCommit)
				}
			}
		}
		if len(candidates) > 1 {
			return nil, fmt.Errorf("ambiguous patch chain: commit %s has multiple patch/base parents", current.ID)
		}
		if len(candidates) == 0 {
			break
		}
		current = candidates[0]
	}
	// Search forwards from the origin to the leaf of the patch chain.
	pathToLeaf := []*jjvcs.Change{}
	for current := originCommit; ; {
		childCommits := children[current.ID]
		var candidates []*jjvcs.Change
		for _, child := range childCommits {
			if IsPatch(child) {
				candidates = append(candidates, child)
			}
		}
		if len(candidates) > 1 {
			// TODO: This should be routed out to a buffer
			log.Printf("warning: ambiguous patch chain: commit %s has multiple patch children", current.ID)
			break
		}
		if len(candidates) == 0 {
			break // Reached a leaf.
		}
		child := candidates[0]
		pathToLeaf = append(pathToLeaf, child)
		current = child
	}
	// Assemble the final chain.
	for _, commit := range slices.Backward(pathToRoot) {
		chain.Patches = append(chain.Patches, commit)
	}
	chain.Patches = append(chain.Patches, pathToLeaf...)
	// Create a Base
	if chain.Base == nil {
		if opts.NoCreateBase {
			return nil, fmt.Errorf("failed to locate base commit")
		}
		commitMsg := fmt.Sprintf("#%s Modify patches for %s.", baseCommitKeyword, opts.RootRelpath)
		args := []string{"new", "-m", commitMsg}
		if len(chain.Patches) != 0 {
			args = append(args, "--insert-before", chain.Patches[0].ID)
		}
		// TODO: Should we add an --insert-after to get the base to appear pre-octopus?
		_, err := jj.Run(args...)
		if err != nil {
			return nil, err
		}
		// TODO: Fix chain.Patches[0].Parents which will now be stale
		c, err := jj.Rev("@")
		if err != nil {
			return nil, err
		}
		chain.Base = c
	} else {
		_, err := jj.Run("edit", chain.Base.ID)
		if err != nil {
			return nil, err
		}
	}
	return chain, nil
}

func PatchMetadata(c *jjvcs.Change) (name, description string, err error) {
	lines := strings.Split(c.Description, "\n")
	if !IsPatch(c) {
		return "", "", fmt.Errorf("invalid patch commit format: %s", lines[0])
	}
	// Extract patch name from first line
	patchName := strings.TrimSpace(strings.TrimPrefix(lines[0], patchCommitPrefix))
	// Replace spaces with dashes for filename
	patchName = strings.ReplaceAll(patchName, " ", "-")
	if !strings.HasSuffix(patchName, ".diff") && !strings.HasSuffix(patchName, ".patch") {
		patchName += ".patch"
	}
	// Get patch description (everything after first line)
	var desc strings.Builder
	for i, line := range lines[1:] {
		if strings.TrimSpace(line) != "" || i > 0 {
			desc.WriteString(line)
			desc.WriteString("\n")
		}
	}
	return patchName, desc.String(), nil
}

// PatchContent generates a patch from a commit
func PatchContent(jj jjvcs.Client, c *jjvcs.Change, repoAbspath, repoRelpath string) (string, error) {
	_, desc, err := PatchMetadata(c)
	if err != nil {
		return "", fmt.Errorf("failed to generate patch metadata: %w", err)
	}
	// Generate diff for the commit
	// TODO: Do this a bit more efficiently than doing two separate diffs.
	diffWhole, err := jj.Run("diff", "--git", "-r", c.ID)
	if err != nil {
		return "", fmt.Errorf("failed to generate diff: %w", err)
	}
	diff, err := jj.Run("diff", "--git", "-r", c.ID, "--", repoAbspath)
	if err != nil {
		return "", fmt.Errorf("failed to generate diff: %w", err)
	}
	if diffWhole != diff {
		return "", fmt.Errorf("patch contains edits outside root")
	}
	// Convert jj diff to quilt format
	quiltDiff := quilt.FormatGitDiff(diff, repoRelpath)
	// Combine description and diff
	var content strings.Builder
	if len(desc) > 0 {
		content.WriteString(strings.TrimRight(desc, "\n"))
		content.WriteString("\n\n")
	}
	content.WriteString(quiltDiff)
	return content.String(), nil
}
