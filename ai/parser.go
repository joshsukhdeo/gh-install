package ai

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// ManifestTemplate is injected into the AI prompt. The AI fills in simple
// directives — it never writes JSON. A deterministic Go parser constructs
// the structured payload from these directives.
const ManifestTemplate = `
OUTPUT FORMAT — follow EXACTLY. Do NOT output JSON.

DEPENDENCY MANIFEST (output as a single code block with language tag "ghpt-manifest"):
` + "```ghpt-manifest" + `
toolchain <primary build tool, e.g. gcc, cargo, go, make>
dep <package-name> <resolver: apt|dnf|pacman|mise|uv|cargo|vcpkg>
dep <package-name> <resolver>
` + "```" + `

COMPILATION SCRIPT (output as a single bash code block):
` + "```bash" + `
#!/bin/bash
set -euo pipefail
# Your build commands here
` + "```" + `

Rules:
- Each "dep" line has exactly two arguments: the package name and the resolver.
- The "toolchain" line has exactly one argument.
- Do NOT wrap the manifest in JSON. Use the directive format above.
- Do NOT add comments or extra text inside the ghpt-manifest block.
`

var (
	// ErrMissingManifest indicates that no ghpt-manifest code block was found.
	ErrMissingManifest = errors.New("missing dependency manifest (ghpt-manifest code block)")
	// ErrMissingScript indicates that no Bash/sh compilation script code block was found.
	ErrMissingScript = errors.New("missing compilation script (bash/sh code block)")
	// ErrMalformedDirective indicates a directive line could not be parsed.
	ErrMalformedDirective = errors.New("malformed directive in manifest block")

	// Aliases for backward compatibility.
	ErrMissingJSONBlock = ErrMissingManifest
	ErrMissingBashBlock = ErrMissingScript
	// Keep for callers that previously caught this.
	ErrMalformedJSON = ErrMalformedDirective
)

// ManifestJSONTemplate is the schema/example template for manifest.json
const ManifestJSONTemplate = `{
  "dependencies": [
    {
      "name": "<package-name>",
      "manager": "<apt|dnf|pacman|brew|apk|zypper|vcpkg|cargo>",
      "version": "<optional: e.g. >=1.0>",
      "requirement": "<optional: min|max|exact|suggested>"
    }
  ]
}`

