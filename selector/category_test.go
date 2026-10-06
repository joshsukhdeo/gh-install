package selector

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCategorizeAsset(t *testing.T) {
	tests := []struct {
		name             string
		expectedCategory AssetCategory
		expectedTag      string
	}{
		{"checksums.txt", CategoryChecksum, "[Checksum]"},
		{"SHA256SUMS", CategoryChecksum, "[Checksum]"},
		{"app-1.0.0.sha256", CategoryChecksum, "[Checksum]"},
		{"release.sig", CategoryChecksum, "[Checksum]"},
		{"signing-key.asc", CategoryChecksum, "[Checksum]"},

		{"libplugin.so", CategorySidecar, "[Sidecar]"},
		{"libsqlite3.so.0", CategorySidecar, "[Sidecar]"},
		{"assets.pak", CategorySidecar, "[Sidecar]"},
		{"library.dylib", CategorySidecar, "[Sidecar]"},
		{"module.dll", CategorySidecar, "[Sidecar]"},
		{"bindings.h", CategorySidecar, "[Sidecar]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cat, tag := CategorizeAsset(tt.name)
			assert.Equal(t, tt.expectedCategory, cat)
			assert.Equal(t, tt.expectedTag, tag)
		})
	}

	if runtime.GOOS != "windows" {
		t.Run("WineDetection", func(t *testing.T) {
			cat, tag := CategorizeAsset("tool_setup.exe")
			assert.Equal(t, CategoryExecutable, cat)
			assert.Equal(t, "[Wine]", tag)

			cat, tag = CategorizeAsset("installer.msi")
			assert.Equal(t, CategoryExecutable, cat)
			assert.Equal(t, "[Wine]", tag)

			cat, tag = CategorizeAsset("tool-windows-x64.zip")
			assert.Equal(t, CategoryExecutable, cat)
			assert.Equal(t, "[Wine]", tag)
		})
	}

	if runtime.GOARCH == "amd64" {
		t.Run("ForeignArchDetection_amd64Host", func(t *testing.T) {
			cat, tag := CategorizeAsset("tool-linux-arm64.tar.gz")
			assert.Equal(t, CategoryExecutable, cat)
			assert.Equal(t, "[Foreign Arch]", tag)

			cat, tag = CategorizeAsset("tool-linux-386.tar.gz")
			assert.Equal(t, CategoryExecutable, cat)
			assert.Equal(t, "[Foreign Arch]", tag)

			cat, tag = CategorizeAsset("tool-linux-amd64.tar.gz")
			assert.Equal(t, CategoryExecutable, cat)
			assert.Equal(t, "[Native]", tag)
		})
	}
}

func TestAssetCategoryPriority(t *testing.T) {
	assert.True(t, AssetCategoryPriority("tool-linux-amd64.tar.gz") < AssetCategoryPriority("libfoo.so"))
	assert.True(t, AssetCategoryPriority("libfoo.so") < AssetCategoryPriority("tool-windows.exe"))
	assert.True(t, AssetCategoryPriority("tool-windows.exe") < AssetCategoryPriority("tool-linux-arm64.tar.gz"))
	assert.True(t, AssetCategoryPriority("tool-linux-arm64.tar.gz") < AssetCategoryPriority("checksums.txt"))
}

func TestInteractiveSelector_DisplayNameMapping(t *testing.T) {
	item1 := &SelectorItem{Name: "app-linux-amd64.tar.gz"}
	item2 := &SelectorItem{Name: "checksums.txt"}
	FormatCategorizedItem(item1)
	FormatCategorizedItem(item2)

	assert.Contains(t, item1.DisplayName, "[Native]")
	assert.Contains(t, item2.DisplayName, "[Checksum]")

	// Test Single select with DisplayName
	s := &InteractiveSelector{
		Kind:   Asset,
		Items:  []*SelectorItem{item1, item2},
		Single: true,
		Prompter: MockPrompter{
			SelectRet: item1.DisplayName,
		},
	}
	res, err := s.Run()
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, "app-linux-amd64.tar.gz", res[0].Name)

	// Test MultiSelect with DisplayName
	multiSel := &InteractiveSelector{
		Kind:   Asset,
		Items:  []*SelectorItem{item1, item2},
		Single: false,
		Prompter: MockPrompter{
			MultiSelectRet: []string{item1.DisplayName, item2.DisplayName},
		},
	}
	multiRes, err := multiSel.Run()
	require.NoError(t, err)
	require.Len(t, multiRes, 2)
	assert.Equal(t, "app-linux-amd64.tar.gz", multiRes[0].Name)
	assert.Equal(t, "checksums.txt", multiRes[1].Name)
}
