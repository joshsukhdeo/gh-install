# SPEC-SOURCE-REPOS: Persistent Repository Clones & AI-Assisted Source Builds

## 1. Overview

This specification defines gh-pt's source build system, which manages persistent repository clones, AI-assisted compilation script generation, and deterministic dependency installation. The system enables users to build tools from source while maintaining reproducibility and security.

### 1.1 Core Principles

- **Persistent Clones**: Repositories are cloned once and reused, not re-downloaded
- **AI-Assisted Compilation**: AI generates compile scripts within strict boundaries
- **Deterministic Dependencies**: All dependencies explicitly listed in manifest.json
- **Security-First**: AI operates in a restricted sandbox via wrapper binary
- **Version Flexibility**: Support for stable, prerelease, and latest-commit tracks

### 1.2 User Stories

**US-1**: As a user, I want to install a tool from source so that I get the latest features not available in binary releases.

**US-2**: As a user, I want to update my source-built tools when new versions are released, without manually re-cloning repositories.

**US-3**: As a user, I want to track different update channels (stable/prerelease/latest) for different tools.

**US-4**: As a developer, I want to build tools with custom modifications by forking repositories.

**US-5**: As a security-conscious user, I want AI-generated compile scripts to be validated before execution.

## 2. Directory Structure

### 2.1 Repository Storage

```
$GH_PT_REPO_DIR/
├── clones/           # Standard clones from `ghpt source owner/repo`
│   ├── owner/
│   │   └── repo/
├── forks/            # Forked repos from `ghpt source owner/repo --fork`
│   └── owner/
│       └── repo/
```

**Default Location**: `$XDG_DATA_HOME/gh-pt/repos` (typically `~/.local/share/gh-pt/repos`)

**Override**: Set `GH_PT_REPO_DIR` environment variable or `repo_dir` in config.yml

**Rationale**:
- `clones/` vs `forks/` separation makes it clear which repos are read-only vs modifiable
- Owner/repo nesting prevents naming collisions (e.g., `github.com/foo/bar` vs `github.com/baz/bar`)
- XDG compliance follows Linux best practices

### 2.2 Compile Artifacts

```
$GH_PT_REPO_DIR/clones/owner/repo/
├── .ghpt/
│   ├── manifest.json      # Dependency manifest (AI-readable, gh-pt validated)
│   ├── compile.sh         # Concatenated script: header + body + footer
│   ├── body.sh            # AI-generated body only (for reference)
│   ├── compile.log        # Execution log
│   └── install.log        # File installation log
```

**Key Points**:
- `.ghpt/` directory is owned by gh-pt, not the user or AI
- `manifest.json` is modified via `gh-pt helper --append-manifest`, not directly edited
- `compile.sh` is auto-generated; users should not edit it directly
- `body.sh` is the AI's output, kept for debugging/reference

## 3. State Schema Changes

### 3.1 New Fields in `InstalledApp`

```go
type InstalledApp struct {
    // ... existing fields ...
    
    Track           string `json:"track,omitempty"`            // "stable" | "prerelease"
    SourceRepo      string `json:"source_repo,omitempty"`      // "clones" | "forks"
    RepoPath        string `json:"repo_path,omitempty"`        // Full path to cloned repo
    LastBuildTime   string `json:"last_build_time,omitempty"`  // ISO timestamp
    BuildSuccess    bool   `json:"build_success,omitempty"`    // Last build succeeded?
}
```

### 3.2 New Fields in `State`

```go
type State struct {
    // ... existing fields ...
    TargetBaseDir string `json:"target_base_dir,omitempty"`  // Base installation directory (e.g., "/home/user/.local" or "/usr/local")
    Global        bool   `json:"global,omitempty"`             // True if installed globally (system-wide)
    SourceRepos   map[string]*SourceRepo `json:"source_repos,omitempty"`
}

type SourceRepo struct {
    Repository      string `json:"repository"`               // "owner/repo"
    RepoPath        string `json:"repo_path"`                // Full path
    IsFork          bool   `json:"is_fork"`                  // Forked or standard clone?
    CurrentVersion  string `json:"current_version"`          // Built version/commit
    Track           string `json:"track"`                    // "stable" | "prerelease" | "latest-commit"
    LastUpdated     string `json:"last_updated"`             // ISO timestamp
    CompileScript   string `json:"compile_script"`           // Path to compile.sh
    ManifestPath    string `json:"manifest_path"`            // Path to manifest.json
}
```

### 3.3 Migration Strategy

**Old State** → **New State**:

