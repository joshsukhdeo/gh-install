# gh-pt

[![Go Report Card](https://goreportcard.com/badge/github.com/joshsukhdeo/gh-pt)](https://goreportcard.com/report/github.com/joshsukhdeo/gh-pt)

`gh-pt` is a powerful, cross-platform GitHub CLI extension designed to radically simplify the installation and management of release binaries across Linux, macOS, Windows, and FreeBSD.

It acts as an intelligent package manager on top of GitHub Releases, automatically resolving the best asset for your architecture and OS, dynamically routing to native package managers (like `apt`, `dnf`, `pacman`), safely extracting archives, and keeping your installations updated in a centralized state.

---

## Installation

Ensure you have the [GitHub CLI (`gh`)](https://cli.github.com/) installed and authenticated, then install the extension:

```bash
gh extension install joshsukhdeo/gh-pt
```

---

## Key Features

- **Intelligent Asset Resolution:** Automatically detects your OS and architecture, filtering out incompatible assets while prioritizing distro-specific tags (e.g., `ubuntu` over generic `linux`).
- **Native Package Manager Routing:** Seamlessly bridges GitHub releases with your system's package manager (`apt`, `dnf`, `pacman`, `pkg`).
- **Advanced Extraction Engine (`--extractor`):** Configurable extraction pipelines supporting `ouch`, native commands (`tar`, `unzip`), and internal Go libraries.
- **Robust State Management (`state.json`):** Tracks all installations, including flags and custom configurations, enabling seamless one-command upgrades.
- **Dependency Resolution (`-y, --resolve-deps`):** Automatically resolves and installs dependencies for `.deb`, `.rpm`, and `.pkg.tar.zst` files.
- **Source Compilation (`source`):** AI-assisted generation and execution of build scripts for repositories without pre-compiled binaries.
- **Security & Integrity:** VirusTotal scanning integration and automatic checksum verification.

---

## Command Reference

### `install`
Installs a GitHub release asset.

```bash
gh pt install <owner/repo> [flags]
```
* **Asset Selection:**
  * `-v, --release-version`: Specify a version tag (default: `latest`).
  * `-a, --release-asset`: Exact asset name to download.
  * `-A, --release-asset-regexp`: Regex matching the asset name.
  * `-T, --type`: Comma-separated list of preferred formats (e.g., `deb,appimage,tar.gz`).
* **Extraction & Target:**
  * `-p, --target-path`: Installation directory. Defaults to `~/.local/bin` (or `/usr/local/bin` with `-g`).
  * `-g, --global`: Install globally (requires sudo if applicable).
  * `-b, --asset-binaries`: Specific binaries to extract from an archive.
  * `--extractor`: Define extraction engine precedence (default: `ouch,native,internal`).
* **State & Tracking:**
  * `-S, --no-save-state`: Perform installation without tracking in `state.json`.
  * `--pin-install`: Install and immediately pin the version to prevent automatic updates.
* **Sidecar Assets:**
  * `--include-sidecars`: Auto-detect and include suspected sidecar assets (implied by `--sidecar-*` params).
  * `-s, --sidecars`: Glob patterns for sidecar assets to capture (e.g., `plugins/*.red`).
  * `--sidecar-target-path`: Target directory for sidecar assets (default: XDG data home).
  * `--sidecar-symlink-to`: Create symlinks from sidecars to app-specific directories (can be specified multiple times).
  * `--ai-setup-sidecars`: Use AI to analyze sidecars and generate post-install setup commands.
* **Fallback & Recovery:**
  * `--fallback-releases`: Try this many older releases if no assets found in latest (default: 0, disabled).

### `upgrade`
Updates all tracked installations in `state.json` to their latest versions, preserving all original installation flags.

```bash
gh pt upgrade [flags]
```
* `-u, --user`: Only update user-level installations.
* `-g, --global`: Only update global-level installations.

### `ls` / `ll`
List currently saved installations. `ll` provides extended metadata.

```bash
gh pt ls [filter]
gh pt ll [filter]
```

### `rm`
Uninstalls an application and removes it from the state tracker.

```bash
gh pt rm <target> [flags]
```
* `--purge`: Completely uninstall and purge any cached files or compile scripts.

### `search`
Searches GitHub repositories directly from the CLI.

```bash
gh pt search <query> [flags]
```
* `-d, --description`: Broadens the search to include repository descriptions in addition to repository names.

### Repository Management: `repo clone` & `repo fork`
Clones or forks a repository into a structured directory (configured via `clone_path` and `fork_path`). Tracked repositories are synced during `gh pt upgrade`.

```bash
gh pt repo clone <owner/repo>
gh pt repo fork <owner/repo>
```

### `source`
Clones a repository to a temporary workspace, leverages an AI agent to write a build script, compiles the binary, installs it, and saves the script in state for future updates.

```bash
gh pt source <owner/repo> --ai-cmd 'agy -p "%s"'
```

---

## Mechanics & Architecture

### Package Manager Detection
On Linux and FreeBSD, `gh-pt` detects available package managers via `exec.LookPath` and adjusts the default asset preference:

| Environment | Detected Via | Default Priority Routing |
|---|---|---|
| **Debian/Ubuntu** | `dpkg` in PATH | `.deb` → `apt-get install` or `dpkg -i` |
| **RHEL/Fedora** | `rpm` in PATH | `.rpm` → `dnf install` or `rpm -i` |
| **Arch Linux** | Neither `dpkg`/`rpm` | `.pkg.tar.zst` / `.pkg.tar.xz` → `pacman -U` |
| **FreeBSD** | `GOOS=freebsd` | `.pkg` / `.txz` → `pkg install` |
| **macOS** | `GOOS=darwin` | `.dmg` > `.tar.gz` > `.zip` |

### The Extraction Engine (`--extractor`)
`gh-pt` uses a cascading fallback system for extracting archives (e.g., `tar.gz`, `zip`), defined by the `--extractor` flag or config setting. The default precedence is `ouch,native,internal`.

1. **`ouch`**: If the [ouch](https://github.com/ouch-org/ouch) utility is installed, it is used first. It is extremely fast and handles almost every format securely.
2. **`native`**: Falls back to native OS binaries like `tar` and `unzip`.
3. **`internal`**: As a last resort, uses the embedded `mholt/archiver/v4` Go library.

*You can force a specific extractor, e.g., `--extractor="internal"`, or reorder them: `--extractor="native,internal"`.*

### State Management (`state.json`)
By default, successful operations are recorded in a state file located in your XDG Data directory (`~/.local/share/gh-pt/state.json`).

This file tracks:
- **Application Metadata:** Version, installed path, and source repository.
- **Replay State:** All flags used during the initial installation (e.g., `--type`, `--asset-binaries-regexp`, `--global`).
- **Pins:** Applications marked to be ignored during `upgrade`.
- **Compile Scripts:** Custom scripts generated by the `source` command.

Use `gh pt state edit` for an interactive UI to manage pins and tracked apps.

### Configuration (`config.yml`)
Configuration is stored in `~/.config/gh-pt/config.yml`. It defines defaults that can be overridden by CLI flags or `GH_PT_` prefixed environment variables.

**Configuration Precedence:** CLI Flag > Environment Variable > `config.yml` > Hardcoded Default.

**Schema Example:**
```yaml
# ~/.config/gh-pt/config.yml
install_path: "~/.local/bin"
global_path: "/usr/local/bin"
clone_path: "~/src"
fork_path: "~/src/forks"

# AI Configuration
ai_cmd: "agy -p \"%s\""
ai_interactive_cmd: ""

# Core Behavior
install_types: "deb,appimage,tar.gz,zip"
resolve_deps: true
no_deps: false
disable_prompts: false
no_save_state: false
wine: "off"
extractor: "default"
keep_suffixes: false
vt_api_key: ""
allow_prerelease: false
disable_icons: false
log_to_file: false
symlink: false
```

Use the `config` commands to manage settings:
```bash
gh pt config menu         # Interactive menu
gh pt config ls           # View all settings
gh pt config set <k> <v>  # Set a specific key
```
