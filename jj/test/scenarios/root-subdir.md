# Execution from Root Subdir Test

Test the use of a subdir within the repo as a root from which quahog is invoked.

**Setup:**

```yaml
test-project/file.txt: |
  original content

test-project/patches/series: ""
```

**Test:**

```bash
$ jj git init --quiet && \
  jj commit --quiet -m "Initial commit" && \
  echo "modified content" > test-project/file.txt && jj commit --quiet -m "[PATCH] file.diff" »
$ # Step 2: Fold the patch from test-project
$ cd test-project »
$ quahog fold --root . --count 1 »
Folding 1 patch into "."
Successfully folded 1 patch

$ # Step 3: Pop the patch back
$ quahog pop --root . --count 1 »
Popping 1 patch from "."
Popping patch "file.diff"
Successfully popped 1 patch

$ jj log --config ui.graph.style=ascii -T 'description.first_line() ++ "\n"' »
@
o  [PATCH] file.diff
o  #QUAHOG Modify patches for test-project.
o  Initial commit
+

```
