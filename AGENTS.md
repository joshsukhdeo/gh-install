# gh-pt

GitHub CLI extension for installing release binaries across Linux, macOS, Windows, and FreeBSD.

## Key Commands
```bash
make build              # Build binary
go test -v ./...        # Run test suite
make fmt && make lint   # Format and lint code
```

## Context Routing
→ cmd: cmd/CLAUDE.md
→ params: params/CLAUDE.md
→ release: release/CLAUDE.md
→ selector: selector/CLAUDE.md
→ state: state/CLAUDE.md
→ config: config/CLAUDE.md
→ memory: .memory/ (decisions.md, patterns.md, inbox.md)

## Core Invariants
1. **GitHub CLI Required**: `gh` CLI presence & auth required.
2. **State Locking**: Always use `state.LoadState()` / `st.Save()`; never bypass `state.json.lock`.
3. **Test Isolation**: In tests, set `XDG_DATA_HOME` via `t.TempDir()` and immediately call `xdg.Reload()`.
4. **Terminal Hygiene**: Never run background progress animations while waiting on interactive stdin.
5. **Security**: Disallow GitHub tokens in CLI arguments; read strictly from environment or config.