```json
// Old
{
  "apps": {
    "owner/repo": {
      "is_prerelease": true,
      "source_track": "stable"
    }
  }
}

// New
{
  "target_base_dir": "/home/user/.local",
  "global": false,
  "apps": {
    "owner/repo": {
      "track": "prerelease"
    }
  },
  "source_repos": {
    "owner/repo": {
      "repository": "owner/repo",
      "repo_path": "/home/user/.local/share/gh-pt/repos/clones/owner/repo",
      "is_fork": false,
      "current_version": "v1.2.3",
      "track": "prerelease",
      "last_updated": "2026-01-04T12:00:00Z",
      "compile_script": "/home/user/.local/share/gh-pt/repos/clones/owner/repo/.ghpt/compile.sh",
      "manifest_path": "/home/user/.local/share/gh-pt/repos/clones/owner/repo/.ghpt/manifest.json"
    }
  }
}
```

**Migration Logic**:
1. For each app with `is_prerelease=true`, set `track="prerelease"`
2. For each app with `source_track` set, use that as `track` (takes precedence)
3. Default to `track="stable"` if neither is set
4. Create corresponding `SourceRepo` entry if repo exists on disk
5. Remove deprecated fields (`is_prerelease`, `source_track`)
6. If `target_base_dir` not set, infer from existing app install paths (default: `$HOME/.local`)
7. Set `global=true` if `target_base_dir` is `/usr/local` or other system directory

## 4. CLI Commands

### 4.1 `ghpt source owner/repo [flags]`

**Purpose**: Build a tool from source

