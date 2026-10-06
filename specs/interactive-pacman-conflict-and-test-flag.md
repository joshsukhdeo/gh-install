# Spec: Fix Interactive Selector Terminal Collision & Add Hermetic --test Flag

## Objective
When running `ghpt install -i <repo>` (e.g. `ghpt install -i xingkongliang/skills-manager`), the terminal select menu is broken/corrupted by the background progress bar animation (`PacmanUI` or `ConveyorUI`) concurrently writing ANSI cursor sequences to stdout while `pterm`'s interactive selector is active. The solution is to firmly defer `pUI.Start()` until after the interactive selectors complete.

Additionally, to ensure safe test execution and isolation:
1. Add a `--test` CLI flag that forces the use of `state.test.json` (with `state.test.json.lock`).
2. Implement a pre-run backup safety mechanism that archives the `gh-pt` state folder using `ouch compress` (or the best available compression library, with `ouch` taking precedence) before executing test suite runs or potentially dangerous operations.
3. If an application requires a compile script OR a `manifest.json` under test, clone these files to `${fileName}.test${fileExtensionStartingWithAPeriod}` before executing. These test files will be selectively retained to support testing initial non-AI behavior and enabling iterative AI debugging of the compile script body.

## Tech Stack
- **Language**: Go 1.24+
- **CLI Framework**: `github.com/alecthomas/kong`
- **TUI & Terminal**: `github.com/pterm/pterm`, `atomicgo.dev/keyboard`
- **Testing**: Go `testing`, `github.com/stretchr/testify`
- **File & State Management**: `github.com/adrg/xdg`, `github.com/gofrs/flock`
- **Compression (Backup)**: External `ouch` command invocation (with fallbacks).

## Commands
```bash
# Build binary
make build

# Run unit & integration tests
go test -v ./...

# Format, Lint, and Tidy
make fmt
make lint
make tidy

# Manual reproduction verification with test flag
./bin/gh-pt install -i xingkongliang/skills-manager --test --dry-run
```

## Project Structure
```
cmd/
  root.go           -> Route CLI flags; handle script/manifest copy if --test is active
  update.go         -> Update runner; pass test state mode
  router.go         -> Dispatch commands with CommonInstallFlags
params/
  params.go         -> Define --test flag in CommonInstallFlags & CliParams
release/
  release.go        -> Defer pUI progress animation until after interactive selection
selector/
  interactive_selector.go -> pterm interactive single & multiselect wrappers
state/
  state.go          -> Support state.test.json path resolution when TestMode is enabled
  backup.go         -> Implement ouch compress folder backup mechanics
specs/
  interactive-pacman-conflict-and-test-flag.md -> This specification document
```

## Code Style
Follow idiomatic Go style and existing repository conventions:
```go
// Example: Safe compile script/manifest test copy helper
func copyForTest(filePath string, testMode bool) (string, error) {
	if !testMode || filePath == "" {
		return filePath, nil
	}
	ext := filepath.Ext(filePath)
	base := strings.TrimSuffix(filePath, ext)
	testPath := fmt.Sprintf("%s.test%s", base, ext)
	if err := copyFile(filePath, testPath); err != nil {
		return "", fmt.Errorf("failed to copy %s for testing: %w", filepath.Base(filePath), err)
	}
	return testPath, nil
}
```

## Testing Strategy
- Unit tests in `params/cli_integration_test.go` verifying `--test` flag parsing and propagation into execution context.
- Unit tests in `state/state_test.go` verifying `GetStatePath()` returns `state.test.json` when test mode is enabled.
- Unit tests verifying the state backup logic (executing `ouch` or fallback).
- Unit tests in `release/release_test.go` or `selector/selector_test.go` verifying that interactive selection runs without active background progress bar rendering.
- Unit test for script and manifest copying logic verifying `${fileName}.test${fileExtension}` naming and file duplication.

## Boundaries
- **Always**:
  - Keep `state.json` and real user data completely untouched when `--test` or automated tests run.
  - Retain specific `.test` files (compile scripts/manifests) selectively. This ensures testing of initial non-AI behavior simply installs deps and runs the compile script; if it fails, the AI debugs the script body (unless `--rebuild-compile-script` is passed, in which case the script is scrapped and the AI recreates it and confirms dependencies).
  - Create a compressed backup of the state directory (preferring `ouch`) before performing tests.
  - Restore terminal state, cursor visibility, and input echoing when exiting interactive prompts or stopping animations.
- **Ask first**:
  - Any alteration to default non-interactive installation workflows or output schemas.
- **Never**:
  - Start or run background animation goroutines while awaiting user input on stdin. (Initialization MUST be deferred).
  - Hardcode state paths or bypass `flock` locks on `state.test.json`.

## Success Criteria
1. Running `ghpt install -i <repo>` cleanly displays the `pterm` interactive menu without screen flicker, cursor teleportation, or overwritten lines caused by the progress bar animation.
2. The progress bar animation starts only after all interactive selections are entirely completed.
3. Adding `--test` redirects all state reads and writes to `$XDG_DATA_HOME/gh-pt/state.test.json` (and `state.test.json.lock`).
4. When `--test` is specified, `compile_script` and `manifest.json` files are automatically copied to `${fileName}.test${fileExtension}` and execution is run against the test copies.
5. The state directory is automatically backed up using an `ouch compress` archive strategy before testing runs.
6. `.test` script files are purposefully retained to allow for iterative AI debugging upon failure, supporting `--rebuild-compile-script` lifecycle behaviors.
7. All test suites (`go test -v ./...`) pass cleanly without polluting the user's real `state.json` (incorporating `xdg.Reload()` where needed for total isolation).

## Future Scope / Open Questions
1. **Containerized Test Suite:** Should we spin up a container for running the test suite to ensure absolute isolation, and how would we integrate this into the current test execution pipeline?
2. **Default Installation Paths:** Instead of installing to standard system locations, should `gh-pt` use `/usr/local/ghpt` or `~/.local/ghpt` by default so it installs to a location controlled entirely by the `gh-pt` ecosystem?
