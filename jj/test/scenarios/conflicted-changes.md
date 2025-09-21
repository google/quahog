# Conflicted and Divergent Changes Test

Test handling of conflicted, divergent, and empty changes in patch chain.

**Setup:**

```yaml
test-project/patches/series: ""
```

**Test:**

```bash
$ jj git init »
Initialized repo in "."
$ jj commit --quiet -m "Initial commit" && \
  (echo foo > test-project/foo) && jj commit --quiet -m "[PATCH] foo1" && jj prev --quiet && \
  (echo bar > test-project/foo) && jj commit --quiet -m "[PATCH] foo2" && \
  jj rebase --quiet -r 'description("foo2")::' --insert-after 'description("foo1")' »
$ jj log --config ui.graph.style=ascii -T 'description.first_line() ++ "\n"' »
@
x  [PATCH] foo2
o  [PATCH] foo1
o  Initial commit
+

$ # Succeeds when fold doesn't cover the conflicted change
$ quahog fold --root test-project --count 1 »
Folding 1 patch into "test-project"
Successfully folded 1 patch

$ jj log --config ui.graph.style=ascii -T 'description.first_line() ++ "\n"' »
@
x  [PATCH] foo2
o  #QUAHOG Modify patches for test-project.
o  Initial commit
+

$ # Fails when conflicted patch is folded
$ quahog fold --root test-project --count 1 »
Folding 1 patch into "test-project"
encountered error. rolling back... done
Error: patch commit for foo2.patch is conflicted
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
