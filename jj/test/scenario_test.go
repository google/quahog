// Copyright 2025 Google LLC
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/quahog/jj/test/runner"
)

// TestScenarios runs all scenario shell-style markdown tests
func TestScenarios(t *testing.T) {
	// Check prerequisites
	if !isCommandAvailable("jj") {
		t.Skip("jj command not available - install Jujutsu VCS to run these tests")
	}
	if !isCommandAvailable("git") {
		t.Skip("git command not available - install git to run these tests")
	}

	// Find all scenario test files
	testFiles, err := filepath.Glob("scenarios/*.md")
	if err != nil {
		t.Fatalf("Failed to find test files: %v", err)
	}

	if len(testFiles) == 0 {
		t.Skip("No scenario test files found (looking for scenarios/*.md)")
	}

	// Run each test file in parallel
	for _, testFile := range testFiles {
		t.Run(filepath.Base(testFile), func(t *testing.T) {
			runScenarioTest(t, testFile)
		})
	}
}

// runScenarioTest executes a single scenario test
func runScenarioTest(t *testing.T, testFile string) {
	// Parse the test
	test, err := runner.ParseFile(testFile)
	if err != nil {
		t.Fatalf("Failed to parse test file %s: %v", testFile, err)
	}
	t.Logf("Running test: %s", test.Name)
	if test.Description != "" {
		t.Logf("Description: %s", test.Description)
	}
	// Create executor
	executor, err := runner.NewScenarioExecutor(t)
	if err != nil {
		t.Fatalf("Failed to create simple executor: %v", err)
	}
	defer executor.Cleanup()
	// Run the test
	if err := executor.RunTest(test); err != nil {
		t.Errorf("Test failed: %v", err)
	}
}

// TestScenarioParserBasic tests the simple parser
func TestScenarioParserBasic(t *testing.T) {

	testContent := `# Simple Test

Basic parser test

**Setup:**
` + "```yaml" + `
test.txt: "hello world"
empty.txt: ""
` + "```" + `

**Test:**
` + "```bash" + `
$ echo "test" »
test

$ # This is a comment
$ cat test.txt »
hello world

$ cat empty.txt »

` + "```" + `
`

	test, err := runner.ParseContent(testContent)
	if err != nil {
		t.Fatalf("Failed to parse test: %v", err)
	}

	// Verify parsing results
	if test.Name != "Simple Test" {
		t.Errorf("Expected name 'Simple Test', got '%s'", test.Name)
	}

	if test.Description != "Basic parser test" {
		t.Errorf("Expected description 'Basic parser test', got '%s'", test.Description)
	}

	if len(test.Setup) != 2 {
		t.Errorf("Expected 2 setup files, got %d", len(test.Setup))
	}

	if test.Setup["test.txt"] != "hello world" {
		t.Errorf("Expected setup file content 'hello world', got '%s'", test.Setup["test.txt"])
	}

	if test.Setup["empty.txt"] != "" {
		t.Errorf("Expected empty setup file, got '%s'", test.Setup["empty.txt"])
	}

	// Check script items
	if len(test.Script) == 0 {
		t.Errorf("Expected script items, got none")
	}

	// Should have command and output pairs
	foundEcho := false
	foundComment := false
	for _, item := range test.Script {
		if item.IsCommand && strings.Split(item.Content, " ")[0] == "echo" {
			foundEcho = true
		}
		if !item.IsCommand && item.Content == "This is a comment" {
			foundComment = true
		}
	}

	if !foundEcho {
		t.Errorf("Expected to find echo command")
	}

	if !foundComment {
		t.Errorf("Expected to find comment")
	}
}

// isCommandAvailable checks if a command exists in PATH
func isCommandAvailable(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}
