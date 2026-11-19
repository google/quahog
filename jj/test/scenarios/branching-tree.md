# Branching Tree Pop and Fold Test

Test that `quahog pop` and `quahog fold` operate strictly at the current working
copy location without rebasing sibling branches off a shared parent commit.

**Setup:**

```yaml
test-project/file1.txt: "modified 1\n"
test-project/file2.txt: "modified 2\n"
test-project/file3.txt: "modified 3\n"
test-project/patches/series: |
  patch1.diff
  patch2.diff
  patch3.diff
test-project/patches/patch1.diff: |
  --- a/file1.txt
  +++ b/file1.txt
  @@ -1,1 +1,1 @@
  -original 1
  +modified 1
test-project/patches/patch2.diff: |
  --- a/file2.txt
  +++ b/file2.txt
  @@ -1,1 +1,1 @@
  -original 2
  +modified 2
test-project/patches/patch3.diff: |
  --- a/file3.txt
  +++ b/file3.txt
  @@ -1,1 +1,1 @@
  -original 3
  +modified 3
other.txt: "base\n"
```

**Test:**

```bash
$ jj git init --quiet && \
  jj commit --quiet -m "Initial commit" »

$ # Create unrelated sibling branches (including an unrelated [PATCH] commit) off Initial commit
$ echo "branch a" > other.txt && jj commit --quiet -m "Branch A" »
$ echo "branch a child" > other.txt && jj commit --quiet -m "Branch A child" »
$ jj new --quiet 'description(exact:"Initial commit\n")' && \
  echo "branch b" > other.txt && jj commit --quiet -m "Branch B" »
$ jj new --quiet 'description(exact:"Initial commit\n")' && \
  echo "unrelated patch" > test-project/unrelated.txt && jj commit --quiet -m "[PATCH] unrelated-other.diff" »

$ # Position @ as a fresh empty working-copy commit directly on Initial commit
$ jj new --quiet 'description(exact:"Initial commit\n")' »

$ # Verify fold from @ does not hijack sibling [PATCH] branches and rolls back cleanly
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
$ jj log --no-graph -r 'children(description(exact:"Initial commit\n"))' -T 'if(description == "", "(empty)", description.first_line()) ++ "\n"' | LC_ALL=C sort »
(empty)
Branch A
Branch B
[PATCH] unrelated-other.diff

$ # Step 1: Pop all patches from @ without affecting sibling branches
$ quahog pop --root test-project --all »
Popping 3 patches from "test-project"
Popping patch "patch3.diff"
Popping patch "patch2.diff"
Popping patch "patch1.diff"
Successfully popped 3 patches

$ # Verify @ is an empty commit on top of the popped patch chain rooted at Initial commit
$ jj log --no-graph -r 'description(exact:"Initial commit\n")::@' -T 'if(description == "", "(empty)", description.first_line()) ++ "\n"' »
(empty)
[PATCH] patch3.diff
[PATCH] patch2.diff
[PATCH] patch1.diff
#QUAHOG Modify patches for test-project.
Initial commit

$ # Verify sibling branches still have Initial commit (NOT #QUAHOG) as their parent
$ jj log --no-graph -r 'children(description(exact:"Initial commit\n"))' -T 'description.first_line() ++ "\n"' | LC_ALL=C sort »
#QUAHOG Modify patches for test-project.
Branch A
Branch B
[PATCH] unrelated-other.diff

$ # Verify #QUAHOG has ONLY [PATCH] patch1.diff as its child
$ jj log --no-graph -r 'children(description(substring:"#QUAHOG"))' -T 'description.first_line() ++ "\n"' »
[PATCH] patch1.diff

$ # Step 2: Fold 1 patch, then pop it back before the remaining patches
$ quahog fold --root test-project --count 1 »
Folding 1 patch into "test-project"
Successfully folded 1 patch

$ quahog pop --root test-project --count 1 »
Popping 1 patch from "test-project"
Popping patch "patch1.diff"
Successfully popped 1 patch

$ # Verify all 3 patches form a single linear chain and @ is restored on top of patch3.diff
$ jj log --no-graph -r 'description(exact:"Initial commit\n")::@' -T 'if(description == "", "(empty)", description.first_line()) ++ "\n"' »
(empty)
[PATCH] patch3.diff
[PATCH] patch2.diff
[PATCH] patch1.diff
#QUAHOG Modify patches for test-project.
Initial commit

$ # Step 3: Fold all and test popping when empty @ sits before a downstream commit
$ quahog fold --root test-project --all »
Folding 3 patches into "test-project"
Successfully folded 3 patches

$ # Create a fresh branch off Initial commit with a downstream commit and insert empty @ before it
$ jj new --quiet 'description(exact:"Initial commit\n")' && \
  echo "downstream work" > other.txt && jj commit --quiet -m "Downstream CL" && \
  jj new --quiet --insert-before 'description(exact:"Downstream CL\n")' »

$ quahog pop --root test-project --count 2 »
Popping 2 patches from "test-project"
Popping patch "patch3.diff"
Popping patch "patch2.diff"
Successfully popped 2 patches

$ # Verify #QUAHOG -> patch2 -> patch3 -> @ -> Downstream CL, with siblings untouched
$ jj log --no-graph -r 'description(exact:"Initial commit\n")::description(exact:"Downstream CL\n")' -T 'if(description == "", "(empty)", description.first_line()) ++ "\n"' »
Downstream CL
(empty)
[PATCH] patch3.diff
[PATCH] patch2.diff
#QUAHOG Modify patches for test-project.
Initial commit

$ # Verify folding preserves the surviving empty @ before Downstream CL (both when @- is kept and when @- is folded)
$ quahog fold --root test-project --count 1 »
Folding 1 patch into "test-project"
Successfully folded 1 patch

$ quahog fold --root test-project --count 1 »
Folding 1 patch into "test-project"
Successfully folded 1 patch

$ jj log --no-graph -r 'description(exact:"Initial commit\n")::description(exact:"Downstream CL\n")' -T 'if(description == "", "(empty)", description.first_line()) ++ "\n"' »
Downstream CL
(empty)
#QUAHOG Modify patches for test-project.
Initial commit

$ # Step 4: Test popping and folding from an empty @ that already has a description
$ jj new --quiet 'description(exact:"Initial commit\n")' -m "WIP empty change" »
$ quahog pop --root test-project --count 1 »
Popping 1 patch from "test-project"
Popping patch "patch3.diff"
Successfully popped 1 patch

$ jj log --no-graph -r 'description(exact:"Initial commit\n")::@' -T 'description.first_line() ++ "\n"' »
WIP empty change
[PATCH] patch3.diff
#QUAHOG Modify patches for test-project.
Initial commit

$ quahog fold --root test-project --count 1 »
Folding 1 patch into "test-project"
Successfully folded 1 patch

$ # Verify @ stays on WIP empty change with no stray empty sibling created on #QUAHOG
$ jj log --no-graph -r 'description(exact:"Initial commit\n")::@' -T 'description.first_line() ++ "\n"' »
WIP empty change
#QUAHOG Modify patches for test-project.
Initial commit

$ jj log --no-graph -r 'children(@-)' -T 'if(description == "", "(undescribed)", description.first_line()) ++ "\n"' »
WIP empty change

```
