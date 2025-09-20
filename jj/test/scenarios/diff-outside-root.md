# Patch with Modifications Outside Root Test

Test rejection of patches whose diff includes changes outside the patch root.

**Setup:**

```yaml
test-project/patches/series: ""
```

**Test:**

```bash
$ jj git init »
Initialized repo in "."
$ jj commit --quiet -m "Initial commit" && \
  (echo foo > foo) && jj commit --quiet -m "[PATCH] foo" »
$ jj log --config ui.graph.style=ascii -T 'description.first_line() ++ "\n"' »
@
o  [PATCH] foo
o  Initial commit
+

$ # Succeeds when fold doesn't cover the conflicted change
$ quahog fold --root test-project --count 1 »
Folding 1 patch into "test-project"
encountered error. rolling back... done
Error: generating patch for foo.patch: patch contains edits outside root
Usage:
  quahog fold [flags]

Flags:
      --all           fold all patches
      --count int     number of patches to fold (default 1)
  -h, --help          help for fold
      --rev string    specific revisions to fold
      --root string   directory containing patches/ subdirectory
      --to string     base commit to fold into

Global Flags:
  -R, --repository string   Path to repository to operate on

```
