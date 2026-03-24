<h1 align="center">tasklease</h1>
Portable task envelopes keep work from getting lost when a session ends.
tasklease compiles, validates, and diffs a tiny JSON lease so the next run can resume from git state instead of starting over.

[![CI](https://github.com/ratelworks/tasklease/actions/workflows/ci.yml/badge.svg)](https://github.com/ratelworks/tasklease/actions/workflows/ci.yml)
[![Latest Release](https://img.shields.io/github/v/release/ratelworks/tasklease)](https://github.com/ratelworks/tasklease/releases)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](./LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/ratelworks/tasklease.svg)](https://pkg.go.dev/github.com/ratelworks/tasklease)

## The Problem
On Monday, one person starts a task in a clean repository and writes down a few notes in chat. On Wednesday, someone changes the repo, the original shell session is gone, and the notes no longer include the exact commit, tool access, or output path. On Friday, the next person has to guess what state the task was in before they can continue.

tasklease turns that handoff into a small JSON lease with the git revision, allowed tools, secret references, and resume checkpoint. The next person can validate the lease before they start, instead of rediscovering the same missing context. This is the same problem that `git stash` solved for local code changes.

## What It Does
Run `validate` on a bad lease and tasklease prints exactly what needs to be fixed.

```bash
tasklease validate /tmp/tasklease.example.json --repo /tmp/tasklease-repo
Git: ERROR
Issue: working tree is dirty.
Fix: Commit or stash the changes before you hand off the task.

Tools: ERROR
Issue: unsupported tools: browser.
Fix: Replace them with supported tools: git, shell, go, make, fs, or test.

Handoff: ERROR
Issue: artifact path "/tmp/tasklease-report.md" is not portable.
Fix: Use a repo-relative path without absolute prefixes or parent traversal.
```

That output is intentional: the command exits with code `1` because the lease is not safe to hand off yet.

## Non-Goals
- Not a task scheduler or queue.
- Not a networked agent broker.
- Not an LLM prompt runner.

## Getting Started
### Step 1: Install
```bash
go install github.com/ratelworks/tasklease@latest
```

### Step 2: Create Input
Create a JSON lease with the fields below.

```json
{
  "version": "v0.1.0",
  "name": "release-review",
  "task": "Review the release notes and update the handoff",
  "repo": {
    "revision": "abc123",
    "slice": "."
  },
  "toolSubset": ["git", "shell", "go"],
  "secretRefs": ["TASKLEASE_API_TOKEN"],
  "artifacts": ["reports/release.md"],
  "resume": {
    "mode": "git",
    "checkpoint": "abc123"
  }
}
```

| Field | What it means |
|---|---|
| `version` | Envelope format version. Keep it on `v0.1.0` for this release. |
| `name` | Short human label for the task lease. |
| `task` | Plain-English description of the work to do. |
| `repo.revision` | The git commit that the lease was compiled from. |
| `repo.slice` | Repo-relative slice or working area for the task. |
| `toolSubset` | The exact tools the next agent may use. |
| `secretRefs` | Secret or environment references the task expects. |
| `artifacts` | Repo-relative files the task should write back. |
| `resume.mode` | Resume strategy. Use `git` for deterministic resumes. |
| `resume.checkpoint` | Commit hash used to resume the task safely. |

### Step 3: Run It
```bash
tasklease validate lease.json --repo .
```

Example output from a lease that is intentionally unsafe:

```text
Git: ERROR
Issue: working tree is dirty.
Fix: Commit or stash the changes before you hand off the task.

Tools: ERROR
Issue: unsupported tools: browser.
Fix: Replace them with supported tools: git, shell, go, make, fs, or test.

Handoff: ERROR
Issue: artifact path "/tmp/tasklease-report.md" is not portable.
Fix: Use a repo-relative path without absolute prefixes or parent traversal.
```

### Step 4: Add to CI
```yaml
name: CI

on:
  push:
  pull_request:

jobs:
  test:
    runs-on: ubuntu-latest

    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
          cache: true
      - run: go mod download
      - run: go test -race ./...
      - run: go vet ./...
      - uses: golangci/golangci-lint-action@v6
        with:
          version: v1.62.0
          args: ./...
```

## How It Works
```text
tasklease compile
  |
  v
lease.json -----> tasklease validate -----> git state + portable paths
   |
   +------------> tasklease diff ---------> field-by-field changes
```

- `compile` normalizes flags and records the current git revision.
- `validate` checks the lease against live git state and portability rules.
- `diff` compares two leases field by field.
- The output stays deterministic for the same input and repository state.

## Feature Reference
| Feature | Description |
|---|---|
| `compile` | Create a portable lease from flags and live git data. |
| `validate` | Check a lease before handoff or resume. |
| `diff` | Compare two leases and show what changed. |

### `compile`
What it does: turns CLI flags plus git state into a normalized JSON lease.

Example:
```bash
tasklease compile --task "Review the release notes" --tool git --tool shell --artifact reports/release.md
```

How to disable: pass `--revision` and `--slice` explicitly if you do not want git auto-detection.

### `validate`
What it does: checks the lease against the current git commit, tool subset, secret refs, and artifact paths.

Example:
```bash
tasklease validate lease.json --repo .
```

How to disable: skip the command in local workflows and keep it for CI or pre-handoff checks.

### `diff`
What it does: shows field-level changes between two lease files.

Example:
```bash
tasklease diff before.json after.json
```

How to disable: do not invoke the command; it has no background side effects.

## CLI Reference
| Type | Command or Flag | Purpose |
|---|---|---|
| Command | `tasklease compile` | Compile a lease from flags and git state. |
| Command | `tasklease validate <envelope>` | Validate a lease file. |
| Command | `tasklease diff <left> <right>` | Compare two lease files. |
| Flag | `--task` | Task description for `compile`. |
| Flag | `--tool` | Repeatable tool subset entry for `compile`. |
| Flag | `--secret` | Repeatable secret reference for `compile`. |
| Flag | `--artifact` | Repeatable artifact path for `compile`. |
| Flag | `--revision` | Override git revision during `compile`. |
| Flag | `--slice` | Override repo slice during `compile`. |
| Flag | `--output` | Write compiled JSON to a file instead of stdout. |
| Flag | `--repo` | Git repository path for `compile` and `validate`. |

| Exit Code | Meaning |
|---|---|
| `0` | Success. |
| `1` | User error, such as an invalid lease or unsafe handoff. |
| `2` | System error, such as I/O or git failure. |

## Development + Contributing + License
Clone, build, and test:
```bash
git clone https://github.com/ratelworks/tasklease.git
cd tasklease
go build ./...
go test -race ./...
```

Contributing:
- Fork the repository and create a feature branch.
- Keep changes small and update the fixture tests when output changes.
- Run `go test -race ./...` and `go vet ./...` before opening a pull request.
- Document any new command-line behavior in this README.

License: tasklease is released under the MIT License.

