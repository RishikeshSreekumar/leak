# Sync

Leak syncs your registry across devices without a hosted backend, an account, or
a subscription of its own. Your files stay canonical; sync is opt-in and no core
command depends on it.

```bash
leak sync init --dir ~/Dropbox/leak      # or: --git git@github.com:me/subs.git
leak sync                                # fetch → merge → write → publish
leak sync status                         # what would change? (writes nothing)
```

## Choosing a transport

| Transport | Setup | Good for |
|---|---|---|
| `--dir <path>` | none | Dropbox, iCloud Drive, Google Drive, Syncthing, a NAS mount, a USB key |
| `--git <remote>` | a repo you own | version history, private GitHub/GitLab repos, servers you already have |

Both move the same file: `leak-snapshot.json`, the exact shape
`leak export --format json` produces. Anything that can read JSON — a script,
another tool, an LLM — can read your registry straight from the remote.

The transport lives in `config.yaml` under `sync:` and **never travels in a
snapshot**, so one machine can sync through Dropbox while another uses git.

### Directory

```bash
leak sync init --dir ~/Dropbox/leak
```

Leak writes the snapshot atomically (temp file + rename), so the folder's own
sync daemon never sees a half-written file. A sync that changes nothing skips
the write entirely, so idle runs don't trigger re-uploads.

### Git

```bash
leak sync init --git git@github.com:me/subscriptions.git
```

Leak keeps its own clone under `<config>/sync-repo` holding one generated file.
Merging happens in Leak's record-level engine, so **git never has to resolve a
conflict** — you will never see a conflict marker in your registry. Credential
prompts are disabled, so a bad key fails fast instead of hanging a script.

Any git remote works, including a bare repo on a machine you control:

```bash
git init --bare -b main /srv/leak.git
leak sync init --git /srv/leak.git
```

## Commands

```bash
leak sync                  # full cycle: fetch, merge, write locally, publish
leak sync --dry-run        # preview the cycle, write nothing
leak sync status           # same as --dry-run, plus last-synced time
leak sync status --json    # machine-readable, including every divergence
leak sync pull             # merge the remote into this device, don't publish
leak sync push             # publish this device's registry
leak sync push --force     # overwrite the remote (skips the safety check)
leak sync disable          # stop syncing; local data is untouched
```

`push` refuses when the remote holds changes this device has not merged —
pull first, or pass `--force` if you really mean to overwrite.

## How merging works

Every subscription carries `updated_at` and `rev`, bumped on every mutation.
Leak also keeps a **merge base** in `sync_state.json`: the content of each
record as of your last successful sync. The base is what separates a
fast-forward from a real conflict.

| Situation | Result |
|---|---|
| Only the remote changed a record | remote wins — no conflict |
| Only this device changed it | local wins, queued for the remote |
| Both changed it since the last sync | **conflict** (see strategies below) |
| Record exists on one side only | it is added |
| Record was hard-deleted | the deletion propagates via a tombstone |
| Profile (currencies, categories, thresholds) | whole-file, same base rule: adopted only if this device hasn't edited its own since the last sync |

Deletions are real: `leak remove --hard` writes a tombstone into
`subscriptions.yaml`, so the record stays deleted instead of being resurrected
by the next sync. Re-adding the same id afterwards clears the tombstone — a
deliberate resurrection sticks.

Two devices always converge on the same result: when timestamps and revisions
tie exactly, the winner is chosen by content hash, not by who synced last.

### Conflict strategies

```bash
leak sync init --dir ~/Dropbox/leak --strategy lww      # default
leak sync init --dir ~/Dropbox/leak --strategy manual
```

- **`lww`** (last-writer-wins) — the newer `updated_at` wins. Divergences are
  still reported so you can see what happened.
- **`manual`** — a genuinely diverged record is never overwritten. Leak keeps
  your local version and stops with the ids that need a decision:

  ```
  leak: 1 record(s) changed on both devices: netflix — inspect with
  `leak sync status --json`, edit the version you want to keep
  (`leak edit <id>`), then re-run
  ```

  Editing the record bumps its `updated_at`, which resolves the divergence on
  the next run.

## Safety

- Every merge that changes local data takes a **backup snapshot first** (unless
  `auto_backup: false`). If a sync ever does something you didn't want:
  `leak restore`.
- A shared folder has no locking, so Leak checks that the snapshot it merged
  from is still the one on disk before writing. If another device published in
  the meantime, its work is merged in and the cycle retried instead of being
  overwritten. Git gets this from its own non-fast-forward rejection.
- Snapshots carry a content checksum. A truncated or corrupted remote file is
  rejected rather than merged.
- A snapshot older than this version of Leak is accepted; a *newer* format is
  refused with an upgrade hint.
- Leak stores payment-method **labels** ("ICICI Amazon Pay"), never card
  numbers, so nothing sensitive travels. `notes` is free text you control — if
  you keep secrets there, don't sync it.

## Automatic sync

```bash
leak sync init --dir ~/Dropbox/leak --auto
```

With `--auto`, every mutating command (`add`, `edit`, `remove`, `mark`,
`review`, `gc --apply`, `import`, `scan --apply`) syncs afterwards. Failures are
reported but never fail the command that triggered them — being offline must not
make `leak add` look broken.

## Troubleshooting

```bash
leak doctor                # checks the transport, the target, and last-synced age
leak sync status --json    # exact per-record divergences
```

| Symptom | Cause |
|---|---|
| `sync is not configured` | run `leak sync init` first |
| `no snapshot at the remote yet` | first sync — this device seeds it |
| `git not found on PATH` | install git, or use `--dir` |
| `remote has changes not in the local registry` | run `leak sync pull` (or `push --force`) |
| `snapshot checksum mismatch` | the remote file is corrupted; `leak sync push --force` republishes from this device |
