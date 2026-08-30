# Core

- **Overview**: Go CLI tool (`gh-install`) for installing GitHub repository releases.
- **Entrypoint**: `main.go` using the `github.com/alecthomas/kong` CLI framework.
- **Project Domains**:
  - `mem:tech_stack`: Languages, frameworks, and tools.
  - `mem:suggested_commands`: Common workflows and Makefile targets.
  - `mem:conventions`: Coding style and dependencies.
  - `mem:task_completion`: Requirements for completing tasks.

- **Packages**:
  - `cmd/`: CLI commands and flags definition.
  - `release/`, `selector/`, `params/`: Core business logic for fetching releases, selecting assets, and managing parameters.