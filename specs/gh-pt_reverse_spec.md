# gh-pt Reverse-Engineered Specification

**Generated**: 2026-10-04  
**Source**: Codebase at `/home/tay/builds/gh-pt`  
**Method**: Spec Miner (Arch Hat + QA Hat analysis)

---

## 1. Technology Stack & Architecture

### Core Technologies
- **Language**: Go 1.23+ (uses `go.mod` with modern toolchain)
- **CLI Framework**: `alecthomas/kong` for command parsing with shell completion (`kongplete`)
- **GitHub API**: `cli/go-gh/v2` (REST + GraphQL)
- **Config**: YAML via `gopkg.in/yaml.v3`, XDG paths via `adrg/xdg`
- **State**: JSON with file locking (`gofrs/flock`)
- **Logging**: `charmbracelet/log` + `pterm` for TUI
- **Architecture**: Single binary, plugin-style command structure

### Directory Structure
```
cmd/           # Command implementations (root.go, state_mgmt.go, update.go, etc.)
config/        # Configuration loading (config.go)
params/        # Kong CLI parameter definitions (params.go)
release/       # Release fetching, asset matching, installation (release.go)
selector/      # Asset regex building and selection logic
state/         # State management with flock (state.go)
resolver/      # Package manager resolution (not fully read)
heuristics/    # Ecosystem detection and priority resolution
ai/            # AI integration (compile-from-source, safety scan)
ui/            # Progress bars and TUI components
status/        # Status tracking
internal/      # Internal packages
```

### Entry Points
- `main.go` - Validates `gh` CLI presence, loads config, parses args, dispatches to `cmd.RunCommand()`
- `params.CLI` - Kong struct defining all subcommands and global flags

---

## 2. Observed Requirements (EARS Format)

### 2.1 Installation Flow

**REQ-INST-001** (Ubiquitous): The system shall install binaries from GitHub releases for a given `owner/repository` format.

**REQ-INST-002** (Event-driven): When the user provides a repository argument without flags, the system shall install the latest release matching the default type priority for the current OS/arch.

**REQ-INST-003** (Event-driven): When `--global` flag is provided, the system shall install to `/usr/local/bin` (Linux/macOS/BSD) or `C:\Program Files` (Windows) instead of `~/.local/bin`.

**REQ-INST-004** (Event-driven): When running as root (EUID=0) without `--global` and without `--allow-root-user-install`, the system shall error with "running as root without --global flag. Use --global for system-wide install or --allow-root-user-install to install to user-local paths".

**REQ-INST-005** (State-driven): While `TargetPath` is empty, the system shall use `GetDefaultTargetPath()` which returns `~/.local/bin` (user) or `/usr/local/bin` (global).

**REQ-INST-006** (Event-driven): When `--global` or `--update-all` is used, the system shall run `sudo -v` to establish credential cache before installation.

**REQ-INST-007** (Event-driven): When a release asset is an archive, the system shall extract it and install matching binaries based on `--asset-binaries` or `--asset-binaries-regexp`.

**REQ-INST-008** (Event-driven): When `--symlink` is used, the system shall extract the entire release to `~/src/apps/{owner}/{repo}` and create symlinks in the target bin directory.

**REQ-INST-009** (Optional): Where `--verify-checksum` is enabled (default), the system shall discover and verify SHA-256/SHA-512 checksums from release assets.

**REQ-INST-010** (Optional): Where `--skip-vt-sandbox` is disabled (default), the system shall scan release assets via VirusTotal API.

### 2.2 Asset Selection & Matching

**REQ-ASSET-001** (Ubiquitous): The system shall build regex matchers from `--type` flags, prioritizing by order in the type list.

**REQ-ASSET-002** (Event-driven): When on Linux, the system shall read `/etc/os-release` to detect distro ID and ID_LIKE for distro-specific asset matching (e.g., `.deb` for Debian/Ubuntu, `.rpm` for Fedora/RHEL).

**REQ-ASSET-003** (Event-driven): When on Linux, the system shall detect hardware acceleration (NPU at `/sys/class/accel`, CUDA at `/dev/nvidia0`, ROCm at `/dev/kfd`) and include `npu`, `cuda`, or `rocm` in match patterns.

