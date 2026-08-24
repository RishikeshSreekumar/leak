# What and why

<!-- What changes, and what problem it solves. Link the issue if there is one. -->

# How to verify

```bash
make check
```

<!-- Plus the commands a reviewer should run by hand, if any. -->

# Checklist

- [ ] `make check` passes (gofmt, vet, tests)
- [ ] Tests cover the change and stay hermetic (no network, no wall clock, no real home dir)
- [ ] Docs updated if behavior changed (`README.md`, `docs/`)
- [ ] `CHANGELOG.md` updated under **Unreleased** for user-visible changes
