# params/ — CLI Flag Definitions

## Files
- `params.go` — All flag structs and validation

## Key Structs
- `UpgradeFlags` — Dedicated struct (replaces CommonInstallFlags embed)
  - Accepts: `-u`/`-g`, `-D`, `--extractor`, `-f`, `--dry-run`, `--verify-checksum`, `--skip-vt-sandbox`, `--fallback-releases`, `--warn-unmapped-assets`, `--progress-bar`
  - Rejects: `--target-base-dir`, `--symlink`, `--ai`, `--ai-cmd`, etc.
- `HelperCmd` — 12 boolean flags + `--target-base-dir`, `-g`
- `CommonInstallFlags` — Used by install command only

## Validation
- Path traversal protection on `--install`
- Mutex groups for conflicting flags