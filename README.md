# ConfigManager
`cm` is a Linux service and CLI utility that monitors selected configuration
and startup files and maintains a **Git-backed history of their contents**.

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

## How it works

```
                        ┌─────────────────────┐
                        │       cm CLI        │
                        └──────────┬──────────┘
                                   │
                             Unix socket
                                   │
                                   ▼
                        ┌─────────────────────┐
                        │      cm daemon      │
                        └──────────┬──────────┘
                                   │
                     ┌─────────────┴─────────────┐
                     │                           │
                     ▼                           ▼
              ┌──────────────┐           ┌──────────────┐
              │ links/       │           │ files/       │
              │ path registry│           │ Git snapshots│
              └──────────────┘           └──────────────┘
```

- **`links/`** — a persistent registry of monitored paths (symbolic links). It
  is the authoritative list of what `cm` watches. Treat it as internal; change
  the registry only through the `cm` CLI.
- **`files/`** — the Git repository containing every snapshot. Only file
  contents are versioned and restored (no metadata, ownership, permissions,
  timestamps, ACLs, etc.).

The daemon is the sole authority for monitoring, Git operations, batching, and
restoration. The CLI only talks to the daemon over a local Unix socket (`/run/cm.sock`).

## Purpose / Capabilities

- Register files for monitoring by absolute or relative path.
- Detect **logical content changes** — identical results count as one save, not
  many noisy filesystem events.
- Detect atomic replacement (`write .tmp` + `rename`), deletion, and recreation.
- Batch all pending changes into **one Git commit** using a global one-minute
  window (the timer starts on the first change and is not restarted).
- Reconcile changes that happened while the daemon was stopped.
- Track how many saves occurred for each file between commits.
- Attach a user message to the next planned commit.
- Inspect history and diffs.
- Restore files — safely, with confirmation, without rewriting history.

## Usage

### Building

```sh
go build -o cm ./cmd/cm
```

### Install
```sh
sudo cp cm /usr/local/bin
sudo cm --install
sudo cm start
```

Default repository is (`/var/lib/cm`), you can set custom:
```sh
sudo cm --install /data/cm-repo
```
At startup the daemon initializes the Git repo, scans `links/`, and reconciles
any changes that occurred while it was stopped.

### Help

```sh
cm
```

### Register monitored files

```sh
cm add /etc/nginx/nginx.conf
cm add ./application.yml          # relative paths are resolved against CWD
cm add /etc/a.conf /etc/b.yml     # multiple files in one command
```

Each target must exist and be a regular file, and is validated up front. An
initial Git commit is created immediately for each successful file.

### List monitored files

```sh
cm ls              # files under the current directory
cm ls --all        # complete list of every monitored file
```

Lists monitored files (absolute paths with `--all`, or relative to the current
directory otherwise). Deleted monitored files are shown as `DELETED`; files
with uncommitted changes are shown with their pending state (`CHANGED`).

### Status

```sh
cm status
```

Lists every entry in the current directory and marks the ones that are
monitored by `cm`:

```text
* foo.conf
  bar.txt
  etc/
```

The `*` prefix marks a monitored file; directories have a trailing `/`.
Useful for seeing at a glance which files are under monitoring.

### History

```sh
cm history                 # last 20 changes
cm history --all           # complete history
cm history <file>          # history for one file (last 20)
cm history <file> --all    # complete history for one file
```

Each entry shows the modification date, file, changed-line summary, and short
commit id, followed by the commit message.

### Diff

```sh
cm diff                      # most recent commit
cm diff <file>               # most recent change to one file
cm diff -c <commit_id>       # a specific commit
cm diff <file> -c <commit>   # one file inside a specific commit
```

### Attach a message to the next commit

Reads from stdin until EOF:

```sh
cm message
Updated production configuration after deployment.
```

### Restore

```sh
cm restore <commit_id>            # restore affected files (interactive)
cm restore <commit_id> --all      # restore all affected files
```

Every restore requires confirmation. `cm` shows the affected files and commit
message, and lets you review the diff against the current version before
proceeding (`[d]`, `[y]`, `[n]`).

Current uncommitted changes are committed first (forced immediately) so nothing
is lost. Restore creates a **new** Git commit — history is never rewritten.


### Service installation

```sh
sudo cm --install [/path/to/repository]   # install the OS service
sudo cm start                             # start it
sudo cm stop                              # stop it
sudo cm --uninstall                       # remove it
```

`cm start`/`cm stop` control the installed operating-system service.

## Design principles

1. The filesystem is the source of **current** state.
2. Git is the source of **historical** snapshot state.
3. `links/` is the persistent registry; don't edit it manually.
4. `files/` is the Git repository.
5. Filesystem events are signals, not saves.
6. The one-minute batch window is global; one batch = one commit.
7. Monitoring follows paths, not inode identities.
8. Binary files are versioned but displayed as binary.
9. Restore never rewrites Git history and always requires confirmation.
10. Only file contents and existence are restored — not metadata.