// BodyTemplate is the template for body.sh (AI-generated build logic).
// This is NOT a complete script - header and footer are added by gh-pt at runtime.
const BodyTemplate = `### DEPENDENCIES ARE INSTALLED BY GH-PT VIA CONTAINERIZATION (per manifest.json)
### THIS body.sh RUNS INSIDE THE BUILD CONTAINER — ONLY COMPILE & INSTALL HERE
#
# IMPORTANT: This is body.sh, NOT a complete script.
# - NO shebang (header provides #!/usr/bin/env bash)
# - NO header/footer (gh-pt adds them at runtime)
# - Call 'ghpt helper --install' DIRECTLY for file operations
# - Do NOT reference env vars ($GHPT_TARGET_BIN_DIR etc.)
# - Do NOT use cp, mv, mkdir, or other filesystem operations
#
# AI SANDBOX RESTRICTION: The 'ghpt' command detects AI execution context by
# checking the process tree for a 'gh-pt' ancestor. If found, it ONLY permits
# 'ghpt helper' subcommands. All other ghpt commands (install, source, upgrade,
# etc.) are BLOCKED with exit code 126. This is built into the main gh-pt binary,
# not a separate wrapper.
#
# MANIFEST MANAGEMENT (use these to declare build dependencies):
#   ghpt helper --append-manifest "manager=pkg@version,manager=pkg@version"  # Add deps
#   ghpt helper --get-manifest              # View current manifest.json
#   ghpt helper --validate-manifest         # Validate manifest schema
#
# VERSION CONSTRAINTS (append after @):
#   @latest or omitted        → latest available
#   @1.2.3                    → exact version
#   @1.2.3-                   → minimum (>= 1.2.3)
#   @1.2.3+                   → maximum (<= 1.2.3)
#   @~1.2.3                   → approximately (~1.2.x)
#
# EXAMPLE:
#   ghpt helper --append-manifest "apt=gcc@13-,apt=cmake@latest,apt=libopencv-dev@4.5-"
#
# FILE INSTALLATION (use this exclusively):
#   ghpt helper --install "bin=./build/myapp"        # Installs to $GHPT_TARGET_BASE_DIR/bin
#   ghpt helper --install "libs=./build/libfoo.so"   # Installs to $GHPT_TARGET_BASE_DIR/libs
#   ghpt helper --install "share=./data/config"      # Installs to $GHPT_TARGET_BASE_DIR/share
#
# FORBIDDEN IN body.sh:
# - Package manager calls (apt install, dnf install, brew install, etc.)
# - Direct filesystem operations (cp, mv, mkdir, chmod, chown)
# - Environment variable references ($GHPT_TARGET_BIN_DIR, etc.)
# - Privilege escalation (sudo, su, pkexec)
# - Writes to /usr/local, /usr/bin, /etc, /var, /opt
#
# CROSS-COMPILATION SUPPORT:
#   When building with --target-os or --target-arch flags, the container will have
#   cross-compilation tools configured. Environment variables like CC, CXX, GOOS,
#   GOARCH will be set automatically. Use standard build tools (make, cmake, etc.)
#   and they will produce binaries for the target platform.
#
# EXAMPLE body.sh:
#   # Build a CMake project
#   cmake -B build -DCMAKE_INSTALL_PREFIX=/usr/local
#   cmake --build build -j$(nproc)
#
#   # Install built artifacts
#   ghpt helper --install "bin=./build/bin/myapp"
#   ghpt helper --install "libs=./build/lib/libfoo.so"
#   ghpt helper --install "share=./build/share/myapp"
`

// CompileScriptTemplate is kept for backward compatibility
// New code should use BodyTemplate instead
const CompileScriptTemplate = BodyTemplate

// HeaderTemplate is prepended to body.sh at runtime to create compile.sh
const HeaderTemplate = `#!/usr/bin/env bash
set -euo pipefail

# ============================================================================
# gh-pt Compile Script - Runtime Reconstructed
# Generated by gh-pt from header + body.sh + footer
# ============================================================================

# Minimal environment - delegate to ghpt helper, don't rely on env vars
export GHPT_TARGET_BASE_DIR="{{.InstallPrefix}}"
export GHPT_REPO_PATH="{{.RepoPath}}"
export GHPT_LOG_FILE="{{.RepoPath}}/.ghpt/compile.log"
export GHPT_INSTALL_LOG="{{.RepoPath}}/.ghpt/install.log"

# Ensure target directories exist via ghpt helper
ghpt helper --install "bin=.ghpt/.keep" 2>/dev/null || true
ghpt helper --install "libs=.ghpt/.keep" 2>/dev/null || true
ghpt helper --install "share=.ghpt/.keep" 2>/dev/null || true

# Logging function
log() {
    echo "[$(date -Iseconds)] $*" | tee -a "$GHPT_LOG_FILE"
}

log "=== Compile Script Started ==="
log "Repository: {{.Repository}}"
log "Version: {{.Version}}"

# --- BEGIN body.sh ---
`

// FooterTemplate is appended to body.sh at runtime to complete compile.sh
const FooterTemplate = `
# --- END body.sh ---

log "=== Compile Script Finished ==="
log "Installed files logged to: $GHPT_INSTALL_LOG"

# Count installed files
if [ -f "$GHPT_INSTALL_LOG" ]; then
    file_count=$(wc -l < "$GHPT_INSTALL_LOG")
    log "Total files installed: $file_count"
fi

exit 0
`

// Dependency represents a package dependency and the resolver required to install it.
type Dependency struct {
	Name        string `json:"name"`
	Resolver    string `json:"resolver,omitempty"`
	Manager     string `json:"manager,omitempty"`
	Version     string `json:"version,omitempty"`
	Requirement string `json:"requirement,omitempty"`
}

