# Updating

Update Leak the same way you installed it. Check your current version first:

```bash
leak --version
```

Compare against the latest [release](https://github.com/RishikeshSreekumar/leak/releases).

## Homebrew

```bash
brew update
brew upgrade leak
```

## Install script

Re-running the script fetches and installs the latest release, replacing the
existing binary:

```bash
curl -fsSL https://raw.githubusercontent.com/RishikeshSreekumar/leak/main/install.sh | sh
```

Use the same `LEAK_INSTALL_DIR` you used originally if you customized it.

## Prebuilt binary (manual)

Download the newer archive from the
[releases page](https://github.com/RishikeshSreekumar/leak/releases), extract,
and overwrite the old binary on your `PATH` (see
[installation](installation.md#prebuilt-binary-manual)).

## `go install`

```bash
go install github.com/RishikeshSreekumar/leak@latest
```

Pin a specific version instead of `@latest` if you need to:

```bash
go install github.com/RishikeshSreekumar/leak@v0.1.0
```

## From source

```bash
cd leak
git pull
make build          # or: make install
```

## Your data is safe across updates

Updates only replace the binary. Your subscriptions and config in the
[config directory](installation.md#where-leak-stores-data) are untouched. The
on-disk YAML format is backward-compatible; new fields default sensibly on old
records.
