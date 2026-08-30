# Conventions

- Follow standard Go formatting rules (enforced by `go fmt`).
- Avoid adding new dependencies without running `make tidy`.
- CLI arguments, flags, and descriptions are managed via the `kong` struct tags in the `cmd` package.
- Resolve any linting issues reported by `golangci-lint` before committing.