**REQ-ASSET-004** (Event-driven): When no architecture is specified in a release filename, the system shall assume it is compatible with the current system architecture.

**REQ-ASSET-005** (Event-driven): When a release asset specifies a foreign architecture (e.g., `arm64` on `amd64`), the system shall reject it unless `--allow-foreign-arch` is provided.

**REQ-ASSET-006** (State-driven): While matching assets, the system shall apply priority: version > OS > (Linux: distro) > architecture > libc > final fallback (`{name}.{extension}`).

**REQ-ASSET-007** (Ubiquitous): The system shall support package formats: deb, rpm, pkg, txz, dmg, appimage, flatpak, snap, 7z, tar.*, zip, py, ts, js, none.

**REQ-ASSET-008** (Event-driven): When Wine mode is `allow`/`priority`/`force` on non-Windows, the system shall include Windows assets (`.exe`, `.msi`) in match patterns.

### 2.3 State Management

**REQ-STATE-001** (Ubiquitous): The system shall persist installation state to `$XDG_DATA_HOME/gh-pt/state.json` with file locking (`state.json.lock`).

**REQ-STATE-002** (State-driven): While state version < 2, the system shall migrate V1 to V2 (segregating `apps` and `repos`, promoting `PackageNames` to `SystemPackages`).

**REQ-STATE-003** (Event-driven): When an app is installed, the system shall save `InstalledApp` with fields: `Repository`, `TargetPath`, `Global`, `ReleaseAsset`, `ReleaseRegexp`, `Version`, `Rename`, `Disabled`, `Type`, `All`, `AssetBinaries`, `AssetBinariesRegexp`, `InstalledBinaries`, `InstalledAssetNames`, `InstalledAssetsFullNames`, `ContainingArchive`, `PackageNames`, `Pinned`, `Extractor`, `Clone`, `Fork`, `MaxDepth`, `CompileScript`, `IsPrerelease`, `LastVTScan`, `LastAIScan`, `SymlinkDir`, `Hooks`, `SystemPackages`, `Sidecars`, `SidecarSymlinkTo`, `IncludeSidecars`, `InstalledSidecars`, `EnvInject`, `FallbackReleases`.

**REQ-STATE-004** (Event-driven): When `--pin-install` is used, the system shall set `Pinned=true` on the app state.

**REQ-STATE-005** (Event-driven): When `ghpt ls` is run with `-g`, the system shall filter to only show `Global=true` apps.

**REQ-STATE-006** (Event-driven): When `ghpt rm <target>` is run, the system shall uninstall via package manager (dpkg/rpm/pacman/pkg) if `PackageNames` exist, else delete binary files from `TargetPath`.

**REQ-STATE-007** (Event-driven): When `ghpt purge <target>` is run, the system shall also remove the cloned/forked repository directory.

### 2.4 Update/Upgrade Flow

**REQ-UPDATE-001** (Ubiquitous): The system shall update tracked applications via `ghpt upgrade [repo]`.

**REQ-UPDATE-002** (Event-driven): When `ghpt upgrade` runs without repo argument, the system shall update all non-disabled, non-pinned apps.

**REQ-UPDATE-003** (Event-driven): When `-u`/`--user` is provided, the system shall update only apps where `Global=false`.

**REQ-UPDATE-004** (Event-driven): When `-g`/`--global` is provided, the system shall update only apps where `Global=true`.

**REQ-UPDATE-005** (Event-driven): When a tracked app has `CompileScript`, the system shall check remote commit via `gh api repos/{owner}/{repo}/commits/HEAD` and re-run the compile script if changed.

**REQ-UPDATE-006** (Event-driven): When a tracked app has `Clone` or `Fork`, the system shall run `gh repo sync` in the target directory.

**REQ-UPDATE-007** (Event-driven): For binary installs, the system shall build a batched GraphQL query to fetch latest release tags for all tracked repos in one round-trip.

**REQ-UPDATE-008** (Event-driven): When a GraphQL batch query includes invalid repos (e.g., `test/repo` from test data), the system shall log partial errors but continue with valid repos.

**REQ-UPDATE-009** (State-driven): While updating, the system shall use `errgroup` with concurrency limit of 4 for parallel downloads/installs.

