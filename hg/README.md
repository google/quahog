# 🐚 Quahog Mercurial Extension

## Overview

Quahog enables managing Quilt-style patch sets using Mercurial.

## Installation

The Quahog extension is distributed as a Mercurial extension and can be
installed by making it reachable from Mercurial and activating it by adding a
snippet to your `~/.hgrc` file:

```ini
[extensions]
quahog =
```

## Concepts

- **Patches** are modifications to software, often source code, that are
  discrete and repeatable.
- **Quilt** is a patch management system that models all changes to a piece of
  software as a _series_ of individual patches, applied in sequence.
- **Quahog** is a Quilt-compatible patch management system that represents
  patches as Mercurial commits.
- **Root** is a directory with Quilt-compatible patch files located in
  `./patches/` and a series file at `./patches/series`.
- **Patch Commit** is the Quahog representation of a patch denoted by a change
  description of the form `[PATCH] <path-filename>`. It contains no patch
  file: All contained diffs comprise the patch when folded.
- **Base Commit** or **Quahog Commit** is the Quahog representation of all
  diffs, both patch and non-patch modifications, to the associated root. While
  Patch Commits exist, it may also contain reverse diffs of these patches'
  contents, files, series entries. Defined as a commit containing `#QUAHOG`.
- **Pop** is the Quahog operation that removes one or more patch files from
  the end of the patch series and converts them into Patch Commits.
- **Fold** is the Quahog operation that converts one or more patch commits
  back into patch file and patch series additions and folds them into the Base
  Commit.

## Guides

Example project: `path/to/import`

The following Quilt patch set is used in all following examples:

```
  path/to/import
  └── patches
      ├── series  # contains just the line "patch1.diff"
      └── patch1.diff
```

If you'd like to start using patching, you can run the following to begin:

```sh
root='path/to/import'
mkdir $root/patches
touch $root/patches/series
```

### Create a new patch

To create a patch, simply create a Patch Commit with the desired changes and
fold it to create a Base Commit with the desired changes.

```shell
$ edit path/to/import/file_to_modify.py
```

```shell
# '[PATCH]' makes it a Patch Commit.
$ hg commit -m "[PATCH] patch2.diff"
```

```shell
$ hg qu-fold --root=path/to/import
```

```shell
$ hg xl

@  3f76b8a7 tip
|  #QUAHOG Modify patches for path/to/import.
o  24b746b9 10 hours ago
|  HEAD
```

```shell
$ hg status --rev .^

M  path/to/import/file_to_modify.py
M  path/to/import/patches/series
A  path/to/import/patches/patch2.diff
```

```shell
$ cat path/to/import/patches/series

patch1.diff
patch2.diff
```

### Edit an existing patch

To edit a patch, the basic procedure is to pop the patch (thus removing its
changes), perform your edits, amend the Patch Commit, then fold it back to
regenerate the patch file in the Base Commit.

```shell
$ hg qu-pop --root=path/to/import
```

```shell
$ hg xl

o  2fa76f57 tip
|  (…) [PATCH] patch1.diff
o  3f76b8a7
|  #QUAHOG Modify patches for path/to/import.
@  24b746b9 10 hours ago
|  HEAD
```

Checkout the patch commit:

```shell
$ hg checkout tip
```

```shell
$ edit path/to/import/already_modified_file.py
```

```shell
$ hg amend
```

Update subsequent patches, if any:

```shell
$ hg evolve
```

```shell
$ hg qu-fold --root=path/to/import
```

```shell
$ hg xl

@  2fb4e923
|  #QUAHOG Modify patches for path/to/import.
o  24b746b9 10 hours ago
|  HEAD
```

```shell
$ hg status --rev .^

M  path/to/import/already_modified_file.py
M  path/to/import/patches/patch1.diff
```

---

To edit a patch that's not the most recent change, things remain largely the
same except you pop all patches up to the patch to edit, then checkout the
patch, amend it, and fold back down.

```shell
$ cat path/to/import/patches/series

patch1.diff
patch2.diff
```

```shell
$ hg qu-pop --count 2 --root=path/to/import
```

```shell
$ hg xl

o  a0d18236 tip
|  (…) [PATCH] patch2.diff
o  x25b53bfa
|  (…) [PATCH] patch1.diff
o  1c046133
|  #QUAHOG Modify patches for path/to/import.
@  e4593d86 10 hours ago
|  HEAD
```

```shell
$ hg checkout x25
```

```shell
$ edit path/to/import/already_modified_file.py
```

```shell
$ hg amend
$ hg evolve --update
```

```shell
$ hg qu-fold --all --root=path/to/import
```

```shell
$ hg xl

@  2fb4e923
|  #QUAHOG Modify patches for path/to/import.
o  e4593d86 10 hours ago
|  HEAD
```

