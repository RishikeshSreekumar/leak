# Leak — Product Vision & Roadmap Spec

**Status:** Draft for team review
**Author:** Rishikesh S
**Last updated:** 2026-07-22

---

## 1. Vision

Leak is a **local-first, backup-able, syncable platform for subscription tracking and insights.**

It should:

1. Keep the user's subscription registry as the single source of truth, **on their machine**, in human-editable files — no mandatory cloud account.
2. Be **trivially backed up and restored**.
3. **Sync** across a user's devices as an opt-in transport, never a requirement.
4. **Import from multiple sources** so users don't hand-enter everything.
5. Help users **act on** their subscriptions (review, cancel, get reminded), not just record them.

Positioning: subscriptions are the financial equivalent of memory leaks — you sign up, forget, and a zombie process drains resources. Leak answers *"what am I paying for, and what should I cancel?"*

### 1.1 Scope decisions (resolving ambiguity in the vision)

Two terms in the vision need explicit definition before we build:

- **"Platform" does not mean a hosted backend with accounts.** Local files stay canonical. "Platform" = the CLI + TUI + an *optional* sync transport. This preserves local-first and avoids standing up multi-tenant infrastructure.
- **"Manage subscriptions through the platform" means *assisted* management, not automated cancellation at the provider.** There are no public cancel APIs, per-provider auth is brittle, and scraping carries ToS/legal risk. We deliver cancellation playbooks, deep links to billing portals, and renewal reminders. True automated cancellation is explicitly deferred and, if ever built, is opt-in browser automation only.

---

## 2. Current state (baseline)

Leak is already a shippable v1 CLI/TUI, not a prototype.

- **~5.1k LOC Go.** Clean layering: `model` → `store` (interface) → `fx` / `insights` / `audit` → `cmd` (cobra) + `tui` (bubbletea).
- 11 test files, golden-file render tests, hermetic (no network in tests).
- Data lives in `~/.config/leak/` (`subscriptions.yaml`, `config.yaml`) or `LEAK_CONFIG_DIR`. Atomic writes.
- Multi-currency with historical FX rates stored per billing record; offline fallback flagged as estimated.
- Mark-and-sweep audit: `review` / `mark` stamp `last_confirmed`; `sweep` flags zombies past `stale_after_days`; `gc` cancels them.

### 2.1 Vision pillars vs. reality

| Pillar | Status | Notes |
|---|---|---|
| Local-first | ✅ Done | YAML in config dir, atomic writes, owner-only perms, human-editable |
| Backup / restore | ✅ Done | `backup`/`backup list`/`backup prune`/`restore`, auto-snapshot before destructive changes, retention, optional git commit, schema versioning + migrations |
| Sync | ✅ Done | `internal/sync` implements dir + git transports, per-record merge against a stored merge base (`sync_state.json`), tombstones, LWW and manual strategies, `sync init/status/pull/push/disable`, `--auto` |
| Multi-source import | 🟡 Bank CSV done | `leak scan` detects recurring charges from bank/card CSV exports; email and app-store receipts still open |
| Manage subs (act on them) | 🟡 Minimal | Per-subscription `url` + `leak open`, `trial_ends` in `due`, `due --quiet` for a shell-rc reminder, statement reconcile in `scan`. No notifications daemon, no provider metadata table (deliberately) |

**Assessment:** Phases 1 and 3 have shipped, along with the bank-CSV half of
Phase 2. The remaining gaps are the other import sources (email, app-store) and
assisted management (Phase 4).

---

## 3. Roadmap

Four phases. Recommended delivery order in §3.5.

### 3.1 Phase 1 — Harden local-first + backup ✅ shipped

- **`leak backup` / `leak restore`** — timestamped snapshots of the config dir, plus `backup list`, `backup prune --keep N`, and automatic snapshots before every destructive or bulk change (`auto_backup`, `backup_keep`).
- **Git-repo transport (optional).** `leak backup --git` commits the config dir locally; the full git *sync* transport landed in Phase 3.
- **Schema `version` field on `Data`** with a `Migrate` step chain. Now at v2 (tombstones).
- **Added beyond the plan:** `leak doctor [--fix]` for registry health, owner-only file permissions, and profile normalization so partial config files behave like fresh ones.

*Exit criteria met:* back up, restore, and roll back are one command each; the on-disk schema is versioned and migrates forward.

