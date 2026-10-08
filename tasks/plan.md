# Plan: Fix `gh-pt show` Default Behavior

## 1. Components and Dependencies
- `cmd/router.go`: Responsible for routing the `show` command.
- `cmd/show.go`: Contains `ShowInfo` (the desired default) and `handleShow` (the interactive sidecar downloader to be removed).
- `cmd/show_test.go`: Will contain the new TDD tests verifying default output and the removal of the interactive mode test.

## 2. Implementation Order (Vertical Slices)
1. **Test Update & cleanup**: Add failing TDD test to assert `show` defaults to `ShowInfo` output. Remove or migrate any tests that relied on `show` triggering an interactive download.
2. **Behavior Fix**: Modify `cmd/router.go` to route `show` to `ShowInfo` completely. 
3. **Cleanup**: Remove `handleShow` from `cmd/show.go` as it will no longer be reachable or used from the `show` command. Make sure `install` is not relying on `handleShow` from `cmd/show.go`. (I'll need to check if `install` uses it; if it does, move/refactor it).

## 3. Risks & Mitigations
- **Risk**: `handleShow` logic for downloading sidecars might be relied upon by other commands. 
  - **Mitigation**: Grep for `handleShow` before deleting it. If `install` relies on the logic, I'll extract it to a shared helper or move it to `cmd/install.go`.

## 4. Verification Checkpoints
- Run `rtk go test ./cmd/...` after step 1 (should fail).
- Run `rtk go test ./cmd/...` after step 2 and 3 (should pass).