**REQ-UPDATE-010** (Event-driven): When update succeeds, the system shall update `app.Version` and save state atomically at the end.

### 2.5 Compile-From-Source (AI-Assisted)

**REQ-COMPILE-001** (Ubiquitous): The system shall support `--compile-from-source` paired with `--ai` to build from source using AI-generated scripts.

**REQ-COMPILE-002** (Event-driven): When `--compile-from-source` is used without `--ai`, the system shall error: "--compile-from-source can only be used with --ai".

**REQ-COMPILE-003** (Event-driven): When enabled, the system shall clone the repo to `~/builds/{repo}` (not persistent), generate a compile script at `~/.config/gh-pt/scripts/compile-{pkg}.sh` (or `.ps1` on Windows).

**REQ-COMPILE-004** (Event-driven): When an existing compile script exists, the system shall attempt to run it first before regenerating with AI.

**REQ-COMPILE-005** (State-driven): While AI generates the script, the system shall pass a prompt containing repo path, script path, target path, and optional symlink dir.

**REQ-COMPILE-006** (Event-driven): When the AI-generated script fails, the system shall retry up to 2 times, passing error output to AI for fix.

**REQ-COMPILE-007** (Event-driven): When the script produces binaries in `.ghpt/dist/`, the system shall move them to the install destination (with sidecar detection).

**REQ-COMPILE-008** (Event-driven): When `--symlink` is used with compile-from-source, the system shall stage in `~/src/apps/{owner}/{repo}` and symlink to target bin.

**REQ-COMPILE-009** (Event-driven): When successful, the system shall save `CompileScript` path and `Version` (commit hash) to state.

### 2.6 Repository Clone/Fork

**REQ-REPO-001** (Ubiquitous): The system shall support `--clone` and `--fork` flags to clone/fork repos persistently.

**REQ-REPO-002** (Event-driven): When `--clone` is used, the system shall clone to `$GH_PT_REPO_DIR/clones/{repo}` (configurable via `clone_path`).

**REQ-REPO-003** (Event-driven): When `--fork` is used, the system shall fork via `gh repo fork --clone` to `$GH_PT_REPO_DIR/forks/{repo}` (configurable via `fork_path`).

**REQ-REPO-004** (Event-driven): When `ghpt upgrade` runs on a cloned/forked repo, the system shall run `gh repo sync` (and `git pull --force` for forks with `--overwrite`).

### 2.7 Dependency Resolution

**REQ-DEP-001** (Ubiquitous): The system shall resolve build dependencies via AI-generated manifest + heuristics.

**REQ-DEP-002** (Event-driven): When dependencies are detected, the system shall use `heuristics.DetectEcosystem(repoDir)` to identify ecosystem (go, rust, cargo, npm, cmake, etc.).

**REQ-DEP-003** (Event-driven): When resolving dependencies, the system shall use priority chain from config `dependency_resolution.priorities[ecosystem]`.

**REQ-DEP-004** (Event-driven): When `--prompt-deps` is used, the system shall present dependencies for user confirmation before installing.

**REQ-DEP-005** (Event-driven): When no explicit resolver, the system shall fall back to native package manager (apt/dnf/pacman/brew).

### 2.8 Configuration

**REQ-CONFIG-001** (Ubiquitous): The system shall load config from `$XDG_CONFIG_HOME/gh-pt/config.yml`.

**REQ-CONFIG-002** (State-driven): While config exists, the system shall apply defaults for: `install_path`, `global_path`, `clone_path`, `fork_path`, `sidecar_path`, `install_types`, `extractor`, `ai_cmd`, `ai_interactive_cmd`, and all `CoreConfig` fields.

**REQ-CONFIG-003** (Event-driven): When CLI flag and config both set a value, CLI flag takes precedence.

**REQ-CONFIG-004** (Event-driven): When environment variable `GH_PT_*` is set, it takes precedence over config.

### 2.9 Sidecar Handling

**REQ-SIDECAR-001** (Ubiquitous): The system shall detect sidecar assets (shared libraries, configs, plugins) via regex pattern (default: `\.so.*|\.h.*|\.pak|\.bin|\.red`).

