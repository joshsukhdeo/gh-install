package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigManagement(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	xdg.Reload()

	t.Run("LoadConfig_MissingFileReturnsNil", func(t *testing.T) {
		cfg, err := LoadConfig()
		require.NoError(t, err)
		assert.Equal(t, &Config{}, cfg)
	})

	t.Run("LoadConfig_ValidYaml", func(t *testing.T) {
		configDir := filepath.Join(tmpDir, "gh-install")
		err := os.MkdirAll(configDir, 0755)
		require.NoError(t, err)

		yamlContent := []byte(`
install_types: deb,rpm
install_path: /custom/bin
global_path: /custom/global
clone_path: /custom/src
fork_path: /custom/projects
ai_cmd: "my-ai -p '%s'"
add_deps: true
prompt_rename: true
no_save_state: true
`)
		configPath := filepath.Join(configDir, "config.yml")
		err = os.WriteFile(configPath, yamlContent, 0644)
		require.NoError(t, err)

		cfg, err := LoadConfig()
		require.NoError(t, err)
		require.NotNil(t, cfg)

		assert.Equal(t, "deb,rpm", cfg.Core.InstallTypes)
		assert.Equal(t, "/custom/bin", cfg.Paths.InstallPath)
		assert.Equal(t, "/custom/global", cfg.Paths.GlobalPath)
		assert.Equal(t, "/custom/src", cfg.Paths.ClonePath)
		assert.Equal(t, "/custom/projects", cfg.Paths.ForkPath)
		assert.Equal(t, "my-ai -p '%s'", cfg.AI.AICmd)
		assert.True(t, cfg.Core.AddDeps)
		assert.False(t, cfg.Core.NoDeps)
		assert.True(t, cfg.Core.NoSaveState)
	})
}
