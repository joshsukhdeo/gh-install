package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
		configDir := filepath.Join(tmpDir, "gh-pt")
		err := os.MkdirAll(configDir, 0755)
		require.NoError(t, err)

		// yaml.v3 inline structs map to the same top-level root keys, so we don't nest them in the yaml
		yamlContent := []byte(`
install_types: deb,rpm
resolve_deps: true
disable_prompts: false
no_save_state: true
install_path: /custom/bin
global_path: /custom/global
clone_path: /custom/src
fork_path: /custom/projects
ai_cmd: "my-ai -p '%s'"
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
		assert.True(t, cfg.Core.ResolveDeps)
		assert.False(t, cfg.Core.NoDeps)
		assert.True(t, cfg.Core.NoSaveState)
	})

	t.Run("LoadConfig_InvalidYamlReturnsError", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "invalid"))
		xdg.Reload()
		err := os.MkdirAll(filepath.Join(tmpDir, "invalid", "gh-pt"), 0755)
		require.NoError(t, err)

		yamlContent := []byte(`
install_types: [invalid yaml
`)
		configPath := filepath.Join(tmpDir, "invalid", "gh-pt", "config.yml")
		err = os.WriteFile(configPath, yamlContent, 0644)
		require.NoError(t, err)

		cfg, err := LoadConfig()
		assert.Error(t, err)
		assert.Nil(t, cfg)
	})

	t.Run("LoadConfig_FileReadErrorReturnsError", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "readerror"))
		xdg.Reload()
		configDir := filepath.Join(tmpDir, "readerror", "gh-pt")
		err := os.MkdirAll(configDir, 0755)
		require.NoError(t, err)

		configPath := filepath.Join(configDir, "config.yml")
		// Create a directory instead of a file so os.ReadFile will fail
		err = os.MkdirAll(configPath, 0755)
		require.NoError(t, err)

		cfg, err := LoadConfig()
		assert.Error(t, err)
		assert.Nil(t, cfg)
	})

	t.Run("SaveConfig", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "save"))
		xdg.Reload()

		cfg := &Config{
			Core: CoreConfig{
				InstallTypes: "deb",
				VTApiKey:     "my-key",
			},
		}

		err := SaveConfig(cfg)
		require.NoError(t, err)

		loaded, err := LoadConfig()
		require.NoError(t, err)
		assert.Equal(t, "deb", loaded.Core.InstallTypes)
		assert.Equal(t, "my-key", loaded.Core.VTApiKey)
	})

	t.Run("SaveConfig_MkdirError", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "saveerror"))
		xdg.Reload()

		// Create file where directory should go
		err := os.MkdirAll(filepath.Join(tmpDir, "saveerror"), 0755)
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(tmpDir, "saveerror", "gh-pt"), []byte("file"), 0644)
		require.NoError(t, err)

		cfg := &Config{}
		err = SaveConfig(cfg)
		assert.Error(t, err)
	})

	t.Run("LoadConfig_ExceedsSizeLimit", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "oversized"))
		xdg.Reload()
		dir := filepath.Join(tmpDir, "oversized", "gh-pt")
		require.NoError(t, os.MkdirAll(dir, 0755))

		// Create a > 1MB dummy config
		hugeData := make([]byte, 1024*1024+50)
		for i := range hugeData {
			hugeData[i] = ' '
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"), hugeData, 0644))

		cfg, err := LoadConfig()
		assert.Error(t, err)
		assert.Nil(t, cfg)
		assert.Contains(t, err.Error(), "exceeds maximum size")
	})

	t.Run("LoadConfig_SensitiveSystemDirectoryBlocked", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "sensitive"))
		xdg.Reload()
		dir := filepath.Join(tmpDir, "sensitive", "gh-pt")
		require.NoError(t, os.MkdirAll(dir, 0755))

		yamlContent := []byte("install_path: /etc/cron.d\n")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"), yamlContent, 0644))

		cfg, err := LoadConfig()
		assert.Error(t, err)
		assert.Nil(t, cfg)
		assert.Contains(t, err.Error(), "cannot point to sensitive system directory")
	})

	t.Run("LoadConfig_ExcessiveYamlAliasesBlocked", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "bomb"))
		xdg.Reload()
		dir := filepath.Join(tmpDir, "bomb", "gh-pt")
		require.NoError(t, os.MkdirAll(dir, 0755))

		// Construct alias bomb
		var b strings.Builder
		b.WriteString("a: &id [1, 2, 3]\n")
		for i := 0; i < 40; i++ {
			b.WriteString(fmt.Sprintf("k%d: *id\n", i))
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"), []byte(b.String()), 0644))

		cfg, err := LoadConfig()
		assert.Error(t, err)
		assert.Nil(t, cfg)
		assert.Contains(t, err.Error(), "aliases/anchors")
	})

	t.Run("LoadConfig_UnknownFieldsRejected", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "unknown"))
		xdg.Reload()
		dir := filepath.Join(tmpDir, "unknown", "gh-pt")
		require.NoError(t, os.MkdirAll(dir, 0755))

		yamlContent := []byte("install_path: /custom/bin\nmalicious_unrecognized_field: evil_value\n")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"), yamlContent, 0644))

		cfg, err := LoadConfig()
		assert.Error(t, err)
		assert.Nil(t, cfg)
		assert.Contains(t, err.Error(), "invalid config format")
	})

	t.Run("LoadConfig_PopulatesChecksum", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "checksum"))
		xdg.Reload()
		dir := filepath.Join(tmpDir, "checksum", "gh-pt")
		require.NoError(t, os.MkdirAll(dir, 0755))

		yamlContent := []byte("install_path: /custom/bin\n")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"), yamlContent, 0644))

		cfg, err := LoadConfig()
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.NotEmpty(t, cfg.Checksum)
		assert.Len(t, cfg.Checksum, 64)
	})

	t.Run("LoadConfig_DeepSensitiveDirectoryBlocked", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "deepsensitive"))
		xdg.Reload()
		dir := filepath.Join(tmpDir, "deepsensitive", "gh-pt")
		require.NoError(t, os.MkdirAll(dir, 0755))

		yamlContent := []byte("package_path: /var/spool/cron/crontabs\n")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"), yamlContent, 0644))

		cfg, err := LoadConfig()
		assert.Error(t, err)
		assert.Nil(t, cfg)
		assert.Contains(t, err.Error(), "cannot point to sensitive system directory")
	})

	t.Run("LoadConfig_ValidExtractorCombinations", func(t *testing.T) {
		validCases := []string{
			"default",
			"ouch",
			"native",
			"internal",
			"ouch,native,internal",
			"native,internal",
			"internal,native",
		}
		for _, tc := range validCases {
			cfg := &Config{
				Core: CoreConfig{
					Extractor: tc,
				},
			}
			err := ValidateConfig(cfg)
			assert.NoError(t, err, "expected extractor %q to be valid", tc)
		}
	})

	t.Run("LoadConfig_InvalidExtractorRejected", func(t *testing.T) {
		invalidCases := []string{
			"bogus",
			"ouch,bogus",
			"ouch,native,zip",
		}
		for _, tc := range invalidCases {
			cfg := &Config{
				Core: CoreConfig{
					Extractor: tc,
				},
			}
			err := ValidateConfig(cfg)
			assert.Error(t, err, "expected extractor %q to be invalid", tc)
			assert.Contains(t, err.Error(), "config extractor")
		}
	})
}
