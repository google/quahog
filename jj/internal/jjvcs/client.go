// Copyright 2025 Google LLC
// SPDX-License-Identifier: Apache-2.0

package jjvcs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Change holds detailed information about a single change.
type Change struct {
	ID           string
	IsMutable    bool
	IsConflicted bool
	IsDivergent  bool
	IsEmpty      bool
	Description  string
	Parents      []string
}

type Client interface {
	Run(...string) (string, error)
	Root() (string, error)
	Revs(string) ([]*Change, error)
	Rev(string) (*Change, error)
	Squash([]string, string) error
}
type client struct{}

func NewClient() Client {
	return &client{}
}

// Run executes a jj command and returns its output.
func (j *client) Run(args ...string) (string, error) {
	cmd := exec.Command("jj", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("jj command failed: %s\nerror: %w\nstderr: %s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String(), nil
}

// Root returns the repo root path
func (j *client) Root() (abspath string, err error) {
	rootPath, err := j.Run("root")
	if err != nil {
		return "", fmt.Errorf("failed to get root path: %w", err)
	}
	return strings.TrimSpace(rootPath), nil
}

func (j *client) Revs(revset string) ([]*Change, error) {
	tplParts := []string{
		"change_id.short()",
		"conflict",
		"divergent",
		"!immutable",
		"empty",
		`parents.map(|c| c.change_id().short()).join(",")`,
		"description.escape_json()",
		`"\n"`,
	}
	template := strings.Join(tplParts, `++" "++`)
	out, err := j.Run("log", "--no-graph", "--template", template, "-r", revset)
	if err != nil {
		return nil, fmt.Errorf("failed to get commit info for %s: %w", revset, err)
	}
	commits := []*Change{}
	for entry := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(entry, " ", len(tplParts)-1)
		if len(parts) < len(tplParts)-1 {
			return nil, fmt.Errorf("unexpected log entry format: %q", entry)
		}
		var description string
		if err := json.Unmarshal([]byte(parts[6]), &description); err != nil {
			return nil, fmt.Errorf("bad json encoding: %w", err)
		}
		commits = append(commits, &Change{
			ID:           parts[0],
			IsConflicted: parts[1] == "true",
			IsDivergent:  parts[2] == "true",
			IsMutable:    parts[3] == "true",
			IsEmpty:      parts[4] == "true",
			Parents:      strings.Split(parts[5], ","),
			Description:  description,
		})
	}
	return commits, nil
}

func (j *client) Rev(revset string) (*Change, error) {
	c, err := j.Revs(revset)
	if err != nil {
		return nil, err
	}
	if len(c) != 1 {
		return nil, fmt.Errorf("failed to get one commit for revset %s", revset)
	}
	return c[0], nil
}

// Squash squashes multiple commits into a base commit
func (j *client) Squash(commitIDs []string, baseCommit string) error {
	if len(commitIDs) == 0 {
		return nil
	}
	// Use jj squash to combine commits, taking the message of the base commit
	args := []string{"squash", "--use-destination-message"}
	if baseCommit != "" {
		args = append(args, "--into", baseCommit)
	}
	args = append(args, "--from", strings.Join(commitIDs, "|"))
	_, err := j.Run(args...)
	return err
}
