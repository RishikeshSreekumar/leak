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

Interactive wizard (prompts for name, amount, currency, category, cycle, renewal):

```bash
leak add
```

Or skip the wizard with flags — handy for scripting:

```bash
leak add --name Netflix --amount 649 --currency INR \
  --category Entertainment --cycle monthly --renewal 2026-07-21
```

- `--cycle` accepts your billing cadence (e.g. `monthly`, `yearly`).
- `--renewal` is the next billing date (`YYYY-MM-DD`).
- `--currency` can differ per subscription; reports convert to your default.

## Viewing what you pay for

```bash
leak list                 # all active subscriptions
leak show <id>            # full detail for one subscription
leak stats                # monthly spend, broken down by category
leak due                  # renewals in the next 30 days
leak due --days 7         # renewals in the next 7 days
leak insights             # top expenses, spending heatmap, savings ideas
leak categories           # spend grouped by category
leak payment-methods      # spend grouped by payment method
```

Every subscription has a short **id** (shown in `leak list`) used by `show`,
`edit`, `remove`, and `mark`.

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

leak import subs.json             # load subscriptions from a file
```

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
categories: [Development, Entertainment, Storage, Utilities, AI]
payment_methods: [ICICI Amazon Pay, HDFC Millennia, UPI]
```

Manage the profile from the CLI instead of editing by hand:

```bash
leak profile                      # show current profile
leak currency <CODE>              # set default currency
leak category add <name>          # add a category
leak category remove <name>       # remove a category
leak payment add <name>           # add a payment method
leak payment remove <name>        # remove a payment method
```

## Sync (designed, not yet built)

`leak sync push|pull|status` is a designed seam for a future backend — local
stays the source of truth. The JSON export format is already sync-ready
(`updated_at`/`rev` on every record). See
[`internal/sync`](../internal/sync/sync.go).

## Getting help

```bash
leak --help               # all commands
leak <command> --help     # flags for one command
leak --version            # build version
```