**Flags**:

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--version, -v` | string | "stable" | Version to build: "stable", "prerelease", "latest-commit", or explicit version |
| `--temp-repo-clone` | bool | false | Use temporary clone instead of persistent |
| `--fork` | bool | false | Fork the repository before cloning |
| `--improve-compile-script` | bool | false | Pass existing script to AI for improvement before execution |
| `--rebuild-compile-script` | bool | false | Delete existing script and regenerate from scratch |
| `--no-optimization` | bool | false | Disable hardware-specific compiler optimizations |
| `--target-base-dir` | string | - | Base installation directory (default: ~/.local or /usr/local with -g). **Only for `ghpt source` and `ghpt install`**. |
| `--global, -g` | bool | false | For `source`/`install`: install to /usr/local. For `upgrade`: update only global installations. |
| `--user, -u` | bool | false | For `upgrade`: update only user installations. |
| `--force` | bool | false | Force reinstall to different target base dir (uninstall old, install new). Only for `ghpt source` and `ghpt install`. |

**Flag Resolution & Validation** (`--target-base-dir` / `-g` / `-u` / `--force`):

| Command | Flag Support | Behavior |
|---------|--------------|----------|
| `ghpt source` | `--target-base-dir`, `-g`, `--force` | Accept all; resolve per rules below |
| `ghpt install` | `--target-base-dir`, `-g`, `--force` | Accept all; resolve per rules below |
| `ghpt upgrade` | `-g`, `-u` only | Accept `-g` (global only) and `-u` (user only); reject `--target-base-dir` and `--force` |

**Resolution Rules** (for `source` / `install`):

| Scenario | Behavior |
|----------|----------|
| Neither `--target-base-dir` nor `-g` specified | Default to `$HOME/.local` (user mode) |
| Only `-g` specified | Use `/usr/local` (global mode) |
| Only `--target-base-dir /path` specified | Use `/path` (explicit mode) |
| Both `-g` and `--target-base-dir` specified | **Error**: Conflicting flags - choose one |
| `--target-base-dir` points to system dir (`/usr`, `/usr/local`, `/opt`, etc.) without `-g` | **Error**: System directories require `-g` flag |
| `--target-base-dir` not owned by current user (and not `-g`) | **Error**: Target directory must be user-owned unless `-g` |
| State has `target_base_dir` set, new invocation differs | **Error**: Target base dir mismatch. Use `--force` to uninstall old and reinstall at new location |
| Same as above with `--force` | Uninstall old version from state's target_base_dir, install new version at specified target_base_dir, update state |

**Resolution Rules** (for `upgrade`):

| Scenario | Behavior |
|----------|----------|
| No flags | Update all installations (both global and user) |
| `-u` / `--user` specified | Update only installations where `state.global=false` |
| `-g` / `--global` specified | Update only installations where `state.global=true` |
| Both `-u` and `-g` specified | **Error**: Conflicting flags - choose one |
| `--target-base-dir` specified | **Error**: Not supported. Use `ghpt source --target-base-dir /new/path --force` to relocate |
| `--force` specified | **Error**: Not supported for upgrade |
| Any other flag not in minimal set | **Error**: Flag not supported for upgrade |

**Behavior**:

1. **First Run (no existing repo)**:
   - Clone repository to `$GH_PT_REPO_DIR/clones/owner/repo` (or `forks/` if `--fork`)
   - Resolve version based on `--version` flag
   - Checkout appropriate tag/branch/commit
   - Generate wrapper script with PID tracking
   - Invoke AI to generate `body.sh`
   - Concatenate header + body + footer → `compile.sh`
   - Execute `compile.sh`
   - Track installed files and update state (including `target_base_dir`)

2. **Subsequent Runs (repo exists, same target_base_dir)**:
   - Update repository based on `--version` flag
   - Check if `compile.sh` exists
   - If exists and no flags: execute existing script
   - If `--improve-compile-script`: pass to AI for enhancement, then execute
   - If `--rebuild-compile-script`: delete and regenerate from scratch

3. **Target Base Dir Change (repo exists, different target_base_dir)**:
   - **Without `--force`**: Error - target base dir mismatch. Use `--force` to relocate.
   - **With `--force`**: 
     a. Uninstall old version from state's `target_base_dir` (remove tracked files, update state)
     b. Update state's `target_base_dir` to new value
     c. Proceed as first run at new location

4. **Update Mode** (called by `ghpt upgrade`):
   - Fetch latest version for repo's track
   - Compare with `current_version` in state
   - If newer version available, pull changes and rebuild
   - **Must use `target_base_dir` from state** - `--target-base-dir` rejected, `-g` accepted as verification

### 4.2 `ghpt upgrade [owner/repo] [flags]`

**Purpose**: Update installed tools (both binary and source builds)

**Flags** (minimal set - most flags from `install`/`source` are NOT accepted):

| Flag | Type | Description |
|------|------|-------------|
| `-h, --help` | - | Show context-sensitive help |
| `-l, --log-level` | string | Log level (default: info) |
| `--log-format` | string | Log output format (default: console) |
| `--no-log-quiet-interactive` | bool | Disable quiet log in interactive mode |
| `-V, --verbose` | bool | Enable verbose output |
| `--version` | - | Show version |
| `-u, --user` | bool | Update only user installations (global=false) |
| `-g, --global` | bool | Update only global installations (global=true) |
| `-D, --disable-prompts` | bool | Disable all interactive prompts |
| `--extractor` | string | Archive extractor precedence (default: ouch,native,internal) |
| `-f, --force` | bool | Reinstall packages overwriting existing files |
| `--dry-run` | bool | Show what would be upgraded |
| `--verify-checksum` | bool | Verify asset checksums [NOT stored in state.json] |
| `--skip-vt-sandbox` | bool | Skip VirusTotal sandbox scan [NOT stored in state.json] |
| `--fallback-releases` | int | Try older releases if no assets found (default: 0) |
| `--warn-unmapped-assets` | bool | Warn about suspected unmapped sidecar assets |
| `--progress-bar` | string | Progress bar style: pacman, standard, spinner:<style>, none |

**Behavior**:

1. Read `state.global` (boolean) to determine which installations to update:
   - No flags: Update all (both global and user)
   - `-u` / `--user`: Update only where `global=false`
   - `-g` / `--global`: Update only where `global=true`

2. For each matching tracked repo in `source_repos`:
   - Read `track` field (default to "stable" if missing)
   - Fetch latest version for that track
   - Compare with `current_version`
   - If update available, rebuild using stored `target_base_dir`

3. For binary installs:
   - Read `track` field
   - Fetch latest release info
   - Compare with installed version
   - If update available, download and install to stored location

**Validation**:
- Refuse to run if `--target-base-dir` provided (error: "use ghpt source --target-base-dir /new/path --force to relocate")
- Refuse to run if `state.target_base_dir` not set (corrupted state)
- Refuse to run if `state.global=false` but `target_base_dir` is not owned by current user (error: "target base dir not user-owned")
- Refuse to run if any flag not in the above table is provided (error: "flag not supported for upgrade")

**Root/Sudo Handling**:
- If running as root (EUID=0): Print **YELLOW WARNING**: `*** running as root is *HIGHLY* discouraged ***`
- If `sudo -v` context is available (password cached), use it for global operations
- Otherwise prompt for sudo when needed for global installs
- Do NOT error out - root is supported but discouraged

### 4.3 `ghpt helper [flags]`

**Purpose**: Provide AI-safe helper functions

**Flags**:

| Flag | Type | Description |
|------|------|-------------|
| `--get-manifest` | - | Display current manifest.json contents |
| `--append-manifest` | string | Add dependencies (format: "manager=pkg@version,manager=pkg@version") |
| `--remove-from-manifest` | string | Remove dependencies (format: "manager=pkg,manager=pkg") |
| `--get-body-template` | - | Display body.sh template |
| `--validate-manifest` | - | Validate manifest.json schema |
| `--validate-compile-script` | - | Validate compile.sh for forbidden patterns |
| `--run-compile-script` | - | Execute compile.sh (for AI iterative debugging) |
| `--get-system-info` | - | Display system information for AI context |
| `--view-target-dirs` | - | List contents of target base directory and subdirs |
| `--view-installed-files` | - | List paths of all installed files across target directories |
| `--install` | string | Install file/directory (format: "dirname=source") |

**Example Output** (`--get-system-info`):
```
os: linux
arch: amd64
distro: ubuntu
distro_version: 26.04
distro_codename: resolute
pkg_managers: gh-pt,mise,cargo,go,micromamba,uv,pnpm,bun,yarn,dotnet,pkgx,apt,snap,flatpak,npm,pip,pipx
compilers: gcc-15.2.0,clang-21.1.8,go-1.27.1,rustc-1.98.1,cmake-4.2.3,ninja-1.13.2,bazel-9.2.0,make-4.4.1
shell: bash
cpu_model: Intel(R) Core(TM) Ultra 9 288V
cpu_cores: 8
ram_gb: 30
user: tay
home: /home/tay
target_base_dir: /home/tay/.local
install_prefix: /home/tay/.local
install_bin: /home/tay/.local/bin
install_lib: /home/tay/.local/lib
install_share: /home/tay/.local/share
global: false
```

**Example Output** (`--view-target-dirs`):
```
/home/tay/.local/
├── bin/
│   ├── gh-pt
│   ├── mise
│   └── uv
├── lib/
│   ├── gh-pt/
│   │   └── scripts/
│   │       └── compile-gh-pt.sh
│   └── mise/
└── share/
    ├── gh-pt/
    │   └── completions/
    └── man/
        └── man1/
