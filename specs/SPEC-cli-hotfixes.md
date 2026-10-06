# Spec: CLI Hotfixes

## Objective
Address three immediate, high-priority bugs and security issues in the `gh-pt` CLI before moving on to larger architectural overhauls:
1. **Fix `ghpt show` routing**: When a user runs `ghpt show <repo>`, it currently falls through to an interactive install menu instead of displaying repository metadata (versions, assets, description, readme).
2. **Fix Wrapper/Completions Shebang Bug**: Running `/home/tay/bin/gh-pt` results in a `bad interpreter: /bin/bash\necho` error because a literal `\n` was written instead of an actual newline during the wrapper script generation.
3. **Secure GitHub Token Handling**: Prevent the GitHub API token from being passed via command-line arguments (which leaks it to shell history). Restrict token ingestion exclusively to environment variables (e.g., `GITHUB_TOKEN`, `GH_TOKEN`) and the XDG config file.
4. **Active Threat AI Sandbox**: Prevent rogue or poisoned AI agents from executing recursive `gh-pt` installations. Implement kernel-level process tree traversal. If a running `gh-pt` instance detects another `gh-pt` process anywhere in its parent lineage, it must lock down completely, permitting only `helper` commands.

## Tech Stack
- **Language**: Go 1.24+
- **CLI Framework**: `github.com/alecthomas/kong`

## Commands
```bash
# Build binary
make build

# Verify show command behavior
./bin/gh-pt show xingkongliang/skills-manager

# Verify wrapper generation
# (Command depends on whether Makefile or Go code generates ~/bin/gh-pt)

# Verify token security
./bin/gh-pt install --token=1234 # Should error: unknown flag
```

## Project Structure
```
cmd/
  show.go           -> Isolate execution flow; prevent fallthrough to interactive install menus
  root.go/params.go -> Remove GitHub token CLI flags; ensure token is only read from config/env
scripts/ / Makefile -> (Or Go source) Fix the string escaping that produces `#!/bin/bash\necho` in wrappers
specs/
  SPEC-cli-hotfixes.md -> This document
```

## Code Style
Follow idiomatic Go style and Kong framework conventions.
```go
// Example: Token configuration without a CLI flag exposing it
type Config struct {
    // Config file mapping, no Kong flag tags
    GitHubToken string `yaml:"github_token"`
}

// In params.go or similar, fetch from env explicitly if not in config
func GetToken(cfg *Config) string {
    if token := os.Getenv("GITHUB_TOKEN"); token != "" {
        return token
    }
    return cfg.GitHubToken
}
```

## Testing Strategy
- **`show` Command**: Unit test asserting that invoking the `show` Kong context executes the display logic and returns immediately, without invoking `pUI.Start()` or `selector.Run()`.
- **Wrapper Bug**: If the wrapper is generated via Go text/template or string concatenation, add a unit test validating the first line correctly resolves to `#!/bin/bash` followed by a true newline character `\n`.
- **Token Security**: Integration test verifying that passing `--token` or similar to the CLI returns an unknown flag error, and that the internal GitHub client initializes correctly using only `env` or mock config file.
- **AI Sandbox**: Unit test mocking process lineage (or testing against a live subprocess execution) to verify that if `gh-pt` is present in the parent tree, any standard install command returns a hard security lockdown error.

## Boundaries
- **Always**:
  - Keep the `show` command strictly read-only; it should never alter state or prompt for installations.
  - Fail fast and clearly if a GitHub token is missing when required, instructing the user to use the config file or environment variable.
  - Enforce process tree traversal natively across target operating systems (using robust libraries or syscalls) without crashing if a process table lookup temporarily fails.
- **Ask first**:
  - If fixing the wrapper script requires changing how `gh-pt` distributes or installs itself globally.
- **Never**:
  - Allow the GitHub token to be defined in Kong CLI `type:"string"` flags that could be captured in `.bash_history`.
  - Trust environment variables for the AI Sandbox. Rely exclusively on kernel-reported process ancestry.

## Success Criteria
1. Running `ghpt show xingkongliang/skills-manager` correctly prints the repository metadata (versions, assets, description, readme) and exits cleanly.
2. The interactive installation TUI does not appear during a `show` command.
3. The `gh-pt` wrapper script at `~/bin/gh-pt` correctly executes without the `bad interpreter` literal string formatting error.
4. The CLI strictly rejects GitHub tokens passed as command-line arguments and successfully reads them from the environment or config file.
5. A running `gh-pt` process successfully detects if it is a descendant of another `gh-pt` process and completely locks out all non-helper functionality, rendering recursive AI attacks impossible.

## Open Questions
1. Does a Go function inside the `gh-pt` binary generate the `/home/tay/bin/gh-pt` wrapper script (e.g., during self-installation/update), or is that script generated by an external `Makefile` or install script?
