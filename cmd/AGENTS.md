# cmd/ — CLI Commands and Routing

## Files
- `root.go` — Main entry, root warning banner (EUID=0), config init
- `router.go` — Command dispatch, upgrade flag routing, helper dispatch
- `update.go` — Upgrade logic, GraphQL batch query with targeted filter
- `helper.go` — 12 helper subcommands for AI sandbox access
- `wrapper/main.go` — PID-validated wrapper binary (0100 perms)

## Key Patterns
- **Upgrade routing**: Rejects install-only flags, accepts only UpgradeFlags
- **GraphQL filter**: `r.Repository != ""` limits batch to single repo
- **Wrapper gate**: Blocks non-helper if parent PID dead
- **Root banner**: Yellow warning, `sudo -v` cache for `--global`

## Tests
- `update_test.go` — Targeted upgrade filter test