```

**Example Output** (`--view-installed-files`):
```
/home/tay/.local/bin/gh-pt
/home/tay/.local/bin/mise
/home/tay/.local/bin/uv
/home/tay/.local/lib/gh-pt/scripts/compile-gh-pt.sh
/home/tay/.local/share/gh-pt/completions/gh-pt.bash
/home/tay/.local/share/man/man1/gh-pt.1
```

**Security**:
- Only accessible via wrapper binary during AI session
- Wrapper validates PID is still active
- All operations logged for audit trail

### 4.4 Version Constraint Syntax

**Format**: `manager=package@constraint`

| Constraint | Meaning | Example |
|------------|---------|---------|
| `@latest` or omitted | Latest available version | `apt=gcc@latest` or `apt=gcc` |
| `@1.2.3` | Exact version | `apt=gcc@1.2.3` |
| `@1.2.3-` | Minimum version (>= 1.2.3) | `apt=gcc@10-` |
| `@1.2.3+` | Maximum version (<= 1.2.3) | `apt=gcc@14+` |
| `@~1.2.3` | Approximately this version (~1.2.x) | `apt=gcc@~13` |

**Examples**:
```bash
ghpt helper --append-manifest "apt=gcc@13-,apt=cmake@latest,cargo=ripgrep@14+"
ghpt helper --remove-from-manifest "apt=clang"
```

## 5. Compile Script Architecture

### 5.1 Three-Part Concatenation

```
┌─────────────────────────────────────┐
│  HEADER (gh-pt generated)           │
│  - Environment variable setup       │
│  - _ghpt_install function definition│
│  - Logging setup                    │
├─────────────────────────────────────┤
│  BODY (AI generated, validated)     │
│  - Build logic                      │
│  - File installation via _ghpt_install│
│  - No package manager calls         │
│  - No writes outside target dirs    │
├─────────────────────────────────────┤
│  FOOTER (gh-pt generated)           │
│  - Cleanup                          │
│  - Final logging                    │
│  - Exit code handling               │
└─────────────────────────────────────┘
```

### 5.2 Header Template

```bash
#!/bin/bash
set -euo pipefail

# ============================================================================
# gh-pt Compile Script - Auto-Generated
# Repository: {{.Repository}}
# Version: {{.Version}}
# Generated: {{.Timestamp}}
# ============================================================================

# Environment Variables
export GHPT_TARGET_BASE_DIR="{{.InstallPrefix}}"
export GHPT_TARGET_BIN_DIR="{{.InstallBin}}"
export GHPT_TARGET_LIB_DIR="{{.InstallLib}}"
export GHPT_TARGET_SHARE_DIR="{{.InstallShare}}"
export GHPT_LOG_FILE="{{.RepoPath}}/.ghpt/compile.log"
export GHPT_INSTALL_LOG="{{.RepoPath}}/.ghpt/install.log"

# Ensure target directories exist
mkdir -p "$GHPT_TARGET_BIN_DIR" "$GHPT_TARGET_LIB_DIR" "$GHPT_TARGET_SHARE_DIR"