```shell
$ hg status --rev .^

M  path/to/import/already_modified_file.py
M  path/to/import/patches/patch1.diff
```

### Add and edit a patch description

Patch descriptions are free text included in the header of a patch file. They
can be useful to document the rationale and process of creating the patch. In
Quahog, these descriptions are stored in (and may be edited from) the Mercurial
commit description below the `[PATCH]` line.

```shell
$ edit path/to/import/file_to_modify.py
```

```shell
$ hg commit -m "[PATCH] patch2.diff

This is my comment."
```

```shell
$ hg qu-fold --root=path/to/import
```

```shell
$ hg xl

@  3f76b8a7 tip
|  #QUAHOG Modify patches for path/to/import.
o  24b746b9 10 hours ago
|  HEAD
```

```shell
$ head -n 1 path/to/import/patches/patch2.diff

This is my comment.
```

---

To modify an existing patch commit to include a description, you can do so by
using `hg reword` to change the commit message of the desired commit.

```shell
$ hg qu-pop --root=path/to/import
```

```shell
$ hg xl

o  a0d18236 tip
|  (…) [PATCH] patch2.diff
o  1c046133
|  #QUAHOG Modify patches for path/to/import.
@  e4593d86 10 hours ago
|  HEAD
```

```shell
$ hg reword -m "[PATCH] patch2.diff

This is my NEW comment."
```

```shell
$ hg qu-fold --root=path/to/import
```

```shell
$ head -n 1 path/to/import/patches/patch2.diff

This is my NEW comment.
```

### Update patches for new upstream version

To update a chain of patches: pop all patches, import the new version, rebase
the patches onto the new version, and fold them all back down.

Create an initially empty Base Commit and pop all patches:

```shell
$ hg commit --config=ui.allowemptycommit=1 -m "#QUAHOG Unpatched current version of path/to/import"
$ hg qu-pop --all --root path/to/import
```

```shell
$ hg xl
…
o  2fa76f57 tip
|  (…) [PATCH] patch1.diff
@  3f76b8a7
|  #QUAHOG Unpatched current version of path/to/import
o  24b746b9 10 hours ago
|  HEAD
╧
```

This commit has been amended to put the directory in a state that's completely
free of patches.

Now we can import a new version of the code, for example:

```shell
$ # command to do import
$ hg addremove
```

Commit and make sure to keep `#QUAHOG` in the description to create a new Base
Commit:

```shell
$ hg commit -m '#QUAHOG Import new version of path/to/import'
```

```shell
$ hg xl
…
@  9894ba84 tip
|  #QUAHOG Import new version of path/to/import
| o  2fa76f57
|/   (…) [PATCH] patch1.diff
o  3f76b8a7
|  #QUAHOG Unpatched current version of path/to/import
o  24b746b9 10 hours ago
|  HEAD
╧
```

Rebase all patch commits onto the import (this is the point where all the merge
conflicts will happen):

```shell
$ hg rebase -s 2fa -d .  # <and fix any merge conflicts>
```

```shell
$ hg xl

o  6c4c686f tip
|  (…) [PATCH] patch1.diff
@  9894ba84
|  #QUAHOG Import new version of path/to/import
o  3f76b8a7
|  #QUAHOG Unpatched current version of for path/to/import
o  24b746b9 10 hours ago
|  HEAD
╧
```

At this point you can further modify patch commits as necessary, e.g. until all
unittests pass.

Then, fold the patch commits back into the "Import new version" commit. (The
`qu-fold` command finds it implicitly because it operates on the nearest
`#QUAHOG` commit.)

```shell
$ hg qu-fold --all --root path/to/import
```

```shell
$ hg xl
…
@  7ae1adba
|  #QUAHOG Import new version of path/to/import
o  3f76b8a7
|  #QUAHOG Unpatched current version of path/to/import
o  24b746b9 10 hours ago
|  HEAD
╧
```

Squash the two commits that are left, as they don't make sense separately:

```shell
$ hg fold --exact . .^ -m "$(hg log -T '{desc}' -r .)"
```

```shell
$ hg xl

@  9a34a584
|  #QUAHOG Import new version of path/to/import
o  24b746b9 10 hours ago
|  HEAD
╧
```

> Question: what does it mean that the two commits didn't make sense separately?
>
> The setup was as follows:
>
> - _`HEAD`: old version of code + old patches_
> - `Unpatched current version`: remove old patches
> - `Import new version`: update code to new version + add new patches
>
> If we squash the two commits, we finally get a commit that does what we
> intended: \
> (update code to new version + update patches)

## See Also

- [Quilt](<https://en.wikipedia.org/wiki/Quilt_(software)>): A patch
  management tool.
