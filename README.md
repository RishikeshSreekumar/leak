# Leak

**Mark. Sweep. Save.**

[![CI](https://github.com/RishikeshSreekumar/leak/actions/workflows/ci.yml/badge.svg)](https://github.com/RishikeshSreekumar/leak/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/RishikeshSreekumar/leak?sort=semver)](https://github.com/RishikeshSreekumar/leak/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/RishikeshSreekumar/leak.svg)](https://pkg.go.dev/github.com/RishikeshSreekumar/leak)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A terminal-first, local-first subscription manager for developers. Track, audit,
and garbage-collect recurring expenses before they quietly drain your wallet.

Subscriptions are the financial equivalent of memory leaks: you sign up, forget,
and months later a zombie process is still consuming resources. Leak answers one
question exceptionally well — *what am I paying for, and what should I cancel?*

## Install

Pick whichever fits your system — full details in
[`docs/installation.md`](docs/installation.md).

```bash
# Homebrew (macOS)
brew install RishikeshSreekumar/tap/leak

# Install script (macOS / Linux) — downloads the right prebuilt binary
curl -fsSL https://raw.githubusercontent.com/RishikeshSreekumar/leak/main/install.sh | sh

# Go toolchain (any OS with Go 1.26+)
go install github.com/RishikeshSreekumar/leak@latest
```

Prebuilt binaries for Linux, macOS, and Windows (amd64 + arm64) are attached to
every [release](https://github.com/RishikeshSreekumar/leak/releases). To build
from source: `git clone` then `make build`.

Check your version any time:

```bash
leak --version
```

## Quick start

```bash
leak scan statement.csv       # find recurring charges in a bank export
leak add                      # or add one by hand (interactive wizard)
leak list                     # what am I paying for?
leak stats                    # monthly spend, by category
leak due                      # renewals + trial ends in the next 30 days
leak insights                 # top expenses, heatmap, savings
leak review                   # mark: confirm what you still use
leak sweep                    # find zombies + potential savings
leak gc --apply               # cancel the zombies
```

Power users skip the wizard with flags:

```bash
leak add --name Netflix --amount 649 --currency INR \
  --category Entertainment --cycle monthly --renewal 2026-07-21
```

Full walkthrough: [`docs/usage.md`](docs/usage.md).

## Commands

| Group | Commands |
|-------|----------|
| Manage | `add` `list` `show <id>` `edit <id>` `remove <id> [--hard]` `open <id>` `tui` |
| Analytics | `stats` `due [--days N] [--quiet]` `insights` |
| Audit | `review` `mark <id>` `sweep` `gc [--apply]` |
| Data | `scan <statement.csv>` `import <file>` `export` `backup` `restore` `doctor` `sync` `profile` |

Anywhere a command takes an `<id>`, the name or a unique prefix works too.

Read commands take `--json`, so Leak composes with `jq` and anything else:

```bash
leak list --json | jq '.[] | select(.currency == "USD") | .name'
leak sweep --json | jq '.savings.yearly'
```

Tab completion (including subscription ids) comes from cobra:

```bash
leak completion zsh > "${fpath[1]}/_leak"     # bash | zsh | fish | powershell
```

## How it works

- **Local-first.** Everything lives in `~/.config/leak/` as human-editable YAML
  (`subscriptions.yaml`, `config.yaml`). No cloud account required. Point
  `LEAK_CONFIG_DIR` elsewhere to relocate it.
- **Mark & sweep.** `review`/`mark` stamp `last_confirmed`. `sweep` flags any
  active subscription unconfirmed past `stale_after_days` (default 180) as a
  zombie. `gc` cancels them. `open <id>` jumps to the provider's cancel page.
- **Dates that stay right.** A renewal date is an anchor; Leak rolls it forward
  by billing cycle on every read, so `due` keeps working months after you added
  something. A `--trial-ends` date takes over until it passes.
- **Multi-currency.** Subscriptions bill in any currency; reports convert to
  your `default_currency`. The FX rate at billing time is stored on each record
  (via [frankfurter.dev](https://frankfurter.dev)) so historical spending stays
  stable. Offline lookups fall back to the last known rate, flagged as estimated.
- **Safe by default.** Writes are atomic, the config dir is owner-only, and
  anything bulk or destructive snapshots the registry first — one `leak restore`
  away.

## Start from your bank statement

Tracking dies if you have to hand-enter everything, so Leak reads the statement
you already have:

```bash
leak scan statement.csv           # dry run — what looks recurring?
leak scan statement.csv --apply   # add the untracked ones
```

It groups the export by merchant, keeps what charges on a steady cadence for a
steady amount, and reports each candidate with a confidence score, detected
cycle, and next renewal. It also runs the comparison the other way: tracked
subscriptions the statement stopped charging, and cancelled ones it still
charges. Real-world exports are handled: metadata rows above the
header, debit/credit column pairs or a single signed amount, day-first and
month-first dates, thousands separators, currency symbols, `DR`/`CR` markers.
Nothing is written without `--apply`, and anything you already track is skipped.

## Sync across devices

Opt-in, no account, no server of ours:

```bash
leak sync init --dir ~/Dropbox/leak            # or --git git@github.com:me/subs.git
leak sync                                      # fetch → merge → publish
leak sync status                               # what would change?
```

The wire format is the same JSON `leak export` emits, so any tool or LLM can
read it. Merging is per-record against a stored merge base, so a change on one
device fast-forwards cleanly and only a genuine both-sides edit is a conflict —
resolved by last-writer-wins, or refused for you to decide with
`--strategy manual`. Hard deletes propagate as tombstones instead of being
resurrected. Full guide: [`docs/sync.md`](docs/sync.md).

## Configuration

`~/.config/leak/config.yaml`:

```yaml
default_currency: INR
exchange_rate_provider: frankfurter.dev
review_after_days: 90
stale_after_days: 180
auto_backup: true               # snapshot before bulk/destructive changes
backup_keep: 20                 # retained snapshots (-1 keeps everything)
categories: [Development, Entertainment, Storage, Utilities, AI]
payment_methods: []             # added as you go; the default currency follows your locale
sync:
  kind: dir                     # dir | git
  target: /Users/me/Dropbox/leak
  strategy: lww                 # lww | manual
```

Not sure the registry is healthy? `leak doctor` checks the config dir, the
schema, the sync target, and every record — `leak doctor --fix` applies the
repairs that have exactly one right answer.

## Documentation

- [Installation](docs/installation.md) — every install method + PATH notes
- [Updating](docs/updating.md) — how to upgrade per install method
- [Usage](docs/usage.md) — full command walkthrough
- [Data management](docs/data.md) — import, scan, backup, restore, doctor
- [Sync](docs/sync.md) — transports, merge rules, conflicts
- [Contributing](CONTRIBUTING.md) — dev setup, conventions, hermetic tests

## Development

```bash
make test    # go test ./...
make build   # build the binary
make vet     # go vet ./...
```

Tests are fully hermetic: an injectable `Clock`, a static FX provider, and a
temp `LEAK_CONFIG_DIR` mean no network, no wall-clock, no real home directory.
Regenerate render golden files with `go test ./internal/render -update`.

## License

[MIT](LICENSE) © Rishikesh Sreekumar
