# Data management

Everything Leak knows lives in one directory of human-editable files. This guide
covers getting data in, keeping it healthy, and getting it back when something
goes wrong.

```
~/.config/leak/
├── subscriptions.yaml   your registry (+ deletion tombstones)
├── config.yaml          your profile, defaults, and sync transport
├── fx_cache.yaml        cached exchange rates (disposable)
├── sync_state.json      device-local sync merge base (disposable)
└── backups/             timestamped snapshots
    └── 2026-08-14T09-30-00Z/
```

Point `LEAK_CONFIG_DIR` anywhere else to relocate the lot.

## Getting data in

### From a bank or card statement

The fastest way to fill the registry — no hand-entering.

```bash
leak scan statement.csv              # dry run: what looks recurring?
leak scan statement.csv --apply      # add the untracked ones
leak scan statement.csv --json       # machine-readable candidates
```

Leak groups the statement by merchant and keeps the groups that charge on a
steady cadence for a steady amount, then reports each one with a confidence
score, the detected cycle, and the projected next renewal. Nothing is written
until you pass `--apply`, and anything already in your registry is skipped.

The parser handles what real exports actually look like: metadata rows above the
header, `Withdrawal Amt`/`Deposit Amt` pairs or a single signed `Amount` column,
day-first *and* month-first dates, thousands separators, currency symbols, and
`DR`/`CR` markers. Credits and refunds are dropped.

Tuning:

```bash
leak scan statement.csv --min-occurrences 2   # short statement (default 3)
leak scan statement.csv --min-confidence 0.6  # only the strong candidates
leak scan statement.csv --tolerance 0.25      # allow ±25% price drift
leak scan statement.csv --currency USD        # statement has no currency column
```

Detection is a proposal, not a verdict. Review with `leak list` and fix any
detail with `leak edit <id>`.

#### Registry vs statement

The same scan also runs the comparison the other way — what you track against
what the statement shows — and prints a **Registry vs statement** section when
they disagree:

| Finding | Meaning | Usual fix |
|---|---|---|
| `no charge since <date>` | An active subscription appears in the statement but stopped being charged more than two cycles ago. | Probably cancelled at the provider: `leak remove <id>`. |
| `marked cancelled but charged on <date>` | You cancelled it in Leak, the provider is still billing. | Chase the provider, or `leak edit <id> --status active` if you meant to keep it. |
| `not in this statement at all` | An active subscription never shows up in a statement spanning at least two of its cycles. | Paid another way, or already gone. |

Nothing here is applied automatically. In `--json` output these are the
`mismatches` array, each with a `kind` of `stopped`, `still_charging`, or
`unseen`.

### From a file

```bash
leak import subs.csv --dry-run       # what would happen?
leak import subs.json                # add what's new, skip duplicates
leak import subs.yaml --update       # overwrite existing records too
```

Duplicates are matched on id, on name, and on normalized merchant name, so
"Netflix" and "NETFLIX.COM" are recognised as the same subscription. `--update`
keeps the existing id and Leak's own FX billing history — an import never owns
those. The registry is snapshotted before every import.

## Getting data out

```bash
leak export --format json      # the sync/LLM wire format
leak export --format csv       # spreadsheets
leak export --format yaml      # config-adjacent tooling
```

Most read commands also speak `--json`, so Leak composes with the rest of your
shell:

```bash
leak list --json | jq '.[] | select(.currency == "USD") | .name'
leak sweep --json | jq '.savings.yearly'
leak stats --json | jq '.by_category'
leak due --json --days 7
leak doctor --json | jq '.findings[] | select(.severity == "error")'
```

## Backup and restore

```bash
leak backup                    # snapshot into backups/<timestamp>/
leak backup --git              # also commit the config dir to a local git repo
leak backup list               # newest first
leak restore                   # restore the newest snapshot
leak restore 2026-08-14T09-30-00Z
leak backup prune --keep 5     # drop everything older
```

A snapshot is a plain copy of your registry files — you can read or restore one
by hand, no Leak required. Restoring snapshots the current state first, so a
restore is itself reversible.

**Automatic snapshots.** Anything bulk or destructive backs up first:
`remove --hard`, `gc --apply`, `import`, `scan --apply`, `doctor --fix`, and any
sync that changes local data. Control it in `config.yaml`:

```yaml
auto_backup: true    # snapshot before bulk/destructive changes (default)
backup_keep: 20      # retained snapshots; -1 keeps everything
```

## Health checks

```bash
leak doctor           # find problems
leak doctor --fix     # apply the safe repairs (backs up first)
leak doctor --json
```

`doctor` checks the config directory (existence, permissions, backup coverage),
the on-disk schema version, the sync transport, and every record: unknown
billing cycles or statuses, missing currencies, non-positive amounts, missing
renewal or confirmation dates, uncategorised or unknown categories, and
duplicate names.

`--fix` only applies repairs with one correct answer — a malformed billing cycle
or status, a missing currency, and directory permissions. Anything needing your
judgement (a duplicate, a suspicious amount) is reported with the command that
resolves it.

## Schema and migrations

`subscriptions.yaml` carries a `version` field. Leak migrates older files
forward on load and stamps the current version on save, so upgrading never
breaks an existing registry.

| Version | Change |
|---|---|
| 1 | versioning introduced |
| 2 | deletion tombstones (`deleted:`) for sync |

A profile missing keys (hand-edited, or written by an older release) is filled
in with defaults on load rather than read as zeros.

## Editing by hand

The files are yours. Edit `subscriptions.yaml` in any editor — then run
`leak doctor` to confirm Leak still likes what you wrote. Writes are atomic
(temp file + rename), so a crash mid-save can never truncate the registry, and
both the directory and its files are owner-only.
