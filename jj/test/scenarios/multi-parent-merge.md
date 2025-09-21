# Multiple Parent Merge Test

Test handling patches with merges.

**Setup:**

```yaml
test-project/file1.txt: "content 1"
test-project/file2.txt: "content 2"
test-project/file3.txt: "content 3"
test-project/patches/series: ""
```

**Test:**

```bash
$ jj git init »
Initialized repo in "."
$ jj commit --quiet -m "Initial commit" && \
  touch test-project/foo && jj commit --quiet -m "[PATCH] Foo" && jj prev --quiet && \
  touch test-project/bar && jj commit --quiet -m "[PATCH] Bar" && jj prev --quiet && \
  jj rebase --quiet -r @ --insert-after 'description("PATCH")' »
$ jj log --config ui.graph.style=ascii -T 'description.first_line() ++ "\n"' »
@
|\
| o  [PATCH] Foo
o |  [PATCH] Bar
|/
o  Initial commit
+

$ quahog fold --root test-project --count 1 »
encountered error. rolling back... done
Error: --count 1 greater than patch chain length 0
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
$ # Should have remained unchanged
$ jj log --config ui.graph.style=ascii -T 'description.first_line() ++ "\n"' »
@
|\
| o  [PATCH] Foo
o |  [PATCH] Bar
|/
o  Initial commit
+

```