func (d *Dependency) GetResolver() string {
	if d.Manager != "" {
		return d.Manager
	}
	return d.Resolver
}

type Manifest struct {
	Dependencies []Dependency `json:"dependencies"`
	Toolchain    string       `json:"toolchain,omitempty"`
}

// CompilePayload represents the parsed 2-stage AI output containing dependencies,
// toolchain, and the compilation script.
type CompilePayload struct {
	Dependencies []Dependency `json:"dependencies"`
	Toolchain    string       `json:"toolchain,omitempty"`
	Script       string       `json:"script"`
	ManifestJSON string       `json:"manifest_json,omitempty"`
}

var (
	manifestJSONBlockRegex = regexp.MustCompile("(?is)```json\\b[^\\r\\n]*\\r?\\n?([^{\\n]*\\{.*?\"dependencies\".*?\\})\\s*```")
	manifestBlockRegex     = regexp.MustCompile("(?is)```ghpt-manifest\\b[^\\r\\n]*\\r?\\n?(.*?)```")
	bashBlockRegex         = regexp.MustCompile("(?is)```(?:bash|sh)\\b[^\\r\\n]*\\r?\\n?(.*?)```")
)

// parseDirectiveBlock deterministically constructs a CompilePayload from
// simple "toolchain" and "dep" directive lines. The AI never writes JSON.
func parseDirectiveBlock(block string) ([]Dependency, string, error) {
	var deps []Dependency
	var toolchain string

	scanner := bufio.NewScanner(strings.NewReader(block))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		directive := strings.ToLower(fields[0])

		switch directive {
		case "toolchain":
			if len(fields) < 2 {
				return nil, "", fmt.Errorf("%w: toolchain requires an argument: %q", ErrMalformedDirective, line)
			}
			toolchain = fields[1]
		case "dep":
			if len(fields) < 3 {
				return nil, "", fmt.Errorf("%w: dep requires <name> <resolver>: %q", ErrMalformedDirective, line)
			}
			deps = append(deps, Dependency{Name: fields[1], Resolver: fields[2], Manager: fields[2]})
		default:
			return nil, "", fmt.Errorf("%w: unknown directive %q: %q", ErrMalformedDirective, directive, line)
		}
	}

	if deps == nil {
		deps = []Dependency{}
	}

	return deps, toolchain, nil
}

