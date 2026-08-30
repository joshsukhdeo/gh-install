# gh-install Agent Guide

## Project Overview
`gh-install` is a GitHub CLI extension for installing release binaries from GitHub repositories. It supports both interactive and non-interactive modes, handling asset matching, archive extraction, dependency resolution, and state management for updates.

## Essential Commands
- **Build**: `make build` or `go build -v -o gh-install .`
- **Test**: `go test ./...`
- **Lint**: `make lint` (requires `golangci-lint`)
- **Format**: `make fmt`
- **Tidy**: `make tidy`

## Architecture & Control Flow
1. **Entry Point** (`main.go`): Validates `gh` CLI presence, loads config, parses CLI args via `kong`.
2. **Command Routing** (`cmd/root.go`): The `RootCLI` type is an alias to `params.CLI`. The `Run()` method routes to state management, updates, or installation logic.
3. **Release Handling** (`release/release.go`): Interacts with GitHub API to find releases, match assets via regex, and extract/install binaries.
4. **State Management** (`state/state.go`): Tracks installed apps in XDG data directory (`state.json`) using file locking (`flock`) for concurrency safety.
5. **Configuration** (`config/config.go`): Loads user preferences from XDG config directory (`config.yml`).

## Key Patterns & Conventions
- **CLI Framework**: Uses `alecthomas/kong` for argument parsing. Flags are defined in `params/params.go` with struct tags. The `RootCLI` type in `cmd/root.go` is a type alias to `params.CLI`.
- **Interactive UI**: Uses `pterm` for confirmations, text input, and multi-select lists.
- **Logging**: Uses `zerolog` with console/JSON formats. Log level is configurable via `-l`.
- **GitHub Integration**: Uses `cli/go-gh/v2` for REST client and `gh.Exec` for shell commands.
- **Archiving**: Uses `mholt/archiver/v4` for extracting various archive formats (tar.gz, zip, etc.).
- **XDG Compliance**: Config at `$XDG_CONFIG_HOME/gh-install`, State at `$XDG_DATA_HOME/gh-install`.
- **Environment Variables**: All flags can be set via env vars with prefix `GH_INSTALL_` (configurable via `GH_INSTALL_ENV_PREFIX`).

## Critical Gotchas & Non-obvious Details

### Prerequisites & Validation
- **gh CLI Required**: The tool validates `gh` CLI presence in `main.go` before anything else. It must be installed and authenticated.
- **Repository Format**: Must be `owner/repo` format, validated in `RootCLI.Validate()`.

### State Management
- **File Locking**: State uses `flock` on `state.json.lock` to prevent concurrent modification. Always use `LoadState()` and `Save()` methods.
- **RmState Deletes Binaries**: The `RmState` function doesn't just remove state entries—it deletes the actual binary files from disk.
- **Pinned Apps**: Apps marked with `Pinned: true` are skipped during updates unless specifically targeted by repository slug.
- **Update Logic**: `DoUpdate` recreates `params.CLI` from stored state for each app, resetting version to "latest".

### Platform-Specific Behavior
- **Default Install Types**: Vary by OS and detected package managers:
  - Linux with dpkg: prioritizes `deb`
  - Linux with rpm: prioritizes `rpm`
  - Linux without either: prioritizes `appimage`, `flatpak`, `snap`
  - macOS: prioritizes `dmg`
  - Windows: prioritizes `exe`, `msi`
  - FreeBSD: prioritizes `pkg`, `txz`
- **Distro Detection**: On Linux, reads `/etc/os-release` to add distro-specific regex patterns. Also parses `ID_LIKE` (e.g., Pop!_OS with `ID_LIKE="ubuntu debian"` will match ubuntu-tagged assets).
- **Hardware Acceleration**: Detects NPU (`/sys/class/accel`) and GPU (`/dev/dri`) on Linux for asset matching.
- **Global Installs**: `--global` flag changes default path to `/usr/local/bin` (or `ProgramFiles` on Windows).

### Checksum Verification
- Automatically detects checksum files in releases (patterns: `checksums.txt`, `sha256sums.txt`, `sha512sums.txt`).
- Supports SHA-256 (64 hex chars) and SHA-512 (128 hex chars).
- Enabled by default (`--verify-checksum=true`). Set `--verify-checksum=false` to skip.
- Fails installation if checksum mismatch is detected. Warns if no checksum entry found for the asset.

### Package Manager Integration
- `PackageNames` field in state tracks installed package names (extracted via `dpkg-deb --field`, `rpm -qp`, `pacman -Qp`).
- `--rm-saved-state` properly uninstalls packages via the correct package manager (`dpkg -r`, `rpm -e`, `pacman -R`, `pkg delete`) before removing state entries.
- Falls back to `os.Remove` for binaries not installed via package manager.

### Pinning & Dry-Run
- `--pin` marks an installation as pinned in state.json. Pinned apps are skipped during `-U`/`-u` updates unless specifically targeted by repo slug.
- `--dry-run` logs what would be downloaded/installed without performing any filesystem changes or package manager operations.

### Asset Matching
- **Regex Priority**: `buildRegexFromTypes` creates multiple regex patterns in priority order. The selector tries each in sequence and stops at the first match.
- **Architecture Aliases**: `amd64` matches `x86_64` and `x64`; `arm64` matches `aarch64`.
- **OS Aliases**: `darwin` matches `macos` and `apple`; `windows` matches `win`.
- **Wine Support**: `--allow-wine` enables matching Windows executables on non-Windows platforms.

### Binary Name Handling
- **Clean Name Generation**: `GenerateCleanName` aggressively strips OS/arch/version affixes from binary names. It's used for the `-r` (prompt rename) feature.
- **Rename Map**: The `-t` flag accepts a map like `"oldname=newname"` to rename binaries during installation.

### Configuration Merging
- **Priority Order**: CLI flags > environment variables > config file values.
- **Config Fields**: `config.yml` supports `install_types`, `install_path`, `add_deps`, `no_deps`, `prompt_rename`, `disable_prompts`, `no_save_state`, `allow_wine`.

## Testing Approach
- Tests are located alongside source files (`*_test.go`).
- Uses `stretchr/testify` for assertions.
- Key test coverage:
  - `name_cleaner_test.go`: Extensive tests for binary name normalization across platforms.
  - `config_test.go`: Config loading and merging.
  - `state_test.go`: State persistence and locking.
- Use `go test -v ./...` for verbose output.

## Directory Structure
- `cmd/`: CLI command definitions, routing, and platform-specific defaults.
- `config/`: Configuration loading from XDG config directory.
- `params/`: CLI parameter definitions using kong struct tags.
- `release/`: GitHub release interaction, binary installation, and name cleaning.
- `selector/`: Interactive and regex-based asset/binary selection.
- `state/`: Persistent state management with file locking.

## Release Process
- GitHub Actions workflow (`.github/workflows/release.yaml`) triggers on version tags (`v*`).
- Uses `cli/gh-extension-precompile@v2` to build cross-platform binaries.
- Go version is read from `go.mod` (`go 1.25.0`).
