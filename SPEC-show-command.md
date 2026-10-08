# Spec: Fix `gh-pt show` Default Behavior

## Objective
The `ghpt show <repository>` command currently defaults to launching an interactive file download prompt for sidecars instead of displaying information about the repository. It should default to showing repository information (versions, assets, description, readme) when no flags are provided, fixing this broken behavior.

## Tech Stack
- Go 1.27
- CLI Framework (custom)

## Commands
- Build: `make build`
- Test: `rtk go test ./cmd/... -run TestShow`

## Project Structure
- `cmd/router.go`: Defines how the CLI commands are routed.
- `cmd/show.go`: Contains `ShowInfo` (information display) and `handleShow` (interactive sidecar file download).
- `cmd/show_test.go`: Unit tests for the show command.

## Code Style
Idiomatic Go, matching the current project conventions.

## Testing Strategy
We will use TDD.
1. Write a failing test in `cmd/show_test.go` asserting that `RunCommand("show", cli)` triggers `ShowInfo` (by checking stdout or mocked behavior) and does NOT trigger the interactive download prompt.
2. Fix `cmd/router.go` (and possibly `cmd/show.go`) to ensure `show <repository>` correctly calls `ShowInfo`.
3. Verify the test passes.

## Boundaries
- Always: Ensure tests pass and the behavior correctly defaults to displaying repo info.
- Ask first: If modifying the flag struct in `params/params.go` is required.
- Never: Remove existing interactive capabilities, they should just be gated behind a flag or a different command (e.g. `ghpt install --interactive` or `ghpt show --interactive`). Wait, the user said it should show info by default, not that the interactive prompt should be removed entirely, but perhaps it shouldn't be the default.

## Success Criteria
- Running `ghpt show user/repo` without additional flags displays versions, assets, description, and readme (using `ShowInfo`).
- Running `ghpt show user/repo` without additional flags does NOT launch the interactive file prompt.
- The interactive download behavior is entirely removed from the `show` command (it should only be in `install`).
- Automated tests verify this default behavior.

## Open Questions
- None.
