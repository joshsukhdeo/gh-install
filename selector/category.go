package selector

import (
	"fmt"
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

// CategorizeAsset classifies a release asset into Category and TUI Tag using the universal classifier.
func CategorizeAsset(name string) (AssetCategory, string) {
	c := DefaultClassifier()
	sub := c.DetectSubcategory(name)

	switch sub {
	case SubcatChecksum, SubcatSignature:
		return CategoryChecksum, "[Checksum]"
	case SubcatSidecar:
		return CategorySidecar, "[Sidecar]"
	case SubcatWine:
		return CategoryExecutable, "[Wine]"
	case SubcatUniversal:
		return CategoryExecutable, "[Universal]"
	case SubcatForeign, SubcatEmulated, SubcatUnsupported:
		return CategoryExecutable, "[Foreign Arch]"
	default:
		return CategoryExecutable, "[Native]"
	}
}

// AssetCategoryPriority returns a sort rank for ordering categorized items.
func AssetCategoryPriority(name string) int {
	_, tag := CategorizeAsset(name)
	switch tag {
	case "[Native]":
		return 1
	case "[Universal]":
		return 2
	case "[Sidecar]":
		return 3
	case "[Wine]":
		return 4
	case "[Foreign Arch]":
		return 5
	case "[Checksum]":
		return 6
	default:
		return 7
	}
}

// FormatCategorizedItem sets DisplayName on an item based on its category tag.
func FormatCategorizedItem(item *SelectorItem) {
	_, tag := CategorizeAsset(item.Name)
	item.DisplayName = fmt.Sprintf("%-15s %s", tag, item.Name)
}
