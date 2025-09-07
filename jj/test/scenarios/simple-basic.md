# Basic Fold and Pop Test

Test the fundamental quahog workflow: create a patch commit, fold it into a patch file, then pop it back.

**Setup:**

```yaml
test-project/file.txt: |
  original content

test-project/patches/series: ""
```

**Test:**

```bash
$ jj git init »
Initialized repo in "."
$ jj commit --quiet -m "Initial commit" »
$ # Step 1: Create patch commit
$ echo "modified content" > test-project/file.txt »
$ jj commit --quiet -m "[PATCH] test-patch-1.diff
This is a test patch" »
$ # Step 2: Fold the patch
$ quahog fold --root test-project --count 1 »
Folding 1 patch into "test-project"
Successfully folded 1 patch

$ # Verify patch file was created
$ cat test-project/patches/series »
test-patch-1.diff

$ # Check patch file content
$ cat test-project/patches/test-patch-1.diff »
This is a test patch

--- a/file.txt
+++ b/file.txt
@@ -1,1 +1,1 @@
-original content
+modified content

$ # Step 3: Pop the patch back
$ quahog pop --root test-project --count 1 »
Popping 1 patch from "test-project"
Popping patch "test-patch-1.diff"
Successfully popped 1 patch

$ # Verify patch file was removed
$ ls test-project/patches/test-patch-1.diff »
ls: cannot access 'test-project/patches/test-patch-1.diff': No such file or directory

$ # Verify series file is empty
$ cat test-project/patches/series »

```
