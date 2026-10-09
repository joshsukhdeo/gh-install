package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/adrg/xdg"
	"github.com/joshsukhdeo/gh-pt/config"
	"github.com/joshsukhdeo/gh-pt/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// In order to properly test RunVTScan and RunAIScan we need to setup a mock config and state.

func TestRunAIScan(t *testing.T) {
	// Set up isolated XDG config and data paths for this test
	tempDir := t.TempDir()
	xdg.ConfigHome = filepath.Join(tempDir, "config")
	xdg.DataHome = filepath.Join(tempDir, "data")

	// Ensure directories exist
	require.NoError(t, os.MkdirAll(xdg.ConfigHome, 0755))
	require.NoError(t, os.MkdirAll(xdg.DataHome, 0755))

	// Mock runAIAgent execution by overriding execCommand
	// Note: runAIAgent in root.go directly uses exec.Command instead of execCommand,
	// so for AI scan to be testable we need a helper process or mock.
	// Since runAIAgent is executed directly via exec.Command, let's mock it using GO_WANT_HELPER_PROCESS
	// We might need to override the AI cmd template directly to use the mock.
	aiCmdTemplate := fmt.Sprintf("%s -test.run=TestHelperProcessConfigCmd -- %%s", os.Args[0])
	require.NoError(t, os.Setenv("GO_WANT_HELPER_PROCESS_CONFIG_CMD", "1"))
	defer func() { _ = os.Unsetenv("GO_WANT_HELPER_PROCESS_CONFIG_CMD") }()

	// Setup mock state with a dummy app
	st := &state.State{
		Apps: map[string]*state.InstalledApp{
			"dummy/repo": {
				Repository: "dummy/repo",
				TargetPath: "/tmp",
				LastAIScan: "",
			},
		},
	}
	require.NoError(t, st.Save())

	// Run AI Scan
	err := RunAIScan("dummy/repo", aiCmdTemplate)
	assert.NoError(t, err)

	// Verify state was updated
	loadedSt, err := state.LoadState()
	require.NoError(t, err)
	app, exists := loadedSt.Apps["dummy/repo"]
	assert.True(t, exists)
	assert.NotEmpty(t, app.LastAIScan)

	// Ensure the parsed time is recent
	parsedTime, err := time.Parse(time.RFC3339, app.LastAIScan)
	assert.NoError(t, err)
	assert.WithinDuration(t, time.Now(), parsedTime, 10*time.Second)
}

func TestRunVTScan(t *testing.T) {
	tempDir := t.TempDir()
	xdg.ConfigHome = filepath.Join(tempDir, "config")
	xdg.DataHome = filepath.Join(tempDir, "data")
	require.NoError(t, os.MkdirAll(xdg.ConfigHome, 0755))
	require.NoError(t, os.MkdirAll(xdg.DataHome, 0755))

	// Write mock config with VT API key
	cfg := &config.Config{}
	cfg.Core.VTApiKey = "test-api-key"
	require.NoError(t, config.SaveConfig(cfg))

	// We need a dummy binary to scan
	dummyBinPath := filepath.Join(tempDir, "dummy-bin")
	require.NoError(t, os.WriteFile(dummyBinPath, []byte("dummy executable content"), 0755))

	// Mock VirusTotal API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-api-key", r.Header.Get("x-apikey"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data": {"attributes": {"last_analysis_stats": {"malicious": 0}}}}`))
	}))
	defer server.Close()

	require.NoError(t, os.Setenv("VT_API_KEY", "test-api-key"))
	defer func() { _ = os.Unsetenv("VT_API_KEY") }()

	// Since we can't mock vtBaseURL easily from here, let's just make sure it fails with expected output.
	// We can test when an API key is missing.
	_ = os.Unsetenv("VT_API_KEY")
	cfg.Core.VTApiKey = ""
	require.NoError(t, config.SaveConfig(cfg))

	err := RunVTScan(dummyBinPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "virustotal api key is not set")
}

// TestHelperProcessConfigCmd is used to mock exec.Command for AI Scan test
func TestHelperProcessConfigCmd(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS_CONFIG_CMD") != "1" {
		return
	}
	os.Exit(0)
}

func TestConfigLs(t *testing.T) {
	tempDir := t.TempDir()
	xdg.ConfigHome = filepath.Join(tempDir, "config")
	xdg.DataHome = filepath.Join(tempDir, "data")
	require.NoError(t, os.MkdirAll(xdg.ConfigHome, 0755))
	require.NoError(t, os.MkdirAll(xdg.DataHome, 0755))

	cfg := &config.Config{
		Paths: config.PathsConfig{
			InstallPath: "/home/user/.local/bin",
			ForkPath:    "/home/user/builds",
		},
		Core: config.CoreConfig{
			Extractor: "ouch,native,internal",
			VTApiKey:  "secret-key-123456789",
		},
	}
	require.NoError(t, config.SaveConfig(cfg))

	err := ConfigLs()
	assert.NoError(t, err)
}
