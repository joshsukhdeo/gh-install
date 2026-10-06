package selector

import (
	"regexp"
	"strings"

	"golang.org/x/sys/cpu"
)

var avxWordRe = regexp.MustCompile(`(?i)(?:^|[-_.])avx(?:[-_.]|$)`)

// DetectHostAVXLevel inspects the host CPU for AVX feature flags.
func DetectHostAVXLevel() string {
	if cpu.X86.HasAVX512 {
		return "avx512"
	}
	if cpu.X86.HasAVX2 {
		return "avx2"
	}
	if cpu.X86.HasAVX {
		return "avx"
	}
	return "none"
}

// AVXLevelRank converts an AVX level string to an integer rank for comparison.
func AVXLevelRank(level string) int {
	switch strings.ToLower(level) {
	case "avx512":
		return 3
	case "avx2":
		return 2
	case "avx":
		return 1
	default:
		return 0
	}
}

// AssetAVXRequirement returns the required AVX level for an asset name.
func AssetAVXRequirement(name string) string {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "noavx") || strings.Contains(lower, "no-avx") || strings.Contains(lower, "non-avx") {
		return "none"
	}
	if strings.Contains(lower, "avx512") || strings.Contains(lower, "avx-512") || strings.Contains(lower, "avx_512") {
		return "avx512"
	}
	if strings.Contains(lower, "avx2") || strings.Contains(lower, "avx-2") || strings.Contains(lower, "avx_2") {
		return "avx2"
	}
	if avxWordRe.MatchString(name) {
		return "avx"
	}
	return "none"
}

// IsAVXCompatible checks if an asset requiring a specific AVX level is compatible with the target level.
func IsAVXCompatible(assetName, targetLevel string) bool {
	req := AssetAVXRequirement(assetName)
	return AVXLevelRank(req) <= AVXLevelRank(targetLevel)
}
