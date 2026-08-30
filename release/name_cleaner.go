package release

import (
	"strings"
	"regexp"
)

// GenerateCleanName attempts to heuristically strip OS, Architecture, and Extension
// affixes from a binary name, returning a clean executable name.
func GenerateCleanName(binaryName, repoName string) string {
	// If it's already shorter than the repo name and has no weird chars, it's likely already clean (e.g. "rg" for "ripgrep")
	if len(binaryName) <= len(repoName) && !strings.ContainsAny(binaryName, "-_.") {
		return binaryName
	}

	clean := binaryName

	// Common OS and Arch strings to strip
	affixes := []string{
		"linux", "darwin", "windows", "mac", "macos", "apple", "win",
		"amd64", "x86_64", "x64", "x86", "arm64", "aarch64", "armv7", "armv6", "arm", "386", "i386",
		"musl", "gnu", "unknown", "pc", "msvc",
	}

	// Remove common extensions first (like .exe for logic, though we might add it back later if on windows)
	exts := []string{".exe", ".bin", ".elf"}
	for _, ext := range exts {
		if strings.HasSuffix(strings.ToLower(clean), ext) {
			clean = clean[:len(clean)-len(ext)]
		}
	}

	// We want to split the name by common delimiters (- or _)
	// But we have to be careful not to strip parts of the actual app name like "docker-compose"
	// Heuristic: If a token matches a known OS/Arch affix exactly, remove it.
	tokens := regexp.MustCompile(`[-_]`).Split(clean, -1)
	var finalTokens []string
	
	for _, token := range tokens {
		isAffix := false
		lowerToken := strings.ToLower(token)
		for _, affix := range affixes {
			if lowerToken == affix {
				isAffix = true
				break
			}
		}
		if !isAffix {
			finalTokens = append(finalTokens, token)
		}
	}

	if len(finalTokens) > 0 {
		clean = strings.Join(finalTokens, "-")
	}

	// Fallback to repoName if we stripped everything somehow
	if clean == "" {
		clean = repoName
	}

	return clean
}
