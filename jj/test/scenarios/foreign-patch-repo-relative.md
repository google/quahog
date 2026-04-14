# Foreign Patch with Repo-Relative Paths

Test that patches with repo-relative paths (containing the subdir prefix) can be
popped and re-folded into canonical subdir-relative form.

**Setup:**

```yaml
test-project/file.txt: |
  original content

test-project/patches/series: |
  fix.diff

test-project/patches/fix.diff: |
  --- a/test-project/file.txt
  +++ b/test-project/file.txt
  @@ -1,1 +1,1 @@
  -original content
  +modified content
```

**Test:**

```bash
$ jj git init --quiet && \
  jj commit --quiet -m "Initial commit" && \
  echo "modified content" > test-project/file.txt && \
  jj commit --quiet -m "#QUAHOG Modify patches for test-project." »
$ # Pop the foreign-format patch
$ quahog pop --root test-project --all »
Popping 1 patch from "test-project"
Popping patch "fix.diff"
Successfully popped 1 patch

$ # Verify the PATCH commit was created
$ jj log --config ui.graph.style=ascii -T 'description.first_line() ++ "\n"' »
@
o  [PATCH] fix.diff
o  #QUAHOG Modify patches for test-project.
o  Initial commit
+

$ # Navigate to the PATCH commit and re-fold
$ jj new --quiet -r 'subject(exact:"[PATCH] fix.diff")' »
$ quahog fold --root test-project --count 1 »
Folding 1 patch into "test-project"
Successfully folded 1 patch

$ # Verify the re-folded patch uses canonical subdir-relative paths
$ cat test-project/patches/fix.diff »
--- a/file.txt
+++ b/file.txt
@@ -1,1 +1,1 @@
-original content
+modified content

```
