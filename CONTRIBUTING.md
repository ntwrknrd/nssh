# Contributing to nssh

## Quick Start

```bash
# Fork on GitHub, then clone your fork
git clone https://github.com/YOUR_USERNAME/nssh.git
cd nssh

# Build for current platform
make build

# Cross-compilation
make darwin-arm64
make darwin-amd64
make linux-amd64
make linux-arm64
```

## Guidelines

- **Secrets**: Wrap provider-resolved passwords in `*secret.Secret`. Access bytes only through `secret.Use()` and destroy secrets when done.
- **Layering**: credential providers, agent runtime, inventory, connect, and SSH packages have explicit boundaries. Keep shared host and credential resolution in `internal/connect`.
- **Exit codes**: Return errors via `internal/exit` (2=connection, 3=auth, 4=host not found, 126=not executable, 127=not found) for scripting consistency.
- **UI**: Route user-facing output through `internal/ui/` for consistent styling.

## References

- [Architecture](docs/ARCHITECTURE.md) - package layering, provider credentials, agent runtime, PTY connector design
- [GoDocs](https://pkg.go.dev/github.com/ntwrknrd/nssh/internal) - package reference

## Testing Your Changes

**Reinstalling after changes:**
```bash
nssh self reinstall --dev
```

**Running tests:**
```bash
make test    # runs vet + fmt + tests
```

**Update CLI help snapshots (after changing command flags/help text):**
```bash
go test ./cmd/nssh -args -update-snapshots
```

**Benchmark connections (results saved to ~/.local/share/nssh/benchmarks/):**
```bash
nssh self bench ssh <host>
nssh self bench scp <host>

# Compare before/after
diff ~/.local/share/nssh/benchmarks/ssh-previous.txt ~/.local/share/nssh/benchmarks/ssh-latest.txt
```

**Quick iteration without installing:**
```bash
go run ./cmd/nssh <command>
```

**Debugging:**
```bash
nssh -v <command>        # verbose logging
NSSH_DEBUG=1 nssh ...    # timing markers to stderr
```

## Opening a PR

1. Test your changes locally
2. Push to your fork
3. Open a PR against `main`
4. Request review from 'ntwrknrd'
