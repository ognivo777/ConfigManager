# ConfigManager
Git-backed version history and monitoring for any configuration files on your host

## Goals and Motivation

Configuration files are often critical to the operation of a Linux system, but they are frequently changed directly on the host without any reliable history of what changed, when it changed, or why.

Git is well suited for maintaining the history of text files, but using Git directly for system configuration management is inconvenient. Configuration files are normally scattered across the filesystem, applications may replace them atomically, and manually maintaining a Git repository or copying files into it adds operational overhead.

**ConfigManager** (`cm`) is designed to provide the useful part of Git for this specific problem: automatic, local, and transparent version history of selected configuration and startup files.

The main goals are:

* **Automatic history** — detect changes to selected files and maintain their historical snapshots without requiring users to manually run `git commit`.
* **Low operational overhead** — once a file is registered, ConfigManager works in the background without changing the normal way applications and administrators edit files.
* **Meaningful commits** — group changes occurring within a short time window and record how many logical saves occurred for each file.
* **Traceability** — make it easy to answer *what changed, when did it change, and what was the state before the change?*
* **Safe restoration** — restore a previous configuration state while preserving the Git history and recording the restoration as a new change.
* **Simple local architecture** — use a local Git repository and a lightweight background service rather than requiring a central server or external infrastructure.
* **Path-based monitoring** — monitor the configuration files themselves, including files that are replaced atomically by applications.
* **Explicit scope** — monitor only the files selected by the administrator rather than attempting to manage the entire filesystem.

ConfigManager is not intended to replace full configuration-management or deployment systems such as Ansible, Puppet, or similar tools. Its purpose is narrower: **provide reliable, lightweight, automatically maintained version history for important files on an individual Linux host.**
