# 🦆 Quahog - Quilt-style Patch Management for Jujutsu

Quahog is a Go-based tool that enables managing Quilt-style patch sets using the [Jujutsu (jj)](https://github.com/martinvonz/jj) version control system. It's a port of the original `quahog` Mercurial extension.

## Installation

### Prerequisites

- [Jujutsu (jj)](https://github.com/martinvonz/jj) - The version control system
- [Git](https://git-scm.com/) - Used for patch application (similar to quahog)
- Go 1.24+ - For building from source

### Building locally

```bash
go install github.com/google/quahog/jj/cmd/quahog@latest
quahog --help
# or run directly with `go run`:
# go run github.com/google/quahog/jj/cmd/quahog@latest --help
```

## Usage

### Basic Workflow

1. **Set up a patch directory**:

   ```bash
   mkdir patches
   touch patches/series
   ```

2. **Create patches by making commits with `[PATCH]` descriptions**:

   ```bash
   # Make your changes
   echo "new content" > file.txt

   # Commit with [PATCH] prefix
   jj commit -m "[PATCH] backport-important-bugfix.diff

   This patch fixes a critical bug in the file processing."
   ```

3. **Fold commits into patch files**:

   ```bash
   quahog fold --root .
   ```

4. **Pop patch files back into commits**:
   ```bash
   quahog pop --root .
   ```

### Command Reference

#### `quahog fold`

Converts jj commits with `[PATCH]` descriptions into quilt patch files.

```bash
quahog fold --root DIR [--count NUM | --rev REVS | --all] [--to REV]
```

**Options:**

- `--root`: Directory containing `patches/` subdirectory (required)
- `--count`: Number of patches to fold (default: 1)
- `--all`: Fold all available patches
- `--rev`: Specific revision(s) to fold
- `--to`: Base commit to fold into

#### `quahog pop`

Converts quilt patch files into jj commits with `[PATCH]` descriptions.

```bash
quahog pop --root DIR [--count NUM | --all] [--from REV]
```

**Options:**

- `--root`: Directory containing `patches/` subdirectory (required)
- `--count`: Number of patches to pop (default: 1)
- `--all`: Pop all patches
- `--from`: Base commit to pop from

### Project Structure

Quahog expects the following directory structure:

```
your-project/
├── patches/
│   ├── series          # List of patch files in order
│   ├── patch1.diff     # Individual patch files
│   └── patch2.diff
└── <your project files>
```

### Patch Commit Format

Patch commits must have descriptions starting with `[PATCH]`:

```
[PATCH] filename.diff

Optional longer description
goes here and will be included
in the patch file header.
```

### Base Commits

Quahog creates base commits with `#QUAHOG` in their description to track the state of the patch set.

## See Also

- [Jujutsu VCS](https://github.com/martinvonz/jj) - The version control system
- [Quilt](https://savannah.nongnu.org/projects/quilt) - The original patch management tool
