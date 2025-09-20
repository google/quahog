# Error Handling Test

Test various error conditions.

**Setup:**

```yaml
file.txt: "content"
patches/series: ""
```

**Test:**

```bash
$ jj git init »
Initialized repo in "."
$ jj commit --quiet -m "Initial commit" »
$ # Test non-existent directory
$ quahog fold --root patches --count 1 »
Error: {{.TempDir}}/patches: does not contain patches/ subdirectory
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

$ # Test non-existent root path
$ quahog fold --root nonexistent --count 1 »
Error: stat {{.TempDir}}/nonexistent: no such file or directory
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
