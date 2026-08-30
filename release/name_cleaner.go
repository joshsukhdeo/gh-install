package release

import (
	"strings"
	"regexp"
)

// GenerateCleanName attempts to heuristically strip OS, Architecture, and Extension
// affixes from a binary name, returning a clean executable name.
func GenerateCleanName(binaryName, repoName, resolvedVersion string) string {
	// If it's already shorter than the repo name and has no weird chars, it's likely already clean (e.g. "rg" for "ripgrep")
	if len(binaryName) <= len(repoName) && !strings.ContainsAny(binaryName, "-_.") {
		return binaryName
	}

	clean := binaryName

	// We will aggressively extract any valid alpha-based extension so tokenization works.
	ext := ""
	if match := regexp.MustCompile(`(?i)(\.[a-z][a-z0-9]*)$`).FindStringSubmatch(clean); len(match) > 0 {
		ext = match[1]
		clean = clean[:len(clean)-len(ext)]
	}

	// Directly strip the exact resolved version, with or without 'v'
	if resolvedVersion != "" {
		noVStr := strings.TrimPrefix(resolvedVersion, "v")
		clean = strings.ReplaceAll(clean, "v"+noVStr, "")
		clean = strings.ReplaceAll(clean, noVStr, "")
	}

	// Regex to remove common affixes
	// We want to remove these words when they appear bounded by word boundaries or punctuation.
	affixPattern := `(?i)(?:^|[-_.])(?:linux|darwin|windows|mac|macos|apple|win|amd64|x86_64|x64|x86|arm64|aarch64|armv7|armv6|arm|386|i386|musl|gnu|unknown|pc|msvc)(?:[-_.]|$)`
	
	// Keep replacing until no more matches (since overlapping boundaries might prevent single-pass removal)
	for {
		newClean := regexp.MustCompile(affixPattern).ReplaceAllString(clean, "-")
		if newClean == clean {
			break
		}
		clean = newClean
	}

	// Clean up leftover punctuation
	clean = regexp.MustCompile(`[-_.]+$`).ReplaceAllString(clean, "")
	clean = regexp.MustCompile(`^[-_.]+`).ReplaceAllString(clean, "")
	clean = regexp.MustCompile(`[-_]{2,}`).ReplaceAllString(clean, "-")

	// Fallback to repoName if we stripped everything somehow
	if clean == "" {
		clean = repoName
	}

	return clean
}
