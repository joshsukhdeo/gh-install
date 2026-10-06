package selector

import (
	"fmt"
	"runtime"
	"strings"
)

// AssetCategory represents one of the 3 major asset categories:
// 1. Executables / Archives (Native, Wine, Foreign Arch)
// 2. Sidecars
// 3. Checksums
type AssetCategory string

const (
	CategoryExecutable AssetCategory = "Executable"
	CategorySidecar    AssetCategory = "Sidecar"
	CategoryChecksum   AssetCategory = "Checksum"
)

// CategorizeAsset classifies a release asset into Category and TUI Tag.
func CategorizeAsset(name string) (AssetCategory, string) {
	lower := strings.ToLower(name)

	// Category 3: Checksums & Signatures
	if strings.Contains(lower, "checksum") ||
		strings.Contains(lower, "sha256") ||
		strings.Contains(lower, "sha512") ||
		strings.Contains(lower, "sha1") ||
		strings.Contains(lower, "md5") ||
		strings.Contains(lower, "hashes") ||
		strings.HasSuffix(lower, ".sig") ||
		strings.HasSuffix(lower, ".asc") ||
		strings.HasSuffix(lower, ".pem") {
		return CategoryChecksum, "[Checksum]"
	}

	// Category 2: Sidecars
	if strings.HasSuffix(lower, ".so") ||
		strings.Contains(lower, ".so.") ||
		strings.HasSuffix(lower, ".pak") ||
		strings.HasSuffix(lower, ".dylib") ||
		strings.HasSuffix(lower, ".dll") ||
		strings.HasSuffix(lower, ".h") ||
		strings.HasSuffix(lower, ".hpp") {
		return CategorySidecar, "[Sidecar]"
	}

	// Category 1: Executables / Archives
	// Wine / Windows
	if runtime.GOOS != "windows" {
		if strings.HasSuffix(lower, ".exe") ||
			strings.HasSuffix(lower, ".msi") ||
			strings.Contains(lower, "windows") ||
			strings.Contains(lower, "win64") ||
			strings.Contains(lower, "win32") {
			return CategoryExecutable, "[Wine]"
		}
	}

	// Foreign Architecture
	foreignArchRegex := getForeignArchRegex(runtime.GOARCH)
	if foreignArchRegex != nil && foreignArchRegex.MatchString(name) {
		return CategoryExecutable, "[Foreign Arch]"
	}

	// Native
	return CategoryExecutable, "[Native]"
}

// AssetCategoryPriority returns a sort rank for ordering categorized items.
func AssetCategoryPriority(name string) int {
	_, tag := CategorizeAsset(name)
	switch tag {
	case "[Native]":
		return 1
	case "[Sidecar]":
		return 2
	case "[Wine]":
		return 3
	case "[Foreign Arch]":
		return 4
	case "[Checksum]":
		return 5
	default:
		return 6
	}
}

// FormatCategorizedItem sets DisplayName on an item based on its category tag.
func FormatCategorizedItem(item *SelectorItem) {
	_, tag := CategorizeAsset(item.Name)
	item.DisplayName = fmt.Sprintf("%-15s %s", tag, item.Name)
}
