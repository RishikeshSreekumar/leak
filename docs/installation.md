# Installation

Leak ships as a single self-contained binary — no runtime, no dependencies.
Pick whichever method suits your platform.

| Method | Platforms | Needs Go? | Auto-update path |
|--------|-----------|-----------|------------------|
| [Homebrew](#homebrew) | macOS | no | `brew upgrade` |
| [Install script](#install-script) | macOS, Linux | no | re-run script |
| [Prebuilt binary](#prebuilt-binary-manual) | Linux, macOS, Windows | no | download newer |
| [`go install`](#go-install) | any | yes (1.26+) | `go install ...@latest` |
| [From source](#from-source) | any | yes (1.26+) | `git pull && make build` |

Verify any install with:

```bash
leak --version
```

## Homebrew

macOS, via the project's tap:

```bash
brew install RishikeshSreekumar/tap/leak
```

This taps `RishikeshSreekumar/homebrew-tap` and installs the `leak` cask.
Homebrew puts the binary on your `PATH` automatically. On Linux, use the
[install script](#install-script) or a [prebuilt binary](#prebuilt-binary-manual)
instead.

## Install script

Downloads the correct prebuilt binary for your OS/arch from the latest GitHub
release, verifies its checksum, and installs it:

```bash
curl -fsSL https://raw.githubusercontent.com/RishikeshSreekumar/leak/main/install.sh | sh
```

Defaults to installing into `/usr/local/bin` (may prompt for `sudo`). Override
the destination:

```bash
curl -fsSL https://raw.githubusercontent.com/RishikeshSreekumar/leak/main/install.sh | LEAK_INSTALL_DIR="$HOME/.local/bin" sh
```

Make sure the target directory is on your `PATH`.

> Prefer to read before you pipe to a shell? Download `install.sh`, inspect it,
> then run `sh install.sh`.

## Prebuilt binary (manual)

Every [release](https://github.com/RishikeshSreekumar/leak/releases) attaches
archives for:

- **Linux** — `amd64`, `arm64`
- **macOS** — `amd64` (Intel), `arm64` (Apple Silicon)
- **Windows** — `amd64`, `arm64`

Steps:

1. Download the archive matching your OS/arch (e.g.
   `leak_<version>_darwin_arm64.tar.gz`).
2. (Optional) verify it against `checksums.txt` in the same release:
   ```bash
   sha256sum -c checksums.txt --ignore-missing
   ```
3. Extract and move the binary onto your `PATH`:
   ```bash
   tar -xzf leak_*_linux_amd64.tar.gz
   sudo mv leak /usr/local/bin/
   ```

On Windows, extract the `.zip` and place `leak.exe` in a directory on your
`PATH`.

## `go install`

Any platform with the Go toolchain (1.26+):

```bash
go install github.com/RishikeshSreekumar/leak@latest
```

The binary lands in `$(go env GOBIN)` (or `$(go env GOPATH)/bin`). Add that to
your `PATH` if it isn't already:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

## From source

```bash
git clone https://github.com/RishikeshSreekumar/leak.git
cd leak
make build          # produces ./leak
sudo mv leak /usr/local/bin/   # optional
# or: make install  # runs `go install .`
```

## Where Leak stores data

Leak is local-first. On first run it creates a config directory:

- **Linux:** `~/.config/leak/`
- **macOS:** `~/Library/Application Support/leak/`
- **Windows:** `%AppData%\leak\`

Override the location with the `LEAK_CONFIG_DIR` environment variable. See
[usage](usage.md) for what lives there.

## Uninstall

- Homebrew: `brew uninstall leak`
- Manual / script / `go install`: delete the `leak` binary from your `PATH`.
- Remove data (optional): delete the config directory above.