# Logging function
log() {
    echo "[$(date -Iseconds)] $*" | tee -a "$GHPT_LOG_FILE"
}

# Installation function - validates target is within allowed prefix
_ghpt_install() {
    local src="$1"
    local dst="$2"
    
    # Resolve to absolute path
    local resolved_dst
    resolved_dst=$(realpath -m "$dst")
    
    # Validate destination is under allowed prefix
    case "$resolved_dst" in
        "$GHPT_TARGET_BASE_DIR"/*) ;;
        *) 
            log "ERROR: destination '$resolved_dst' outside allowed prefix '$GHPT_TARGET_BASE_DIR'"
            return 1
            ;;
    esac
    
    # Ensure parent directory exists
    mkdir -p "$(dirname "$resolved_dst")"
    
    # Copy file/directory
    if [ -d "$src" ]; then
        cp -r "$src" "$resolved_dst"
        # Log all installed files
        find "$resolved_dst" -type f >> "$GHPT_INSTALL_LOG"
    else
        cp "$src" "$resolved_dst"
        echo "$resolved_dst" >> "$GHPT_INSTALL_LOG"
    fi
    
    log "Installed: $src -> $resolved_dst"
}

log "=== Compile Script Started ==="
log "Repository: {{.Repository}}"
log "Version: {{.Version}}"
```

### 5.3 Body Template (AI-Generated)

```bash
# ============================================================================
# Build Logic - AI Generated
# Use _ghpt_install for all file operations
# Example: _ghpt_install ./build/myapp "$GHPT_TARGET_BIN_DIR/myapp"
# ============================================================================

{{.AI_BUILD_LOGIC}}
```

### 5.4 Footer Template

```bash
# ============================================================================
# Cleanup and Finalization
# ============================================================================

log "=== Compile Script Finished ==="
log "Installed files logged to: $GHPT_INSTALL_LOG"

# Count installed files
if [ -f "$GHPT_INSTALL_LOG" ]; then
    file_count=$(wc -l < "$GHPT_INSTALL_LOG")
    log "Total files installed: $file_count"
fi

exit 0
```

### 5.5 Validation Rules

**Forbidden Patterns** (detected by semgrep/regex):

1. **Package Manager Calls**:
   ```
   apt(-get)? install
   dnf install
   pacman -S
   brew install
   cargo install
   go install
   pip install
   npm install
   ```

2. **Direct System Writes**:
   ```
   /usr/local/*
   /usr/bin/*
   /usr/lib/*
   /opt/*
   ```

3. **Privilege Escalation**:
   ```
   sudo
   su
   pkexec
   ```

4. **System File Modifications**:
   ```
   /etc/*
   /var/*
   ```

**Validation Function**:
```go
func ValidateCompileScript(scriptPath string) error {
    content, err := os.ReadFile(scriptPath)
    if err != nil {
        return err
    }
    
    forbidden := []string{
        `apt(-get)?\s+install`,
        `dnf\s+install`,
        `pacman\s+-S`,
        `brew\s+install`,
        `cargo\s+install`,
        `go\s+install`,
        `pip\s+install`,
        `npm\s+install`,
        `/usr/local/`,
        `/usr/bin/`,
        `/usr/lib/`,
        `/opt/`,
        `\bsudo\b`,
        `\bsu\b`,
        `/etc/`,
        `/var/`,
    }
    
    for _, pattern := range forbidden {
        matched, _ := regexp.MatchString(pattern, string(content))
        if matched {
            return fmt.Errorf("forbidden pattern found: %s", pattern)
        }
    }
    
    return nil
}
```

## 6. Wrapper Binary Design

### 6.1 Purpose

The wrapper binary (`ghpt-ai-wrapper`) restricts AI to only call `ghpt helper` commands, preventing:
- Direct execution of `ghpt install` (which could modify state inappropriately)
- Access to sensitive gh-pt commands
- Bypassing of security validation

### 6.2 Implementation

```go
package main

import (
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strconv"
    "syscall"
)

func main() {
    // Get parent PID from environment
    parentPID := os.Getenv("GHPT_AI_PARENT_PID")
    if parentPID == "" {
        fmt.Println("ERROR: GHPT_AI_PARENT_PID not set")
        os.Exit(1)
    }
    
    pid, err := strconv.Atoi(parentPID)
    if err != nil {
        fmt.Printf("ERROR: invalid PID: %s\n", parentPID)
        os.Exit(1)
    }
    
    // Check if parent process is still alive
    proc, err := os.FindProcess(pid)
    if err != nil || proc.Signal(syscall.Signal(0)) != nil {
        // Parent is dead, forward to real gh-pt
        fmt.Println("Parent process not found, forwarding to gh-pt...")
        forwardToGhpt(os.Args[1:])
        return
    }
    
    // Validate command is "helper"
    if len(os.Args) < 2 || os.Args[1] != "helper" {
        fmt.Println("Only 'ghpt helper' is permitted within this AI session.")
        fmt.Println("If you need to install a dependency, use:")
        fmt.Println("  ghpt helper --append-manifest \"manager=package@version\"")
        os.Exit(1)
    }
    
    // Execute ghpt helper with all arguments
    ghptPath, err := exec.LookPath("gh-pt")
    if err != nil {
        fmt.Printf("ERROR: gh-pt not found in PATH\n")
        os.Exit(1)
    }
    
    cmd := exec.Command(ghptPath, os.Args[1:]...)
    cmd.Stdin = os.Stdin
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    
    if err := cmd.Run(); err != nil {
        os.Exit(cmd.ProcessState.ExitCode())
    }
}

func forwardToGhpt(args []string) {
    ghptPath, err := exec.LookPath("gh-pt")
    if err != nil {
        fmt.Printf("ERROR: gh-pt not found\n")
        os.Exit(1)
    }
    
    cmd := exec.Command(ghptPath, args...)
    cmd.Stdin = os.Stdin
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    
    if err := cmd.Run(); err != nil {
        os.Exit(cmd.ProcessState.ExitCode())
    }
}
```

### 6.3 Permissions and Location

```
/tmp/ghpt-ai-wrapper-$PID/
├── ghpt                  # Wrapper binary
└── (PID checked on each invocation)
```

**Permissions**: `0100` (execute-only, no read/write)

**Rationale**:
- Prevents AI from reading wrapper source and reverse-engineering restrictions
- PID in path prevents collisions between concurrent AI sessions
- `/tmp` ensures cleanup on reboot

### 6.4 Integration with AI Prompt

```
You are building a tool from source. You have access to `ghpt` command, but
you may ONLY use `ghpt helper` subcommands. All other ghpt commands are blocked.

Available commands:
- ghpt helper --get-system-info          # Get system information
- ghpt helper --view-target-dirs         # List target directory structure
- ghpt helper --view-installed-files     # List all installed files
- ghpt helper --get-manifest             # View current dependencies
- ghpt helper --append-manifest "..."    # Add dependencies
- ghpt helper --remove-from-manifest "..." # Remove dependencies
- ghpt helper --get-body-template        # Get build script template
- ghpt helper --validate-manifest        # Validate manifest
- ghpt helper --validate-compile-script  # Validate build script
- ghpt helper --run-compile-script       # Execute build script
- ghpt helper --install 'dirname=src'    # Install file/directory

If you need to install a dependency (e.g., gcc, cmake), use:
  ghpt helper --append-manifest "apt=gcc@latest,apt=cmake@3.20-"

If you need to install another gh-pt managed tool as a dependency:
  ghpt helper --append-manifest "gh-pt=owner/repo@latest"

DO NOT attempt to run `ghpt install`, `ghpt source`, or other ghpt commands.
They will be blocked by the wrapper.
```

## 7. Environment Variables and Configuration

### 7.1 Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GH_PT_REPO_DIR` | `$XDG_DATA_HOME/gh-pt/repos` | Base directory for repository clones |
| `GHPT_TARGET_BASE_DIR` | `/usr/local` (global) or `$HOME/.local` (user) | Base installation directory (set by `--target-base-dir`/`-g`, stored in state) |
| `GHPT_GLOBAL` | `false` | Whether installation is global (set by `-g`/`--global`, stored in state) |
| `GH_PT_INSTALL_DEFAULT_VERSION` | `stable` | Default version for `ghpt install` |
| `GH_PT_SOURCE_DEFAULT_VERSION` | `stable` | Default version for `ghpt source` |

**Precedence** (highest to lowest):
1. `--target-base-dir` CLI flag (explicit)
2. `-g` / `--global` CLI flag (implies `/usr/local` and `global=true`)
3. `-u` / `--user` CLI flag (implies user mode, `global=false`)
4. State file `target_base_dir` + `global` (from previous install)
5. `GHPT_TARGET_BASE_DIR` / `GHPT_GLOBAL` environment variables
6. Default: `$HOME/.local` (user, global=false) or `/usr/local` (global, global=true)

### 7.2 Configuration File

```yaml
# ~/.config/gh-pt/config.yml

# Repository storage
repo_dir: /home/user/.local/share/gh-pt/repos

# Installation paths
target_base_dir: /home/user/.local      # Base installation directory (user)
global: false                           # True if installing globally
global_path: /usr/local/bin             # Global installation directory

# Default versions
install_default_version: stable    # stable | prerelease
source_default_version: stable     # stable | prerelease | latest-commit

# AI settings
ai_cmd: agy -p "%s"
ai_interactive_cmd: agy -i -p "%s"

# Other settings
# ... (existing config fields)
```

## 8. Migration Path

### 8.1 State Migration (v1 → v2)

```go
func migrateStateV1toV2(state *State) error {
    // Initialize new fields
    if state.SourceRepos == nil {
        state.SourceRepos = make(map[string]*SourceRepo)
    }
    
    // Migrate existing apps
    for repo, app := range state.Apps {
        // Handle track migration
        if app.Track == "" {
            if app.IsPrerelease {
                app.Track = "prerelease"
            } else {
                app.Track = "stable"
            }
        }
        
        // Create SourceRepo entry if repo exists
        repoPath := getRepoPath(repo, false)
        if _, err := os.Stat(repoPath); err == nil {
            state.SourceRepos[repo] = &SourceRepo{
                Repository: repo,
                RepoPath:   repoPath,
                IsFork:     false,
                Track:      app.Track,
            }
        }
    }
    
    // Remove deprecated fields
    for _, app := range state.Apps {
        app.IsPrerelease = false
        app.SourceTrack = ""
    }
    
    return nil
}
```

### 8.2 Configuration Migration

```go
func migrateConfigV1toV2(config *Config) error {
    // Set defaults for new fields
    if config.RepoDir == "" {
        config.RepoDir = getDefaultRepoDir()
    }
    
    if config.InstallDefaultVersion == "" {
        config.InstallDefaultVersion = "stable"
    }
    
    if config.SourceDefaultVersion == "" {
        config.SourceDefaultVersion = "stable"
    }
    
    return nil
}
```

### 8.3 User Communication

**On first run after upgrade**:
```
gh-pt has been updated with new features:
- Persistent repository clones (default: ~/.local/share/gh-pt/repos)
- New 'track' field for version management (stable/prerelease/latest-commit)
- AI-assisted compile script generation with security validation

Your existing installations have been migrated automatically.
Run `ghpt state view` to see the updated state structure.
```

## 9. Security Considerations

### 9.1 AI Sandboxing

- Wrapper binary restricts AI to `ghpt helper` commands only
- All file operations validated against `GHPT_TARGET_BASE_DIR`
- No package manager calls allowed in AI-generated scripts
- No writes to system directories (`/usr`, `/etc`, etc.)
- No privilege escalation (`sudo`, `su`)

### 9.2 Manifest Validation

- Schema versioning (`schema_version` field)
- Strict validation of dependency format
- Rejection of unknown package managers
- Version constraint syntax validation

### 9.3 Compile Script Validation

- Semgrep/regex-based forbidden pattern detection
- Path canonicalization before validation
- Logging of all validation attempts (pass/fail)

### 9.4 Wrapper Security

- Execute-only permissions (0100)
- PID validation on each invocation
- Automatic cleanup of dead wrappers
- No write access prevents tampering

## 10. Testing Strategy

### 10.1 Unit Tests

- **State Migration**: Test v1→v2 migration with various scenarios
- **Manifest Parsing**: Test all version constraint formats
- **Validation**: Test forbidden pattern detection
- **Helper Commands**: Test each `ghpt helper` subcommand

### 10.2 Integration Tests

- **Full Build Flow**: Clone → AI generate → Validate → Execute → Track
- **Update Flow**: Fetch → Compare → Rebuild
- **Fork Flow**: Fork → Clone → Modify → Build
- **Error Cases**: Invalid manifest, forbidden patterns, build failures

### 10.3 Security Tests

- **Wrapper Bypass**: Attempt to call non-helper commands
- **Path Traversal**: Attempt to install outside target dirs
- **Package Manager Bypass**: Attempt to call apt/pip in AI script
- **Privilege Escalation**: Attempt sudo/su in AI script

### 10.4 Compatibility Tests

- **Old State Files**: Load v1 state and verify migration
- **Old Config Files**: Load v1 config and verify migration
- **Different OS**: Test on Linux, macOS, FreeBSD
- **Different Shells**: Test with bash, zsh, fish

## 11. Performance Considerations

### 11.1 Repository Cloning

- **Shallow Clones**: Use `git clone --depth 1` for faster initial clones
- **Sparse Checkouts**: Only checkout necessary files if possible
- **Caching**: Reuse existing clones, don't re-download

### 11.2 Version Resolution

- **GitHub API Caching**: Cache release info to avoid rate limits
- **Concurrent Resolution**: Resolve versions in parallel for multiple repos
- **Lazy Evaluation**: Only resolve version when needed (not on every command)

### 11.3 Build Execution

- **Parallel Builds**: Use `make -j$(nproc)` or equivalent
- **Incremental Builds**: Leverage build system caching (ccache, etc.)
- **Resource Limits**: Set memory/CPU limits to prevent system overload

## 12. Future Enhancements

### 12.1 Build Caching

- Cache compiled binaries to avoid rebuilding unchanged code
- Use content-addressed storage (hash of source + deps)
- Share cache across machines (optional)

### 12.2 Build Isolation

- Run builds in containers (Docker/Podman) for reproducibility
- Isolate build environment from host system
- Ensure consistent builds across different machines

### 12.3 Build Metrics

- Track build times per repository
- Track success/failure rates
- Identify slow builds and optimize

### 12.4 Build Notifications

- Notify user when long-running builds complete
- Send build status to external systems (webhook, email)
- Integrate with CI/CD systems

## 13. Appendix A: Glossary

**Clone**: A read-only copy of a repository, used for standard builds

**Fork**: A user-owned copy of a repository, allows modifications

**Track**: Update channel for a repository (stable/prerelease/latest-commit)

**Manifest**: JSON file listing all dependencies required for a build

**Body**: AI-generated portion of compile script containing build logic

**Wrapper**: Restricted binary that limits AI to safe operations

**Helper**: Safe subset of gh-pt commands accessible to AI

## 14. Appendix B: Examples

### 14.1 Basic Source Build

```bash
# Build neovim from source (stable track)
ghpt source neovim/neovim

# Build neovim from latest commit
ghpt source neovim/neovim --version latest-commit

# Build neovim with fork (allows modifications)
ghpt source neovim/neovim --fork
```

### 14.2 Update Source Builds

```bash
# Update all source-built tools
ghpt upgrade

# Update specific tool
ghpt upgrade neovim/neovim
```

### 14.3 AI-Assisted Build (Manual)

```bash
# Get system info
ghpt helper --get-system-info

# Append dependencies
ghpt helper --append-manifest "apt=gcc@13-,apt=cmake@latest,cargo=ripgrep@14+"

# View manifest
ghpt helper --get-manifest

# Validate manifest
ghpt helper --validate-manifest

# Run compile script (for iterative debugging)
ghpt helper --run-compile-script

# Install built files
ghpt helper --install 'bin=./build/neovim'
```

### 14.4 Configuration Examples

**Minimal Config**:
```yaml
# ~/.config/gh-pt/config.yml
install_path: /home/user/.local/bin
```

**Full Config**:
```yaml
# ~/.config/gh-pt/config.yml
repo_dir: /home/user/projects/ghpt-repos
install_path: /home/user/.local/bin
global_path: /usr/local/bin
install_default_version: stable
source_default_version: latest-commit
ai_cmd: agy -p "%s"
```

## 15. Appendix C: Error Messages

### 15.1 Repository Errors

```
ERROR: Repository not found: owner/repo
  → Check repository name and your GitHub access

ERROR: Repository already exists at /path/to/repo
  → Use --temp-repo-clone for temporary clone
  → Or remove existing clone first
```

### 15.2 Validation Errors

```
ERROR: Forbidden pattern in compile.sh: sudo
  → Remove privilege escalation attempts
  → Use _ghpt_install for file operations

ERROR: Destination '/usr/local/bin/app' outside allowed prefix '/home/user/.local'
  → Use $GHPT_TARGET_BIN_DIR instead of hardcoded paths
  → Or run with --global flag
```

### 15.3 Build Errors

```
ERROR: Build failed with exit code 1
  → Check compile.log for details: /path/to/repo/.ghpt/compile.log
  → Try --rebuild-compile-script to regenerate

ERROR: Manifest validation failed: unknown package manager 'brew'
  → Use supported package managers: apt, dnf, cargo, go, etc.
  → Or use 'gh-pt' for gh-pt-managed tools
```

## 16. Conclusion

This specification provides a comprehensive framework for persistent repository management, AI-assisted source builds, and secure dependency resolution. The design prioritizes:

- **Security**: Strict sandboxing and validation
- **Reproducibility**: Deterministic builds with explicit dependencies
- **Flexibility**: Multiple version tracks and update strategies
- **Usability**: Clear CLI commands and helpful error messages
- **Maintainability**: Clean separation of concerns and comprehensive testing

The implementation should proceed in phases:
1. State schema migration and new fields
2. Persistent repository cloning
3. Helper command implementation
4. Compile script validation
5. Wrapper binary for AI sandboxing
6. Integration testing and documentation

---

**Document Version**: 1.0  
**Last Updated**: 2026-01-04  
**Author**: AI Assistant (with user input)  
**Status**: Draft - Pending Review
