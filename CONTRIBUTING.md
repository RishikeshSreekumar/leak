# Contributing to Leak

Thanks for your interest in improving Leak! This project is small, terminal-first,
and local-first — contributions that keep it fast, hermetic, and dependency-light
are especially welcome.

## Getting started

```bash
git clone https://github.com/RishikeshSreekumar/leak.git
cd leak
make build      # go build -o leak .
make test       # go test ./...
make vet        # go vet ./...
```

Requires **Go 1.26+** (see `go.mod`).

## Development workflow

1. **Fork** the repo and create a topic branch off `main`:
   `git checkout -b fix/short-description`
2. Make your change with matching tests.
3. Run the full gate locally before pushing:
   ```bash
   make fmt      # gofmt -w .
   make vet
   make test
   ```
4. Open a pull request against `main`. CI runs `go test ./...` and `go vet ./...`
   on every PR — keep it green.

## Project layout

```
cmd/                cobra command tree (one file per command group)
internal/model/     domain types + on-disk schema and migrations
internal/store/     YAML persistence behind a Store interface
internal/insights/  aggregation for stats/insights
internal/audit/     mark-and-sweep (zombies, savings)
internal/detect/    bank-statement parsing + recurring-charge detection
internal/sync/      snapshots, merge engine, dir/git transports
internal/backup/    timestamped snapshots, retention, git commit
internal/render/    data → terminal strings (golden-tested)
internal/tui/       bubbletea dashboard
internal/fx/        exchange rates (HTTP + cache + static test provider)
```

Dependencies point one way: `cmd` and `tui` sit on top, `model` sits at the
bottom. If a change makes a lower layer import a higher one, it belongs
somewhere else.

## Coding conventions

- **Format** with `gofmt` (`make fmt`) — CI assumes formatted code.
- **Commit messages** follow [Conventional Commits](https://www.conventionalcommits.org):
  `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:` … Keep the subject
  under ~50 chars; explain the *why* in the body when it isn't obvious.
- Prefer reusing existing helpers over adding dependencies. New third-party
  modules need justification in the PR description.

## Tests are hermetic — keep them that way

Leak's tests never touch the network, the wall clock, or your real home
directory:

- an injectable `Clock` (`internal/clock`) replaces `time.Now`,
- a static FX provider replaces live exchange-rate lookups,
- a temp `LEAK_CONFIG_DIR` replaces `~/.config/leak`.

Any new code that reads time, money rates, or the filesystem must accept these
seams so tests stay deterministic and offline.

### Golden files

Render output is snapshot-tested. If you intentionally change rendered output,
regenerate the golden files and commit them:

```bash
go test ./internal/render -update
```

Review the diff before committing — an unexpected golden change usually means a
real regression.

### Sync and statement tests

Two areas have their own conventions:

- **Sync** (`internal/sync`) — tests wire two `device`s over a real
  `DirTransport` in temp directories and assert they converge. The git transport
  test creates a bare repo locally and skips when git is absent. Never reach for
  the network.
- **Statement detection** (`internal/detect`) — add a fixture that mirrors the
  real export shape you hit (header preamble, column names, date order, sign
  convention). If your bank's format doesn't parse, a failing test with a
  redacted sample is the most useful bug report you can send.

## Good first contributions

- A statement format Leak mis-parses — see `internal/detect/parse.go`; new
  column synonyms and date layouts are one-liners.
- A merchant missing from the name/category table in
  `internal/detect/detect.go`.
- A `leak doctor` check for a mistake you actually made.
- A new sync transport: implement `sync.Transport` (two methods) and register it
  in `sync.NewTransport`.

## Reporting bugs / requesting features

Open a [GitHub issue](https://github.com/RishikeshSreekumar/leak/issues) with:
- what you ran, what you expected, what happened,
- `leak --version` output and your OS/arch.

## License

By contributing, you agree that your contributions are licensed under the
[MIT License](LICENSE).
