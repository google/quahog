// Copyright 2025 Google LLC
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"text/template"

	"github.com/google/go-cmp/cmp"
	"github.com/google/quahog/jj/cmd"
)

// ScenarioExecutor runs simple shell-style tests.
type ScenarioExecutor struct {
	tempDir    string
	execDir    string
	t          *testing.T
	lastOutput string
}

// NewScenarioExecutor creates a new simple test executor.
func NewScenarioExecutor(t *testing.T) (*ScenarioExecutor, error) {
	tempDir, err := os.MkdirTemp("", "quahog-scenario-test-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	return &ScenarioExecutor{
		tempDir: tempDir,
		execDir: tempDir,
		t:       t,
	}, nil
}

// Cleanup removes temporary directories.
func (e *ScenarioExecutor) Cleanup() {
	if e.tempDir != "" {
		os.RemoveAll(e.tempDir)
	}
}

// RunTest executes a complete simple test.
func (e *ScenarioExecutor) RunTest(test *ScenarioTest) error {
	// Ensure dependencies are available.
	if !e.isCommandAvailable("jj") {
		e.t.Skip("jj command not available")
		return nil
	}
	if !e.isCommandAvailable("git") {
		e.t.Skip("git command not available")
		return nil
	}
	// Set up initial files from the YAML block.
	if err := e.setupFiles(test.Setup); err != nil {
		return fmt.Errorf("setup failed: %w", err)
	}

	// Execute each step in the script.
	for i, step := range test.Script {
		if !step.IsCommand {
			e.t.Logf("# %s", step.Content)
			continue
		}
		e.t.Logf("$ %s", step.Content)
		e.lastOutput = "" // Reset output before each command.
		// Determine which command executor to use.
		fields := strings.Fields(step.Content)
		var cmdErr error
		if len(fields) == 0 {
		} else if fields[0] == "quahog" {
			cmdErr = e.executeQuahogCommand(step.Content)
		} else if fields[0] == "cd" {
			e.execDir = filepath.Join(e.execDir, fields[1])
		} else {
			cmdErr = e.executeShellCommand(step.Content)
		}
		if cmdErr != nil {
			return fmt.Errorf("command execution failed for step %d: %w", i+1, cmdErr)
		}

		// Immediately verify the output against the expected output for the step.
		if err := e.verifyOutput(step.ExpectedOutput); err != nil {
			return fmt.Errorf("output verification failed for step %d (`%s`):\n%w", i+1, step.Content, err)
		}
	}

	return nil
}

func (e *ScenarioExecutor) isCommandAvailable(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

func (e *ScenarioExecutor) setupFiles(setup map[string]string) error {
	for filePath, content := range setup {
		fullPath := filepath.Join(e.tempDir, filePath)
		// Create directory if needed.
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
		// Write file.
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write file %s: %w", fullPath, err)
		}
	}
	return nil
}

// executeQuahogCommand runs an internal quahog command.
func (e *ScenarioExecutor) executeQuahogCommand(command string) error {
	var output bytes.Buffer
	defer func() { e.lastOutput = output.String() }()

	// NOTE: Substitute relative root params for ones absolute wrt execDir
	rootRe := regexp.MustCompile(`(--root[= ])([^/][^\s]*)`)
	command = rootRe.ReplaceAllStringFunc(command, func(match string) string {
		parts := rootRe.FindStringSubmatch(match)
		return parts[1] + filepath.Join(e.execDir, parts[2])
	})
	// Cobra wants a slice of args, excluding the program name.
	args := strings.Fields(command)[1:]
	// NOTE: Detect repository from execDir, if present
	var jjRoot string
	for dir := e.execDir; dir != "/"; dir, _ = filepath.Split(filepath.Clean(dir)) {
		if _, err := os.Stat(filepath.Join(dir, ".jj")); err == nil {
			jjRoot = dir
			break
		}
	}
	// Construct separate command to isolate each execution
	root := cmd.Root()
	root.PersistentFlags().Set("repository", jjRoot)
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs(args)
	root.Execute() // We don't return Execute's error, as tests may expect failure.
	return nil
}

// executeShellCommand runs an external shell command.
func (e *ScenarioExecutor) executeShellCommand(command string) error {
	var output bytes.Buffer
	defer func() { e.lastOutput = output.String() }()

	cmd := exec.Command("/bin/bash", "-c", command)
	cmd.Dir = e.execDir
	cmd.Stdout = &output
	cmd.Stderr = &output
	cmd.Run() // We don't return Run's error, as tests may expect failure.
	return nil
}

// verifyOutput treats expectedOutput as a Go template and compares its
// rendered output with the last command's actual output.
func (e *ScenarioExecutor) verifyOutput(expectedTemplate string) error {
	// Create and parse the template from the expected output string.
	tmpl, err := template.New("output").Parse(expectedTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse output template: %w", err)
	}

	// Define the data to be injected into the template.
	templateData := struct{ TempDir string }{TempDir: e.tempDir}

	// Execute the template to generate the final expected output string.
	var expectedBuf bytes.Buffer
	if err := tmpl.Execute(&expectedBuf, templateData); err != nil {
		return fmt.Errorf("failed to execute output template: %w", err)
	}

	// Trim trailing whitespace for cleaner and more reliable comparisons.
	expected := strings.TrimSpace(expectedBuf.String())
	actual := strings.TrimSpace(e.lastOutput)

	// Compare the rendered template with the actual output.
	if actual != expected {
		diff := cmp.Diff(expected, actual)
		return fmt.Errorf("output mismatch (-expected, +actual):\n%s", diff)
	}

	return nil
}