**REQ-SIDECAR-002** (Event-driven): When `--include-sidecars` is set, the system shall route sidecars per mode: `same_dest`, `xdg_data_home`, `bin`, `local-map`, or `custom-path:/path`.

**REQ-SIDECAR-003** (Event-driven): When `--ai-setup-sidecars` is used, the system shall invoke AI to generate post-install setup commands for sidecars.

### 2.10 Security & Scanning

**REQ-SEC-001** (Ubiquitous): The system shall support VirusTotal integration via `ghpt vt set-key` and automatic scanning.

**REQ-SEC-002** (Event-driven): When `--ai-safety-scan` is used, the system shall invoke AI to analyze repo for malicious code before install.

**REQ-SEC-003** (Event-driven): When `--barbarous` is used, the system shall disable checksum verification, VT sandbox, allow foreign arch, and allow downgrades.

**REQ-SEC-004** (Event-driven): When `sudo` is needed for global install, the system shall prompt via `sudo -v` and use cached credentials.

### 2.11 Hooks

**REQ-HOOK-001** (Ubiquitous): The system shall support `post-install` and `pre-uninstall` hooks per repository.

**REQ-HOOK-002** (Event-driven): When a `post-install` hook exists in state, the system shall execute it after successful installation.

**REQ-HOOK-003** (Event-driven): When a `pre-uninstall` hook exists, the system shall execute it before removal.

### 2.12 Environment Variables

**REQ-ENV-001** (Ubiquitous): The system shall use `GH_PT_*` prefix for all env vars (configurable via `GH_PT_ENV_PREFIX`).

**REQ-ENV-002** (Ubiquitous): Legacy `GH_INSTALL_*` prefix is supported for backward compatibility.

---

## 3. Non-Functional Observations

### Performance
- Batched GraphQL queries reduce N API calls to 1 for updates
- Concurrent updates limited to 4 workers (`errgroup`)
- Progress bars (pacman/standard/spinner) for long operations

### Security
- File locking (`flock`) on state.json prevents corruption
- Path traversal protection in `safeDeletePath()`
- Root user detection with explicit `--allow-root-user-install` opt-in
- Checksum verification enabled by default
- VirusTotal scanning optional but default-on

### Compatibility
- Cross-platform: Linux, macOS, Windows, FreeBSD
- Architecture detection: amd64, arm64 (with regex patterns)
- Distro detection via `/etc/os-release` (ID, ID_LIKE)
- Wine support for Windows binaries on Linux

### Extensibility
- Hook system for lifecycle events
- Configurable package manager priority chains
- Custom extractor precedence (ouch, native, internal)
- AI command template configurable

---

## 4. Inferred Acceptance Criteria

| ID | Scenario | Expected Behavior |
|----|----------|-------------------|
| AC-01 | `ghpt install owner/repo` | Installs latest release to `~/.local/bin` |
| AC-02 | `ghpt install owner/repo -g` | Installs to `/usr/local/bin` (prompts sudo) |
| AC-03 | `ghpt install owner/repo --version v1.0.0` | Installs specific version |
| AC-04 | `ghpt upgrade` | Updates all tracked apps |
| AC-05 | `ghpt upgrade -u` | Updates only user apps |
| AC-06 | `ghpt upgrade -g` | Updates only global apps |
| AC-07 | `ghpt upgrade owner/repo` | Updates specific repo |
| AC-08 | `ghpt source owner/repo --ai` | Clones, generates AI script, compiles, installs |
| AC-09 | `ghpt source owner/repo --ai --symlink` | Stages in `~/src/apps`, symlinks to bin |
| AC-10 | `ghpt ls -g` | Lists only global installations |
| AC-11 | `ghpt rm owner/repo` | Uninstalls and removes from state |
| AC-12 | `ghpt config set install_path /custom/path` | Persists config |

---

## 5. Uncertainties & Questions

### U-001: State Schema Evolution
- **Observation**: State struct has `Version` field but only V1→V2 migration implemented
- **Question**: Is there a planned V3 for the new `target_base_dir` / `global` fields discussed in SPEC-SOURCE-REPOS.md?