// ParseAIOutput extracts the dependency manifest (JSON or ghpt-manifest directives) and
// compilation script (Bash/sh) from AI-generated response text.
func ParseAIOutput(raw string) (*CompilePayload, error) {
	var deps []Dependency
	var toolchain string
	var manifestJSON string
	manifestFound := false

	// 1. Try ghpt-manifest directives first if present
	manifestMatch := manifestBlockRegex.FindStringSubmatch(raw)
	if len(manifestMatch) >= 2 {
		manifestStr := strings.TrimSpace(manifestMatch[1])
		if manifestStr == "" {
			return nil, fmt.Errorf("%w: empty manifest block", ErrMalformedDirective)
		}
		var err error
		deps, toolchain, err = parseDirectiveBlock(manifestStr)
		if err != nil {
			return nil, err
		}
		manifestFound = true
		m := Manifest{Dependencies: deps, Toolchain: toolchain}
		formatted, _ := json.MarshalIndent(m, "", "  ")
		manifestJSON = string(formatted)
	}

	// 2. If no ghpt-manifest block, try JSON code block
	if !manifestFound {
		jsonMatch := manifestJSONBlockRegex.FindStringSubmatch(raw)
		if len(jsonMatch) >= 2 {
			jsonStr := strings.TrimSpace(jsonMatch[1])
			var m Manifest
			if err := json.Unmarshal([]byte(jsonStr), &m); err == nil {
				for i := range m.Dependencies {
					if m.Dependencies[i].Resolver == "" && m.Dependencies[i].Manager != "" {
						m.Dependencies[i].Resolver = m.Dependencies[i].Manager
					}
					if m.Dependencies[i].Manager == "" && m.Dependencies[i].Resolver != "" {
						m.Dependencies[i].Manager = m.Dependencies[i].Resolver
					}
				}
				deps = m.Dependencies
				toolchain = m.Toolchain
				formatted, _ := json.MarshalIndent(m, "", "  ")
				manifestJSON = string(formatted)
				manifestFound = true
			}
		}
	}

	// 3. Fallback: check if raw itself is valid JSON manifest
	if !manifestFound {
		var m Manifest
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "{") && strings.Contains(trimmed, `"dependencies"`) {
			if err := json.Unmarshal([]byte(trimmed), &m); err == nil && len(m.Dependencies) > 0 {
				for i := range m.Dependencies {
					if m.Dependencies[i].Resolver == "" && m.Dependencies[i].Manager != "" {
						m.Dependencies[i].Resolver = m.Dependencies[i].Manager
					}
					if m.Dependencies[i].Manager == "" && m.Dependencies[i].Resolver != "" {
						m.Dependencies[i].Manager = m.Dependencies[i].Resolver
					}
				}
				deps = m.Dependencies
				toolchain = m.Toolchain
				formatted, _ := json.MarshalIndent(m, "", "  ")
				manifestJSON = string(formatted)
				manifestFound = true
			}
		}
	}

	if !manifestFound {
		return nil, ErrMissingManifest
	}

	// 4. Find compilation script
	bashMatch := bashBlockRegex.FindStringSubmatch(raw)
	if len(bashMatch) < 2 {
		return nil, ErrMissingScript
	}

	script := strings.TrimSpace(bashMatch[1])
	if script == "" {
		return nil, ErrMissingScript
	}

	return &CompilePayload{
		Dependencies: deps,
		Toolchain:    toolchain,
		Script:       script,
		ManifestJSON: manifestJSON,
	}, nil
}

// ValidateBodyScript validates the body.sh script using AST-based parsing.
	// It checks that the script only contains allowed commands and patterns.
	func ValidateBodyScript(bodyPath string) error {
		f, err := os.Open(bodyPath)
		if err != nil {
			return fmt.Errorf("failed to open body.sh: %w", err)
		}
		defer f.Close()

		parser := syntax.NewParser()
		file, err := parser.Parse(f, "")
		if err != nil {
			return fmt.Errorf("failed to parse body.sh: %w", err)
		}

		// Use file to avoid unused variable error
		_ = file

		// Allowed commands that can appear in body.sh
	allowedCommands := map[string]bool{
		"cmake":        true,
		"make":         true,
		"gcc":          true,
		"g++":          true,
		"clang":        true,
		"clang++":      true,
		"go":           true,
		"cargo":        true,
		"rustc":        true,
		"ghpt":         true,
		"bash":         true,
		"sh":           true,
		"python3":      true,
		"python":       true,
		"pip":          true,
		"npm":          true,
		"yarn":         true,
		"pnpm":         true,
		"meson":        true,
		"ninja":        true,
		"bazel":        true,
		"cc":           true,
		"c++":          true,
		"ar":           true,
		"ranlib":       true,
		"strip":        true,
		"ld":           true,
		"objcopy":      true,
		"objdump":      true,
		"nm":           true,
		"readelf":      true,
		"pkg-config":   true,
		"autoreconf":   true,
		"autoconf":     true,
		"automake":     true,
		"libtool":      true,
		"patch":        true,
		"sed":          true,
		"awk":          true,
		"grep":         true,
		"find":         true,
		"mkdir":        true,
		"cp":           true,
		"rsync":        true,
		"tar":          true,
		"unzip":        true,
		"git":          true,
	}

	// Forbidden patterns that should not appear in body.sh
	forbiddenPatterns := []string{
		// Network commands
		`curl`, `wget`, `ssh`, `scp`, `nc`, `socat`, `ncat`, `netcat`, `telnet`,
		// Privilege escalation
		`sudo`, `su`, `pkexec`, `doas`, `runuser`, `run0`,
		// Shell escapes
		`eval`, `exec`, `source`,
		// Package managers (should use manifest)
		`apt(-get)?\s+install`, `dnf\s+install`, `pacman\s+-S`, `brew\s+install`,
		`cargo\s+install`, `go\s+install`, `pip\s+install`, `npm\s+install`,
		`yarn\s+install`, `pnpm\s+install`,
		// Direct system writes
		`/usr/local/`, `/usr/bin/`, `/usr/lib/`, `/opt/`,
		`/etc/`, `/var/`, `/sbin/`, `/usr/sbin/`,
		// Direct filesystem operations (should use ghpt helper --install)
		`\bcp\b`, `\bmv\b`, `\bmkdir\b`, `\bchmod\b`, `\bchown\b`,
		// Privilege escalation
		`\bsudo\b`, `\bsu\b`, `\bpkexec\b`, `\bdoas\b`,
		// Environment variable references to target dirs
		`GHPT_TARGET_BIN_DIR`, `GHPT_TARGET_LIB_DIR`, `GHPT_TARGET_BASE_DIR`,
		`GHPT_REPO_PATH`, `GHPT_LOG_FILE`, `GHPT_INSTALL_LOG`,
	}

	return ValidateScript(bodyPath, allowedCommands, forbiddenPatterns)
}

