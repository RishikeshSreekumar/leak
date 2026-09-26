# Usage

Leak tracks your recurring subscriptions locally and helps you find and cancel
the ones you no longer use. This guide walks the full workflow. For a one-line
reference of every command, run `leak --help` or `leak <command> --help`.

## The dashboard (TUI)

Running `leak` with no arguments in an interactive terminal launches a full-screen
dashboard (tabs for your list, stats, insights, and audit). In a pipe or script
it prints help instead.

```bash
leak            # dashboard when interactive; help otherwise
leak tui        # force the dashboard
```

## Adding subscriptions

Interactive wizard (prompts for name, amount, currency, cycle, renewal, trial
end, category, payment method, URL, notes):

```bash
leak add
```

Or skip the wizard with flags — handy for scripting:

```bash
leak add --name Netflix --amount 649 --currency INR \
  --category Entertainment --cycle monthly --renewal 2026-07-21
```

- `--cycle` accepts your billing cadence (e.g. `monthly`, `yearly`).
- `--renewal` is a billing date (`YYYY-MM-DD`). It is an *anchor*: Leak rolls
  it forward by the billing cycle on every read, so a subscription added in
  January still shows its correct next charge in September. `leak doctor --fix`
  rewrites the stored date to the current one if you want the file to match.
- `--trial-ends` marks a free trial. Until that day `leak due` and `leak list`
  show the trial end instead of the renewal, because that is the day to cancel
  by. No billing record is written while a trial is running.
- `--url` is the provider's billing or cancellation page; `leak open <id>`
  launches it.
- `--currency` can differ per subscription; reports convert to your default.

### From a bank statement

Instead of typing each one in, point Leak at a CSV export from your bank or card:

```bash
leak scan statement.csv           # dry run — what looks recurring?
leak scan statement.csv --apply   # add the untracked ones
```

Full details, including the tuning flags, are in
[data management](data.md#from-a-bank-or-card-statement).

## Viewing what you pay for

```bash
leak list                 # name, amount, next charge, id
leak show <id>            # full detail for one subscription
leak stats                # monthly spend, broken down by category
leak due                  # renewals and trial ends in the next 30 days
leak due --days 7         # …in the next 7 days
leak insights             # top expenses, what's coming up, savings
leak open <id>            # open the billing/cancel page you stored
```

`leak due --quiet` prints one line, or nothing when the window is empty, and
exits 1 when something is due. Drop it in your shell rc for a login nag:

```bash
# ~/.zshrc
leak due --days 7 --quiet
```

Every one of these takes `--json` for scripting:

```bash
leak list --json | jq '.[] | select(.currency == "USD") | .name'
leak sweep --json | jq '.savings.yearly'
```

Every subscription has a short **id** (the last column of `leak list`) used by
`show`, `edit`, `remove`, `mark`, and `open`. You rarely need to type it in
full: those commands also accept the name, or any unique prefix of either, case
insensitively — `leak show net` finds Netflix.

## Editing and removing

```bash
leak edit <id>            # interactive edit
leak remove <id>          # soft-cancel (keeps history)
leak remove <id> --hard   # permanently delete the record
```

Soft-removing marks a subscription cancelled but keeps it for historical
spending reports. `--hard` erases it entirely.

## Mark & sweep: killing zombies

This is Leak's core idea — treat forgotten subscriptions like leaked memory and
garbage-collect them.

```bash
leak review               # walk active subs, confirm each one you still use
leak mark <id>            # confirm a single subscription is still wanted
leak sweep                # list "zombies": unconfirmed past the stale window
leak gc                   # dry run — show what would be cancelled
leak gc --apply           # actually cancel the zombies
```

- `review` / `mark` stamp `last_confirmed` on a subscription.
- `sweep` flags any active subscription not confirmed within `stale_after_days`
  (default 180) as a zombie, with the potential monthly savings.
- `gc` is a **dry run by default**. Add `--apply` to perform the cancellations.

A typical monthly hygiene pass:

```bash
leak review        # re-confirm what you use
leak sweep         # see the zombies + savings
leak gc --apply    # cancel them
```

## Multi-currency

Subscriptions can bill in any currency. Reports convert everything to your
`default_currency`. The exchange rate at billing time is stored on each record
(fetched from [frankfurter.dev](https://frankfurter.dev)), so historical totals
stay stable even as rates move. Offline, Leak falls back to the last known rate
and flags the figure as estimated.

## Import & export

The wire format is plain, human-readable, and LLM-friendly.

```bash
leak export                       # JSON to stdout (default)
leak export --format csv          # CSV
leak export --format yaml         # YAML
leak export --format json > subs.json

leak import subs.json             # add what's new, skip duplicates
leak import subs.csv --dry-run    # preview without writing
leak import subs.csv --update     # overwrite existing records too
```

## Backup, restore, and health

```bash
leak backup                       # snapshot the registry
leak backup list                  # newest first
leak restore                      # roll back to the newest snapshot
leak backup prune --keep 5        # trim old snapshots

leak doctor                       # check the registry and config
leak doctor --fix                 # apply the safe repairs
```

Bulk and destructive commands snapshot first, so a bad import or an over-eager
`gc --apply` is one `leak restore` away. Details: [data management](data.md).

## Data & configuration

Everything lives locally in your [config directory](installation.md#where-leak-stores-data):

- `subscriptions.yaml` — your subscriptions (human-editable).
- `config.yaml` — your profile and defaults.

Relocate it with the `LEAK_CONFIG_DIR` environment variable:

```bash
LEAK_CONFIG_DIR=~/dotfiles/leak leak list
```

### `config.yaml`

```yaml
default_currency: INR
exchange_rate_provider: frankfurter.dev
review_after_days: 90
stale_after_days: 180
auto_backup: true                 # snapshot before bulk/destructive changes
backup_keep: 20                   # retained snapshots (-1 keeps everything)
categories: [Development, Entertainment, Storage, Utilities, AI]
payment_methods: [Amex, PayPal]   # empty on a fresh install; the wizard adds as you go
sync:                             # written by `leak sync init`
  kind: dir
  target: /Users/me/Dropbox/leak
  strategy: lww
```

Manage the profile from the CLI instead of editing by hand:

```bash
leak profile                              # show current profile
leak profile currency set-default <CODE>  # set the reporting currency
leak profile currency add|remove <CODE>
leak profile category add|remove <name>
leak profile payment add|remove <name>
```

On first run the reporting currency is picked from your shell locale
(`LC_ALL` / `LC_MONETARY` / `LANG`, so `en_IN` starts in INR and `de_DE` in
EUR), falling back to USD.

## Sync across devices

Opt-in, no account required — local files stay the source of truth.

```bash
leak sync init --dir ~/Dropbox/leak        # or --git git@github.com:me/subs.git
leak sync                                  # fetch → merge → publish
leak sync status                           # what would change?
leak sync pull                             # merge in, don't publish
leak sync push                             # publish this device
leak sync disable                          # stop; local data untouched
```

Merging is per-record and backed by a stored merge base, so a one-sided change
fast-forwards and only a both-sides edit counts as a conflict. Full guide:
[sync](sync.md).

## Shell completion

```bash
leak completion zsh > "${fpath[1]}/_leak"   # bash | zsh | fish | powershell
```

Completion knows your subscription ids, so `leak show <TAB>` lists them by name
and status, and `leak restore <TAB>` lists your snapshots.

## Getting help

```bash
leak --help               # all commands, grouped by workflow
leak <command> --help     # flags for one command
leak --version            # build version
leak doctor               # is my data healthy?
```
