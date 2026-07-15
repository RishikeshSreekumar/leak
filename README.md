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
leak add                      # interactive wizard
leak list                     # what am I paying for?
leak stats                    # monthly spend, by category
leak due                      # renewals in the next 30 days
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
| Manage | `add` `list` `show <id>` `edit <id>` `remove <id> [--hard]` |
| Analytics | `stats` `due [--days N]` `insights` `categories` `payment-methods` |
| Audit | `review` `mark <id>` `sweep` `gc [--apply]` |
| Profile | `profile` `category add/remove` `payment add/remove` |
| Data | `import <file>` `export [--format csv\|json\|yaml]` |

## How it works

- **Local-first.** Everything lives in `~/.config/leak/` as human-editable YAML
  (`subscriptions.yaml`, `config.yaml`). No cloud account required. Point
  `LEAK_CONFIG_DIR` elsewhere to relocate it.
- **Mark & sweep.** `review`/`mark` stamp `last_confirmed`. `sweep` flags any
  active subscription unconfirmed past `stale_after_days` (default 180) as a
  zombie. `gc` cancels them.
- **Multi-currency.** Subscriptions bill in any currency; reports convert to
  your `default_currency`. The FX rate at billing time is stored on each record
  (via [frankfurter.dev](https://frankfurter.dev)) so historical spending stays
  stable. Offline lookups fall back to the last known rate, flagged as estimated.

## Configuration

`~/.config/leak/config.yaml`:

```yaml
default_currency: INR
exchange_rate_provider: frankfurter.dev
review_after_days: 90
stale_after_days: 180
categories: [Development, Entertainment, Storage, Utilities, AI]
payment_methods: [ICICI Amazon Pay, HDFC Millennia, UPI]
```

## Cloud sync (designed, not yet built)

Sync is an opt-in seam, not a requirement — local stays the source of truth. The
wire format is JSON (identical to `leak export --format json`), so any external
tool or LLM can already read your registry today. Every subscription carries
`updated_at`/`rev` so a future backend (git, object store, REST, or an MCP
server exposing subs to an LLM) can merge field-agnostically with last-writer-wins.
See [`internal/sync`](internal/sync/sync.go) for the full design.

## Documentation

- [Installation](docs/installation.md) — every install method + PATH notes
- [Updating](docs/updating.md) — how to upgrade per install method
- [Usage](docs/usage.md) — full command walkthrough
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