// ValidateScript checks a script against allowed commands and forbidden patterns
func ValidateScript(scriptPath string, allowedCommands map[string]bool, forbiddenPatterns []string) error {
	f, err := os.Open(scriptPath)
	if err != nil {
		return fmt.Errorf("failed to open script: %w", err)
	}
	defer f.Close()

	parser := syntax.NewParser()
	file, err := parser.Parse(f, "")
	if err != nil {
		return fmt.Errorf("failed to parse script: %w", err)
	}

	var errs []string

	// Walk the AST and check each command
	syntax.Walk(file, func(n syntax.Node) bool {
		// Handle nil nodes
		if n == nil {
			return true
		}
		// Get line number for error reporting
		line := ""
		if pos := n.Pos(); pos.IsValid() {
			line = fmt.Sprintf(" (line %d)", pos.Line())
		}

		// Check CallExpr (command calls)
		if callExpr, ok := n.(*syntax.CallExpr); ok {
			// Get the command name
			var cmdName string
			if len(callExpr.Args) > 0 && callExpr.Args[0] != nil {
				cmdName = callExpr.Args[0].Lit()
			}

			if cmdName != "" {
				// Check if command is allowed
				if !allowedCommands[cmdName] {
					// Check if it's a forbidden pattern
					for _, pattern := range forbiddenPatterns {
						matched, _ := regexp.MatchString(pattern, cmdName)
						if matched {
							errs = append(errs, fmt.Sprintf("forbidden command: %s (matches %s)%s", cmdName, pattern, line))
							break
						}
					}
				}
				
				// Check all arguments for forbidden patterns
				for _, arg := range callExpr.Args {
					argLit := arg.Lit()
					for _, pattern := range forbiddenPatterns {
						matched, _ := regexp.MatchString(pattern, argLit)
						if matched {
							errs = append(errs, fmt.Sprintf("forbidden argument: %s (matches %s)%s", argLit, pattern, line))
							break
						}
					}
				}
			}
		}
		
		// Check ParamExp (parameter expansions like $VAR or ${VAR})
		if paramExp, ok := n.(*syntax.ParamExp); ok {
			if paramExp.Param != nil {
				varName := paramExp.Param.Value
				for _, pattern := range forbiddenPatterns {
					matched, _ := regexp.MatchString(pattern, varName)
					if matched {
						errs = append(errs, fmt.Sprintf("forbidden variable reference: $%s (matches %s)%s", varName, pattern, line))
						break
					}
				}
			}
		}
		
		return true
	})

	if len(errs) > 0 {
		return fmt.Errorf("validation failed: %s", strings.Join(errs, "; "))
	}

	return nil
}
