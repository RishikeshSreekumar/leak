# Changelog

All notable changes to Leak are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Leak follows
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **`leak scan <statement.csv>`** — detects recurring charges in a bank or card
  statement and proposes them as subscriptions, with a confidence score,
  detected billing cycle, and projected next renewal. Dry run by default;
  `--apply` adds the untracked ones. The parser handles metadata rows above the
  header, debit/credit column pairs or a single signed amount column, day-first
  and month-first dates, `,`/`;`/tab delimiters, both decimal conventions
  (`1,234.56` and `1.234,56`), a UTF-8 BOM, currency symbols, `DR`/`CR` markers,
  and German/Spanish/French/Italian column names.
- **Multi-device sync** (`leak sync`) over two transports: a shared directory
  (Dropbox, iCloud, Syncthing, NAS, USB) or any git remote. Merging is
  per-record against a stored merge base, so a one-sided change fast-forwards
  and only a both-sides edit is a conflict — resolved last-writer-wins, or
  refused for the user under `--strategy manual`. Includes `sync init`,
  `status`, `pull`, `push [--force]`, `disable`, `--dry-run`, and `--auto`, plus
  a `S` key in the dashboard. The profile follows the same base rule, so
  categories and thresholds propagate without clobbering a local edit. A
  directory publish verifies the remote hasn't moved, so two devices syncing at
  once merge instead of overwriting each other.
- **Deletion tombstones** so a hard delete propagates across devices instead of
  being resurrected by the next sync. Re-creating an id clears its tombstone.
- **`leak doctor`** — checks the config directory, permissions, backup coverage,
  schema version, sync transport, and every record (unknown cycles or statuses,
  missing currencies, non-positive amounts, missing renewal/confirmation dates,
  unknown categories, duplicate names). `--fix` applies the repairs that have
  exactly one right answer, after a backup.
- **`leak backup prune --keep N`** plus automatic retention on `leak backup`.
- **Automatic pre-change snapshots** before `remove --hard`, `gc --apply`,
  `import`, `scan --apply`, `doctor --fix`, and any sync that writes locally.
  Controlled by `auto_backup` / `backup_keep` in `config.yaml`.
- **`--json` output** on `list`, `show`, `due`, `stats`, `insights`,
  `categories`, `payment-methods`, `sweep`, `scan`, `doctor`, and `sync`.
  JSON field names are a stable, snake_case contract.
- **`leak import --dry-run` and `--update`**, with duplicate matching by id,
  name, and normalized merchant name. `--update` preserves the existing id and
  Leak's own FX billing history.
- **Shell completion for subscription ids and backup names** on `show`, `edit`,
  `remove`, `mark`, and `restore`.
- Grouped `leak --help` output (Manage / Analyze / Audit / Data).
- Guides: [`docs/sync.md`](docs/sync.md) and [`docs/data.md`](docs/data.md).

### Changed

- Registry schema is now **version 2** (tombstones). Older files migrate
  forward automatically on load.
- The config directory and its files are created **owner-only** (`0700`/`0600`);
  `leak doctor --fix` tightens an existing directory.
- A `config.yaml` missing keys is filled in with defaults on load instead of
  being read as zeros.
- `store.Store.RemoveSub` takes the deletion time, so tombstones stay hermetic
  under test.

## [0.1.0]

Initial public release: local-first subscription registry, mark-and-sweep audit
(`review`/`mark`/`sweep`/`gc`), multi-currency reporting with historical FX
rates, insights, the bubbletea dashboard, CSV/JSON/YAML import and export, and
`backup`/`restore`.
