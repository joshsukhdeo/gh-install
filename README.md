# gh-install

A GitHub CLI extension for rapidly and intelligently installing GitHub repository releases. 

`gh-install` is intended for quickly installing release binaries for projects that do not distribute using Homebrew, apt, or other traditional package managers. It handles extraction, execution matching, system package installations (like `.deb`, `.rpm`), and state management for seamless future updates.

## Installation

```bash
gh extension install joshsukhdeo/gh-install
```

---

## Features & Capabilities

- **Intelligent Asset Selection:** Automatically prioritizes system packages (`.deb`, `.rpm`) and containerized formats (Snap, Flatpak, AppImage) over raw archives depending on your OS.
- **State Management & Updates:** Tracks installed binaries so you can update them all later with a single command.
- **Dependency Resolution:** Can automatically resolve and install dependencies for `.deb`/`.rpm` via `apt` or `dnf`.
- **Cross-Platform:** Supports Linux, macOS, Windows, and FreeBSD.
- **Wine Support:** Can seamlessly pull and install Windows `.exe`/`.msi` binaries on Linux and FreeBSD systems if `--allow-wine` is enabled.
- **Clean Naming:** Automatically strips messy hardware/OS tags (like `-x86_64-linux`) and redundant version strings from the final installed binary name.

---

## Usage

```bash
$ gh install --help
Usage: gh-install [<repository>] [flags]

Install binaries for a Github repository release interactively or
non-interactively.

    Intended for quickly installing release binaries for projects that do not distribute
    using Homebrew or other package managers.

Arguments:
  [<repository>]    Github repository in OWNER/REPOSITORY_NAME format
                    ($GH_INSTALL_REPOSITORY).

Flags:
  -h, --help                    Show context-sensitive help.
  -U, --update-all              Update all installed applications (user and
                                global) ($GH_INSTALL_UPDATE_ALL).
  -u, --update                  Update user installations (add -g for global
                                only) ($GH_INSTALL_UPDATE).
  -p, --target-path="~/.local/bin"
                                Target installation directory (default:
                                ~/.local/bin or /usr/local/bin if --global)
                                ($GH_INSTALL_TARGET_PATH).
  -l, --log-level="info"        Log level ($GH_INSTALL_LOG_LEVEL).
  -f, --log-format="console"    Log output format ($GH_INSTALL_LOG_FORMAT).
      --version                 Show version ($GH_INSTALL_VERSION).

Interactive Mode
  -i, --interactive      Use interactive installation. If true, all non-log
                         related flags are ignored ($GH_INSTALL_INTERACTIVE).
  -r, --prompt-rename    Prompt to strip OS/hardware affixes from long binary
                         names ($GH_INSTALL_PROMPT_RENAME).
      --[no-]log-quiet-interactive
                         Quiet log in interactive mode
                         ($GH_INSTALL_LOG_QUIET_INTERACTIVE)

State Management
  --list-saved-state         List saved state in a user-friendly format
                             ($GH_INSTALL_LIST_SAVED_STATE).
  --edit-saved-state         Edit saved state (enable/disable updates or remove
                             apps) ($GH_INSTALL_EDIT_SAVED_STATE).
  --rm-saved-state=STRING    Remove a saved app from state by repository slug or
                             binary name ($GH_INSTALL_RM_SAVED_STATE).

Non-interactive Mode
  -v, --release-version="latest"
                                   Repository release tag (version) to install
                                   ($GH_INSTALL_RELEASE_VERSION).
  -a, --release-asset=STRING       Name of repository release asset to download.
                                   If not set, --release-asset-regexp is used
                                   ($GH_INSTALL_RELEASE_ASSET).
  -A, --release-asset-regexp=STRING
                                   Regular expression matching release asset to
                                   download ($GH_INSTALL_RELEASE_ASSET_REGEXP).
  -T, --format=deb,snap,flatpak,appimage,7z,t(ar\.)?([gxl]z|bz2?|zst),tar(\.lzma)?,zip,py,ts,js,none,...
                                   Comma-separated list of types to match and
                                   prioritize ($GH_INSTALL_TYPE).
      --all                        Install all matched assets instead of just
                                   the first one ($GH_INSTALL_ALL).
  -b, --asset-binaries=ASSET-BINARIES,...
                                   If release asset is an archive - names
                                   of a binaries in the archive to install.
                                   If not set, --install-binary-regexp is used
                                   ($GH_INSTALL_ASSET_BINARIES).
  -B, --asset-binaries-regexp=STRING
                                   If release asset is an archive - regular
                                   expression matching binaries in the archive
                                   to install. If not set, repository name is
                                   used ($GH_INSTALL_ASSET_BINARIES_REGEXP).
  -g, --global                     Install globally (e.g. /usr/local/bin)
                                   instead of user bin ($GH_INSTALL_GLOBAL).
  -y, --add-deps                   Automatically resolve and install
                                   dependencies without prompting
                                   ($GH_INSTALL_ADD_DEPS).
  -n, --no-deps                    Do not install dependencies (use dpkg/rpm
                                   directly) ($GH_INSTALL_NO_DEPS).
  -t, --rename=KEY=VALUE;...       Rename binaries installed at target path,
                                   "<asset archive binary | asset>=<renamed
                                   binary>;..." ($GH_INSTALL_RENAME)
  -D, --disable-prompts            Disable all interactive prompts
                                   ($GH_INSTALL_DISABLE_PROMPTS).
  -S, --no-save-state              Do not save installation to state
                                   (prevents tracking for updates)
                                   ($GH_INSTALL_NO_SAVE_STATE).
      --allow-wine                 Allow installing Windows executables on
                                   Linux/macOS/FreeBSD ($GH_INSTALL_ALLOW_WINE).
      --[no-]target-path-create    Create target installation
                                   directory if it does not exist
                                   ($GH_INSTALL_TARGET_PATH_CREATE).
  -o, --overwrite                  Overwrite target binaries
                                   ($GH_INSTALL_OVERWRITE).
```

---

## State Management & Update System

By default, every successful installation is saved to an internal `state.json` file inside your XDG Data directory. This tracks the repository, current version, target path, and scope (User/Global).

You can instantly update all tracked applications by running:
```bash
gh install -U
```
*(`-U` updates all global and user packages. `-u` updates only user packages. `-u -g` updates only global packages).*

To view and manage your current state:
- `gh install --list-saved-state`: Prints a beautiful table of all currently tracked applications.
- `gh install --edit-saved-state`: Launches an interactive terminal UI to enable/disable automatic updates for specific apps, or delete them from the tracker.
- `gh install --rm-saved-state="fzf"`: Immediately drops an application from state tracking.

If you are running `gh install` in a temporary script and don't want to track it for updates, pass the `-S` (`--no-save-state`) flag.

---

## Configuration & Environment Variables

All CLI flags can be set via environment variables (prefixed with `GH_INSTALL_`) or a YAML configuration file located at `~/.config/gh-install/config.yml`.

Example `config.yml`:
```yaml
install_types: "deb,appimage,tar.gz,zip"
add_deps: true
allow_wine: false
prompt_rename: true
```

The configuration precedence is: `CLI Argument > Environment Variable > config.yml > Default`.

---

## Topgrade Integration

`gh-install` can easily be integrated with [Topgrade](https://github.com/topgrade-rs/topgrade) to keep all your installed binaries up to date automatically alongside your system packages. Just add the following to your `topgrade.toml` under the `[custom_commands]` block:

```toml
[custom_commands]
"gh-install" = "gh install -U"
```
