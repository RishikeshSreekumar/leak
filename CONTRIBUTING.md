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

## Reporting bugs / requesting features

Open a [GitHub issue](https://github.com/RishikeshSreekumar/leak/issues) with:
- what you ran, what you expected, what happened,
- `leak --version` output and your OS/arch.

## License

By contributing, you agree that your contributions are licensed under the
[MIT License](LICENSE).
