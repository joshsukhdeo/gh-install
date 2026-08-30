# GEMINI.md

## Project Overview
`gh-install` is a GitHub CLI extension for installing release binaries from GitHub repositories across Linux, macOS, Windows, and FreeBSD. It supports both interactive and non-interactive workflows, checksum verification, distro/hardware detection, packaging formats (deb, rpm, pkg, appimage, flatpak, snap), archive extraction (`mholt/archiver/v4`), and atomic state management (`state.json` via `flock`).

## Key Commands & Workflow
```bash
# Build
make build
# or: go build -v -o gh-install .

# Run Tests
go test -v ./...

# Format & Lint
make fmt
make lint
make tidy
```

## Architecture & Codebase Map
- `main.go`: Validates `gh` presence, initializes config, parses arguments with `alecthomas/kong`.
- `cmd/`:
  - `root.go`: Command routing (`RootCLI.Run()`), default install type heuristics, distro detection (`/etc/os-release`, `ID_LIKE`), and hardware acceleration detection (NPU `/sys/class/accel`, GPU `/dev/dri`).
  - `state_mgmt.go`: State inspection, removal (`--rm-saved-state`), uninstallation logic per package manager (`dpkg -r`, `rpm -e`, `pacman -R`, `pkg delete`, or `os.Remove`), and pin management (`--pin`).
  - `update.go`: Update runner (`-U`/`-u`), replaying installation params from state entries while preserving pin rules.
- `params/params.go`: Kong struct tag definitions for all CLI flags, environment variable bindings (`GH_INSTALL_*`), and custom types.
- `release/`:
  - `release.go`: GitHub REST client (`cli/go-gh/v2`), asset matching, checksum discovery & verification (SHA-256 / SHA-512), extraction, and permission management.
  - `name_cleaner.go`: OS/architecture/version affix removal heuristics for normalized binary renaming (`GenerateCleanName`).
- `selector/`:
  - `selector.go`: Regex builder (`buildRegexFromTypes`) and priority asset filtering.
  - `interactive_selector.go`: TUI/interactive prompts using `pterm`.
- `state/state.go`: XDG data store (`$XDG_DATA_HOME/gh-install/state.json`) with file-locking (`flock` on `state.json.lock`).
- `config/config.go`: XDG configuration parsing (`$XDG_CONFIG_HOME/gh-install/config.yml`).

## Critical Implementation Rules & Gotchas
1. **GitHub CLI Prerequisite**: Authentication and presence of `gh` CLI is mandatory.
2. **State & File Locking**: Always operate through `LoadState()` and `Save()`. Never bypass `state.json.lock`.
3. **Deletions on State Removal**: `RmState` uninstalls tracked packages or removes target binary files directly from disk.
4. **Testing Before Completion**: Run `go test -v ./...` and `make lint` on any Go codebase modifications.