### 3.2 Phase 2 — Multi-source import (adoption unlock) 🟡 bank CSV shipped

- **Bank/card statement CSV → subscription detector.** ✅ `leak scan` in `internal/detect`: merchant normalization that strips payment-rail noise, cadence classification (weekly/monthly/quarterly/yearly) from median inter-charge gaps, amount-stability checks, and a confidence score blending count, regularity, stability, and recency. Dry run by default; `--apply` writes.
- **Merchant → name/category mapping table.** ✅ ~40 common services map to a display name and category.
- **Improve dedup.** ✅ Imports and scans match on id, exact name, *and* normalized merchant key.
- **Still open:** email receipt scan and app-store / Play Store receipts. The parser lives behind `detect.ParseCSV`, so a second source only needs to produce `[]detect.Txn`.

*Exit criteria met for CSV:* a user points Leak at a bank export and gets a reviewed list with sensible name/category/amount/cadence.

### 3.3 Phase 3 — Real sync ✅ shipped

- **Two transports, not one.** ✅ `dir` (Dropbox/iCloud/Syncthing/NAS/USB — zero infrastructure) and `git` (any remote). Git keeps a Leak-owned clone holding a single generated file, so git never resolves a text conflict.
- **Merge.** ✅ Per-record, field-agnostic, with a stored merge base (`sync_state.json`) so a one-sided edit fast-forwards and only a both-sides edit is a conflict. LWW by default; `--strategy manual` surfaces `[]Conflict` and refuses to overwrite. Ties break on content hash, so both devices choose the same winner.
- **Deletions.** ✅ Tombstones in the registry; a re-created id clears its tombstone.
- **Safety.** ✅ Snapshot checksums, format-version gating, pre-merge backups, `--dry-run`, and a `push` that refuses to clobber an ahead remote without `--force`.
- **Still open:** `leak mcp` — expose the registry as a live MCP resource to an LLM. The snapshot format is already the JSON an LLM would read.

*Exit criteria met:* two devices converge through either transport with deterministic conflict resolution.

### 3.4 Phase 4 — Assisted management

- **Per-service metadata:** cancel URL, billing-portal link, cancellation difficulty.
- **`leak cancel <id>`** — opens the playbook + deep link, records intent.
- **Renewal reminders / notifications.**
- **Deferred:** true automated cancellation. Revisit only as opt-in browser automation.

*Exit criteria:* for a flagged zombie, the user gets a one-command path to the provider's cancel flow plus a reminder.

### 3.5 Sequencing

Planned: **Phase 1 → Phase 2 (bank CSV only) → Phase 3**. That is what shipped,
with sync growing a second (directory) transport because it needs no
infrastructure at all and covers the majority of "my laptop and my desktop" use.

Next, in order: Phase 4 (assisted management), `leak mcp`, then the remaining
import sources once real users say which ones they need.

---

## 4. Non-goals (for now)

- Hosted multi-tenant backend or user accounts.
- Automated cancellation at the provider side.
- Personalized financial/investment advice.
- Storing sensitive payment data — Leak stores payment-method *labels* only (e.g. "ICICI Amazon Pay"), never card numbers.

---

## 5. Open questions for the team

1. ~~**Primary sync transport**~~ — resolved: shipped `dir` and `git`. A REST/object-store backend can implement `sync.Transport` later without touching the merge engine.
2. **Bank CSV formats** — the parser adapts to column synonyms and both date orders, but accuracy still depends on real statements. Which banks/cards should we collect samples for?
3. **Email import** — is read-only inbox scanning in scope, and via what (local mbox, IMAP, Gmail API)? Privacy implications are significant.
4. **Distribution** — stay CLI/TUI-only, or is a GUI/mobile companion on the horizon? Affects how much we invest in the TUI.
5. **"Platform" branding** — are we comfortable defining it as CLI + optional sync (per §1.1), or does leadership expect a hosted service?

---

## 6. Risks

- **Import accuracy.** A recurring-charge detector that mislabels one-off purchases as subscriptions erodes trust fast. Needs a human review step and real test data.
- **Scope creep on "manage."** Automated cancellation is a tar pit; keep it out of the committed roadmap.
- **Sync conflict edge cases.** LWW can silently lose edits; the `--strategy manual` path must be real, not a stub.
- **Email/privacy.** Any inbox access is a trust and compliance surface; treat as its own project with explicit consent.