### U-002: Compile Script State Fields
- **Observation**: `InstalledApp` has `CompileScript`, `SymlinkDir`, `MaxDepth` but no `TargetBaseDir` or `Track` (stable/prerelease/latest-commit)
- **Question**: Are these fields pending implementation per the new spec?

### U-003: GraphQL Batch Query Filtering
- **Observation**: `FetchLatestReleaseTags` queries ALL repos in state, including invalid ones like `test/repo`
- **Question**: Should targeted upgrade (`ghpt upgrade owner/repo`) filter to only that repo before GraphQL query?

### U-004: Config Missing Fields
- **Observation**: `config.go` has `install_path`, `global_path` but SPEC-SOURCE-REPOS.md proposes `target_base_dir` + `global` boolean
- **Question**: Will config migrate to new schema?

### U-005: Upgrade Flag Reduction
- **Observation**: Current `UpgradeCmd` in params.go only has `User`, `Global`, `Repository` but main.go passes full `ExecContext` to `DoUpdate`
- **Question**: Does `ghpt upgrade` actually accept all the install flags shown in current `-h` output (which are from `CommonInstallFlags` embedded in root)?

### U-006: Source Command State Tracking
- **Observation**: `handleCompileFromSource` saves to `state.Apps` but SPEC-SOURCE-REPOS.md proposes separate `source_repos` map
- **Question**: Is the new `SourceRepo` struct (with `RepoPath`, `Track`, `CurrentVersion`, `ManifestPath`) implemented?

### U-007: Helper Command
- **Observation**: No `ghpt helper` command exists in params.go or cmd/
- **Question**: Is this entirely new per SPEC-SOURCE-REPOS.md?

### U-008: Wrapper Binary
- **Observation**: No wrapper binary (`ghpt-ai-wrapper`) exists
- **Question**: Is this entirely new per SPEC-SOURCE-REPOS.md?

---

## 6. Code Location References

| Requirement | File | Line(s) |
|-------------|------|---------|
| Root validation, gh check | main.go | 19-90 |
| Install flow | cmd/root.go | 141-410 |
| Asset regex building | cmd/root.go | 1455-1604 |
| Distro detection | cmd/root.go | 1472-1495 |
| Hardware detection | cmd/root.go | 1498-1515 |
| State load/save | state/state.go | 194-260 |
| State migration V1→V2 | state/state.go | 124-192 |
| Upgrade flow | cmd/update.go | 130-473 |
| GraphQL batch query | cmd/update.go | 49-128 |
| Compile-from-source | cmd/root.go | 1046-1369 |
| AI script generation | cmd/root.go | 573-607, 1214-1296 |
| Dependency resolution | cmd/root.go | 923-1003 |
| Clone/Fork handling | cmd/root.go | 487-558 |
| Config loading | config/config.go | 60-77 |
| CLI param definitions | params/params.go | 49-360 |
| System info prototype | system-info-prototype/main.go | 1-325 |

---

## 7. Recommendations

### R-001: Fix GraphQL Batch Query for Targeted Upgrades
Filter `trackedRepos` to only the specified repo when `r.Repository != ""` in `DoUpdate()` before calling `FetchLatestReleaseTags()`.

### R-002: Align Upgrade Flags with Spec
Remove embedded `CommonInstallFlags` from upgrade path; implement minimal flag set per SPEC-SOURCE-REPOS.md.

### R-003: Add State Fields for Source Builds
Add `TargetBaseDir`, `Global` (bool), `Track` (string) to `State` struct; add `SourceRepo` map.

### R-004: Implement ghpt helper Command
Add `HelperCmd` to params.go with subcommands: `--get-system-info`, `--view-target-dirs`, `--view-installed-files`, `--install`, manifest operations.

### R-005: Add Root Warning
Add yellow warning banner when EUID=0: `*** running as root is *HIGHLY* discouraged ***`

### R-006: Config Migration
Add migration logic for `install_path`/`global_path` → `target_base_dir` + `global` boolean.

---

## 8. Validation Checklist

- [x] All subcommands mapped
- [x] State schema documented
- [x] Config schema documented
- [x] CLI flag precedence documented
- [x] Error handling patterns identified
- [x] Security patterns identified
- [x] Cross-platform paths verified
- [ ] Test coverage analysis (pending)
- [ ] Performance benchmarks (pending)