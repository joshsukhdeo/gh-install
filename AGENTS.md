# gh-pt

GitHub CLI extension for installing release binaries across Linux, macOS, Windows, and FreeBSD.

## Key Commands
```bash
make build              # Build binary
go test -v ./...        # Run test suite
make fmt && make lint   # Format and lint code
```

## Context Routing

Agents and subagents MUST read only the context file relevant to their active path:
→ cmd: cmd/AGENTS.md
→ params: params/AGENTS.md
→ release: release/AGENTS.md
→ selector: selector/AGENTS.md
→ state: state/AGENTS.md
→ config: config/AGENTS.md
→ memory: .memory/ (decisions.md, patterns.md, inbox.md)

## Core Invariants
1. **GitHub CLI Required**: `gh` CLI presence & auth required.
2. **State Locking**: Always use `state.LoadState()` / `st.Save()`; never bypass `state.json.lock`.
3. **Test Isolation**: In tests, set `XDG_DATA_HOME` via `t.TempDir()` and immediately call `xdg.Reload()`.
4. **Terminal Hygiene**: Never run background progress animations while waiting on interactive stdin.
5. **Security**: Disallow GitHub tokens in CLI arguments; read strictly from environment or config.