package release

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsAllowedBinaryDir(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"mybin", true},
		{"bin/mybin", true},
		{"bin32/mybin", true},
		{"bin64/mybin", true},
		{"app-1.0.0/mybin", true},
		{"app-1.0.0/bin/mybin", true},
		{"app-1.0.0/bin32/mybin", true},
		{"app-1.0.0/bin64/mybin", true},
		{"app-1.0.0/tests/fixtures/fakebin", false},
		{"internal/vendor/tool", false},
		{"deeply/nested/dir/bin/mybin", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsAllowedBinaryDir(tt.path))
		})
	}
}

func TestSetupBinaryCompletions_NoSetupFlag(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	err := SetupBinaryCompletions("/bin/sh", true)
	require.NoError(t, err)

	// Should not have created completions directory
	_, statErr := os.Stat(filepath.Join(tmpDir, "gh-pt", "completions"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestScanAndInstallArchiveCompletions(t *testing.T) {
	tmpData := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpData)

	archiveDir := t.TempDir()
	bashComp := filepath.Join(archiveDir, "complete.bash")
	zshComp := filepath.Join(archiveDir, "_mytool")
	require.NoError(t, os.WriteFile(bashComp, []byte("complete -F _mytool mytool\n"), 0644))
	require.NoError(t, os.WriteFile(zshComp, []byte("#compdef mytool\n"), 0644))

	installed := ScanAndInstallArchiveCompletions(archiveDir, "mytool", false)
	assert.True(t, installed)

	// Verify completions installed in $XDG_DATA_HOME/gh-pt/completions/
	bashInstalled := filepath.Join(tmpData, "gh-pt", "completions", "bash", "mytool")
	zshInstalled := filepath.Join(tmpData, "gh-pt", "completions", "zsh", "_mytool")
	assert.FileExists(t, bashInstalled)
	assert.FileExists(t, zshInstalled)

	// Verify master loaders created beside state.json
	bashLoader := filepath.Join(tmpData, "gh-pt", "completions.bash")
	zshLoader := filepath.Join(tmpData, "gh-pt", "completions.zsh")
	assert.FileExists(t, bashLoader)
	assert.FileExists(t, zshLoader)

	content, err := os.ReadFile(zshLoader)
	require.NoError(t, err)
	assert.Contains(t, string(content), "*(N)")
}

func TestScanAndInstallArchiveCompletions_NoSetup(t *testing.T) {
	tmpData := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpData)

	archiveDir := t.TempDir()
	bashComp := filepath.Join(archiveDir, "complete.bash")
	require.NoError(t, os.WriteFile(bashComp, []byte("complete -F _mytool mytool\n"), 0644))

	installed := ScanAndInstallArchiveCompletions(archiveDir, "mytool", true)
	assert.False(t, installed)

	// Verify no completions directory created
	_, statErr := os.Stat(filepath.Join(tmpData, "gh-pt", "completions"))
	assert.True(t, os.IsNotExist(statErr))
}
