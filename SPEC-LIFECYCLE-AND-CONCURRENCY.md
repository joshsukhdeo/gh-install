# Spec: State Concurrency, Batch Resilience, and Directory Hygiene

## Objective
Harden `gh-pt` against data corruption, API rejection, and filesystem clutter by implementing:
1. **Transactional State Locking (`state.Mutate`)**: Eliminate read-modify-write lost update races across parallel installs/updates.
2. **GraphQL Chunking in `cmd/update.go`**: Prevent query complexity rejections by partitioning batched repository release queries into chunks of 30.
3. **Incremental Update Checkpoints**: Persist state per completed application update so SIGINT/crashes don't desynchronize disk and state.
4. **Bottom-Up Empty Directory Pruning**: Clean up empty parent directories left behind after sidecar deletion.

## Tech Stack
- Go 1.27.1 / Go standard library
- `github.com/gofrs/flock` (existing dependency for state locking)
- `github.com/cli/go-gh/v2/pkg/api` (existing GitHub GraphQL client)
- `github.com/stretchr/testify` (testing)

## Commands
```bash
Build: make build
Test:  go test -v ./...
Lint:  make lint
Fmt:   make fmt
```

## Project Structure
```
state/
  state.go           → Add Mutate(func(st *State) error) error with lock held through read + write
  state_test.go      → Concurrent stress test verifying 0 lost updates
cmd/
  update.go          → Chunk FetchLatestReleaseTags in batches of 30; incremental state checkpointing
  update_test.go     → Test chunking logic and incremental update recovery
  state_mgmt.go      → Prune empty parent directories bottom-up up to basePrefix
  state_mgmt_test.go → Test directory pruning on sidecar uninstall
```

## Code Style
Idiomatic Go, minimalist Ponytail ladder (no gratuitous abstractions, lock scoped strictly to mutation callback):

```go
// Mutate acquires the file lock, loads fresh state from disk, invokes fn,
// and saves the updated state atomically before unlocking.
func Mutate(fn func(st *State) error) error {
	path := GetStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	lock := flock.New(path + ".lock")
	if err := lock.Lock(); err != nil {
		return err
	}
	defer func() { _ = lock.Unlock() }()

	st, err := LoadState()
	if err != nil {
		return err
	}
	if err := fn(st); err != nil {
		return err
	}
	return st.saveUnlocked()
}
```

## Testing Strategy
- **Framework**: `testing` + `testify/assert` + `testify/require`.
- **Seams Under Test**:
  1. `state.Mutate`: Test 10 concurrent goroutines concurrently mutating different apps; verify all 10 apps persist in `state.json` without dropped updates.
  2. `FetchLatestReleaseTags`: Test batching of 75 mock repos into chunks of 30 without exceeding batch limits.
  3. `cmd/state_mgmt.go:RemoveApp`: Test that removing a deeply nested sidecar (`~/.local/share/app/nested/asset.json`) prunes `nested/` and `app/`, but leaves `~/.local/share` intact.

## Boundaries
- **Always do**: Test before commit, use `os.Lstat` for filesystem existence checks, run `make lint` and `go test ./...`.
- **Ask first**: Altering `state.json` schema version or changing default concurrency limit.
- **Never do**: Drop error checks on file unlock/write, commit untested concurrency primitives.

## Success Criteria
1. `state.Mutate` completely prevents lost updates under concurrent goroutine stress (`go test -race ./state/...`).
2. Repositories in `FetchLatestReleaseTags` are partitioned into batches of 30 or fewer per query.
3. If an update run is interrupted or fails midway, completed updates are already persisted in `state.json`.
4. Empty parent directories left by deleted sidecars are removed up to the base target boundary.
5. All 593+ existing tests pass, plus new concurrency and pruning unit tests.

## Architectural Decisions (Locked)
1. **Backward Compatibility**: `state.AddApp()` and `state.SetInstallMap()` are routed through `state.Mutate()` to ensure legacy callers never bypass transactional file locks.
2. **Directory Pruning Stop-Boundaries**: Pruning walks parent directories upwards and stops at non-empty directories or protected base roots (`$XDG_DATA_HOME`, `~/.local/share`, `~/.local/lib`, `~/.local/bin`, `/usr/local/*`, or the filesystem root).
3. **GraphQL Chunk Size**: `FetchLatestReleaseTags` chunks queries into slices of at most 30 repositories per request.
4. **Incremental Checkpointing**: Successful worker updates commit state immediately via `state.Mutate()` rather than waiting for the entire batch to complete.

