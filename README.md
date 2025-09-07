# 🐚 Quahog: A Patch Management System

## Overview

**Quahog** is a tool that enables the management of
[Quilt-style](<https://en.wikipedia.org/wiki/Quilt_(software)>) patch sets using
modern version control systems. It provides a powerful workflow for maintaining
and applying a series of patches to a codebase.

The core idea behind Quahog is to represent each patch as a distinct commit in
your version control history. This allows you to use the powerful tools of your
VCS (like rebasing, amending, and interactive history editing) to manage your
patches.

## Implementations

This repository contains implementations of the Quahog workflow for the following version control systems:

- **[Mercurial (hg)](./hg/README.md)**
- **[Jujutsu (jj)](./jj/README.md)**

Please refer to the `README.md` file in each subdirectory for specific installation and usage instructions.

## See Also

- [Quilt](<https://en.wikipedia.org/wiki/Quilt_(software)>): A patch management tool.
