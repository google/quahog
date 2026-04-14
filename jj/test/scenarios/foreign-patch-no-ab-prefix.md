# Foreign Patch without a/b Prefix

Test that patches with repo-relative paths and no a/b prefix can be popped.

**Setup:**

```yaml
test-project/file.txt: |
  original content

test-project/patches/series: |
  fix.diff

test-project/patches/fix.diff: |
  --- test-project/file.txt
  +++ test-project/file.txt
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
$ # Pop the foreign-format patch (no a/b prefix)
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

```
