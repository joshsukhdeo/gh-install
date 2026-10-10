# Spec: Add `--no-ai-container` Flag for `gh-pt source` and Sync Autocomplete / Help

## Objective
Implement an explicit `--no-ai-container` flag (env: `GH_PT_NO_AI_CONTAINER` / `GHPT_NO_AI_CONTAINER`) for `gh-pt source` and execution flags.
This is **not** an alias. It specifically controls Stage 1 and repair AI execution: when set, the AI runs directly on the host machine using `runAIAgentWithOutput` rather than inside an isolated container (`runAIAgentInContainer`). A prominent warning will be displayed alerting the user that running unvetted AI scripts directly on the host is UNSAFE.

Additionally:
- Purge any out-of-date completion entries.
- Ensure `gh-pt source --help` and global `--help` are up-to-date with `--no-ai-container`.
- Keep documentation and specifications updated in parallel.

## Tech Stack
- Go 1.27.0
- `alecthomas/kong` CLI framework & `willabides/kongplete` completion
- `github.com/posener/complete`

## Commands
- Build: `make build` (or `go build -v -o ./gh-pt .`)
- Test: `go test -v ./...`
- Lint: `make fmt && make lint`

## Project Structure
- `params/params.go`:
  - Add `NoAIContainer bool` to `ExecutionFlags` with tag `name:"no-ai-container" env:"GH_PT_NO_AI_CONTAINER" help:"Run AI generation directly on the host machine without container isolation (UNSAFE)."`
- `cmd/root.go`:
  - In `handleCompileFromSource`: Check `r.NoAIContainer || os.Getenv("GHPT_NO_AI_CONTAINER") == "1"`.
  - When enabled, log prominent security warning:
    `log.Warn("*** [SECURITY: AI_SANDBOX_BYPASS] Executing AI generation directly on host without container isolation (--no-ai-container specified). Host compromise risk! ***")`
  - In Stage 1 body generation & repair loops, call `runAIAgentWithOutput` directly on the host rather than `runAIAgentInContainer`.
- `cmd/autocomplete.go`:
  - Validate all keys in `ConfigKeys` against `config/config.go`.
- `cmd/sandbox_test.go` & `cmd/autocomplete_test.go`:
  - Verify flag parsing, behavior, and autocomplete prediction.
- `docs/SPEC-SOURCE-REPOS.md`:
  - Document `--no-ai-container` under Stage 1 security isolation and threat model.

## Code Style
- Idiomatic Go with explicit error checking and logging.
- Preserve all existing comments and documentation invariants.
- Strict security notices marked with `[SECURITY: AI_SANDBOX_BYPASS]`.

## Testing Strategy
- Unit test in `cmd/sandbox_test.go` testing `NoAIContainer` flag defaults and setting.
- Unit test in `cmd/autocomplete_test.go` testing flag autocomplete.
- Run full test suite `go test -v ./...`.

## Boundaries
- **Always:**
  - Default to running AI in container when `--no-ai-container` is false and container runtime exists.
  - Emit loud security warning when running on host.
- **Never:**
  - Conflate `--no-ai-container` with `--no-compile-container`. They govern Stage 1 (AI script generation) vs Stage 2 (build execution) respectively.

## Success Criteria
1. `gh-pt source <repo> --no-ai-container` runs Stage 1 directly on host and displays the security warning.
2. `--no-ai-container` appears in `gh-pt source --help`.
3. Out-of-date completions are cleaned up.
4. All tests pass with 0 regressions.
