package ai

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
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

// CompileScriptTemplate is the starting template for compile.sh
const CompileScriptTemplate = `#!/usr/bin/env bash
set -euo pipefail
exec > >(tee -a compile.log) 2>&1

### SKIP INSTALLING DEPENDENCIES as that step occurs prior by gh-pt using the manifest.json
#
# NOTE: A symlink named "install-dir" is located in .ghpt (i.e. .ghpt/install-dir).
# Treat "install-dir" the same as "/usr/local/" or "$HOME/.local" and place binaries
# into the same structure:
#   install-dir/bin
#   install-dir/libs
#   install-dir/share
#   install-dir/state
# Do NOT install files directly into /usr/local or ~/.local outside of install-dir!
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
