# Multiple Patches Test

Test handling multiple patches with --all flag.

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
$ jj commit --quiet -m "Initial commit" »
$ jj log --config ui.graph.style=ascii -T 'description.first_line() ++ "\n"' »
@
o  Initial commit
+

$ # Step 1: Create multiple patch commits
$ echo "modified 1" > test-project/file1.txt »
$ jj commit --quiet -m "[PATCH] patch1.diff" »
$ echo "modified 2" > test-project/file2.txt »
$ jj commit --quiet -m "[PATCH] patch2.diff" »
$ echo "modified 3" > test-project/file3.txt »
$ jj commit --quiet -m "[PATCH] patch3.diff" »
$ jj log --config ui.graph.style=ascii -T 'description.first_line() ++ "\n"' »
@
o  [PATCH] patch3.diff
o  [PATCH] patch2.diff
o  [PATCH] patch1.diff
o  Initial commit
+

$ # Step 2: Fold all patches
$ quahog fold --root test-project --all »
Folding 3 patches into "test-project"
Successfully folded 3 patches

$ jj log --config ui.graph.style=ascii -T 'description.first_line() ++ "\n"' »
@
o  #QUAHOG Modify patches for test-project.
o  Initial commit
+
$ cat test-project/patches/series »
patch1.diff
patch2.diff
patch3.diff
$ # Step 3: Pop all patches back
$ quahog pop --root test-project --all »
Popping 3 patches from "test-project"
Popping patch "patch3.diff"
Popping patch "patch2.diff"
Popping patch "patch1.diff"
Successfully popped 3 patches

$ jj log --config ui.graph.style=ascii -T 'description.first_line() ++ "\n"' »
@
o  [PATCH] patch3.diff
o  [PATCH] patch2.diff
o  [PATCH] patch1.diff
o  #QUAHOG Modify patches for test-project.
o  Initial commit
+
$ # Verify series is empty
$ cat test-project/patches/series »

$ quahog fold --root test-project --count 2 »
Folding 2 patches into "test-project"
Successfully folded 2 patches

$ jj log --config ui.graph.style=ascii -T 'description.first_line() ++ "\n"' »
@
o  [PATCH] patch3.diff
o  #QUAHOG Modify patches for test-project.
o  Initial commit
+

```
