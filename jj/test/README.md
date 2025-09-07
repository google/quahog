# Quahog Scenario Shell Test Format

## Overview

Quahog scenario tests are essentially shell scripts in markdown code blocks.
Expected output is written after the command we expect to produce it. Initial
filesystem setup can be done using a YAML block.

## Format

````markdown
# Test Name

Brief description of what this test does.

**Setup:**

```yaml
path/to/file: |
  this is file content
separate/path/to/another/file: "this is some more content"
```

**Test:**

```bash
$ # Comments explain steps in the test.
$ # Commands are prefixed with '$' and end with '»'.
$ echo "Hello, World!" »
Hello, World!

$ # Commands can also span multiple lines. The command ends at the '»'.
$ jj commit -m "A commit message
that spans multiple lines" »

$ # Use standard shell commands to make assertions about the system state.
$ # For example, verify the content of a file using 'cat'.
$ cat path/to/file »
this is file content

$ # Verify that a file does NOT exist by checking the error from 'ls'.
$ ls non-existent-file.txt »
ls: cannot access 'non-existent-file.txt': No such file or directory

$ # Finally, you can test quahog without building the binary.
$ quahog fold --root test-project --count 1 »
Folding 1 patch into "test-project"
Successfully folded 1 patch

```
````
