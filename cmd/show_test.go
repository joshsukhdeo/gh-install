package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrg/xdg"
	"github.com/joshsukhdeo/gh-pt/params"
	"github.com/joshsukhdeo/gh-pt/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockGhClient struct {
	releases []Release
	assets   map[int64][]ReleaseAsset
	tags     map[string]Release
	err      error
}

func (m *mockGhClient) Get(path string, response interface{}) error {
	if m.err != nil {
		return m.err
	}

	if strings.HasSuffix(path, "/releases") {
		data, err := json.Marshal(m.releases)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, response)
	}

	if strings.Contains(path, "/assets") {
		var id int64
		_, err := fmt.Sscanf(path[strings.Index(path, "/releases/")+len("/releases/"):strings.Index(path, "/assets")], "%d", &id)
		if err == nil {
			if a, ok := m.assets[id]; ok {
				data, err := json.Marshal(a)
				if err != nil {
					return err
				}
				return json.Unmarshal(data, response)
			}
		}
		return nil
	}

	if strings.Contains(path, "/releases/tags/") {
		tag := path[strings.LastIndex(path, "/")+1:]
		if rel, ok := m.tags[tag]; ok {
			data, err := json.Marshal(rel)
			if err != nil {
				return err
			}
			return json.Unmarshal(data, response)
		}
		return fmt.Errorf("tag not found")
	}

	return nil
}

func captureOutput(f func()) string {
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	_ = w.Close()
	os.Stdout = orig

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func TestShowInfo_ShowVersions(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	xdg.ConfigHome = tmpDir
	defer func() { xdg.ConfigHome = "" }()
	xdg.ConfigHome = tmpDir
	defer func() { xdg.ConfigHome = "" }()
	xdg.Reload()

	st, err := state.LoadState()
	require.NoError(t, err)
	require.NoError(t, st.AddApp(&state.InstalledApp{
		Repository: "test/repo",
		Version:    "v1.1.0",
		Pinned:     true,
	}))

	mock := &mockGhClient{
		releases: []Release{
			{ID: 103, TagName: "v1.2.0-rc1", Prerelease: true},
			{ID: 102, TagName: "v1.1.0", Prerelease: false},
			{ID: 101, TagName: "v1.0.0", Prerelease: false},
		},
	}

	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository:   "test/repo",
			ShowVersions: 10,
		},
	}

	out := captureOutput(func() {
		err := showInfoWithClient(r, mock)
		assert.NoError(t, err)
	})

	// v1.2.0-rc1 is latest prerelease (🗻🧪)
	// v1.1.0 is latest stable, installed, and pinned (📌🗻🎯)
	// v1.0.0 is older uninstalled stable ("")
	assert.Contains(t, out, "🗻🧪 v1.2.0-rc1")
	assert.Contains(t, out, "📌🗻🎯 v1.1.0")
	assert.Contains(t, out, "v1.0.0")
}

func TestShowInfo_ShowVersions_DisableIcons(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	xdg.Reload()

	cfgPath := filepath.Join(tmpDir, "gh-pt", "config.yml")
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0755)
	_ = os.WriteFile(cfgPath, []byte("disable_icons: true\n"), 0644)

	st, err := state.LoadState()
	require.NoError(t, err)
	require.NoError(t, st.AddApp(&state.InstalledApp{
		Repository: "test/repo",
		Version:    "v1.1.0",
		Pinned:     true,
	}))

	mock := &mockGhClient{
		releases: []Release{
			{ID: 103, TagName: "v1.2.0-rc1", Prerelease: true},
			{ID: 102, TagName: "v1.1.0", Prerelease: false},
		},
	}

	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository:   "test/repo",
			ShowVersions: 10,
		},
	}

	out := captureOutput(func() {
		err := showInfoWithClient(r, mock)
		assert.NoError(t, err)
	})

	// v1.2.0-rc1 with text fallback (*!)
	// v1.1.0 with text fallback (^*@)
	assert.Contains(t, out, "*! v1.2.0-rc1")
	assert.Contains(t, out, "^*@ v1.1.0")
	assert.Contains(t, out, "--- VERSIONS ---")
}

func TestShowInfo_Show(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	xdg.ConfigHome = tmpDir
	defer func() { xdg.ConfigHome = "" }()
	xdg.ConfigHome = tmpDir
	defer func() { xdg.ConfigHome = "" }()
	xdg.Reload()

	var releases []Release
	for i := 15; i >= 1; i-- {
		releases = append(releases, Release{
			ID:         int64(100 + i),
			TagName:    fmt.Sprintf("v1.%d.0", i),
			Prerelease: i == 15,
		})
	}

	mock := &mockGhClient{
		releases: releases,
		assets: map[int64][]ReleaseAsset{
			114: { // v1.14.0 is latest stable
				{ID: 1, Name: "app-linux-amd64.tar.gz"},
				{ID: 2, Name: "app-darwin-arm64.tar.gz"},
			},
			115: { // v1.15.0 is latest prerelease
				{ID: 3, Name: "app-prerelease.tar.gz"},
			},
		},
	}

	// Default (stable): should pick v1.14.0 and display max 10 versions, then v1.14.0 assets
	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository: "test/repo",
			Show:       true, ShowVersions: -1, ShowAssets: -1, ShowDescription: -1, ShowReadme: -1,
			CommonInstallFlags: params.CommonInstallFlags{
				ReleaseVersion: "latest",
			},
		},
	}

	out := captureOutput(func() {
		err := showInfoWithClient(r, mock)
		assert.NoError(t, err)
	})

	// 10 versions + 2 assets = 12 lines

	assert.Contains(t, out, "v1.15.0")
	assert.Contains(t, out, "v1.6.0")
	assert.NotContains(t, out, "v1.5.0") // truncated after 10 versions
	assert.Contains(t, out, "app-linux-amd64.tar.gz")
	assert.Contains(t, out, "app-darwin-arm64.tar.gz")

	// With Prerelease: should pick v1.15.0
	rPrerelease := &RootCLI{
		ExecContext: params.ExecContext{
			Repository: "test/repo",
			Show:       true, ShowVersions: -1, ShowAssets: -1, ShowDescription: -1, ShowReadme: -1,
			CommonInstallFlags: params.CommonInstallFlags{
				Prerelease:     true,
				ReleaseVersion: "latest",
			},
		},
	}
	outPre := captureOutput(func() {
		err := showInfoWithClient(rPrerelease, mock)
		assert.NoError(t, err)
	})
	assert.Contains(t, outPre, "app-prerelease.tar.gz")
}

func TestShowInfo_ShowAssets(t *testing.T) {
	mock := &mockGhClient{
		releases: []Release{
			{ID: 102, TagName: "v2.0.0", Prerelease: false},
			{ID: 101, TagName: "v1.0.0", Prerelease: false},
		},
		assets: map[int64][]ReleaseAsset{
			102: {
				{ID: 1, Name: "asset-2.0-a"},
				{ID: 2, Name: "asset-2.0-b"},
			},
			101: {
				{ID: 3, Name: "asset-1.0-a"},
			},
		},
	}

	// Target specific version v1.0.0
	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository: "test/repo",
			ShowAssets: 50,
			CommonInstallFlags: params.CommonInstallFlags{
				ReleaseVersion: "v1.0.0",
			},
		},
	}

	out := captureOutput(func() {
		err := showInfoWithClient(r, mock)
		assert.NoError(t, err)
	})

	assert.Contains(t, out, "asset-1.0-a")
	assert.NotContains(t, out, "asset-2.0-a")
}

func TestShowInfo_Errors(t *testing.T) {
	// Empty repository
	r := &RootCLI{
		ExecContext: params.ExecContext{
			Show: true, ShowVersions: -1, ShowAssets: -1, ShowDescription: -1, ShowReadme: -1,
		},
	}
	err := showInfoWithClient(r, &mockGhClient{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "repository must be provided")

	// Releases fetch failure
	mockErr := &mockGhClient{err: fmt.Errorf("api network error")}
	r.Repository = "test/repo"
	err = showInfoWithClient(r, mockErr)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch releases")

	// Target version not found
	mock := &mockGhClient{
		releases: []Release{
			{ID: 1, TagName: "v1.0.0"},
		},
	}
	r.ReleaseVersion = "v9.9.9"
	err = showInfoWithClient(r, mock)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no release found")
}

func TestShowInfo_RoutingInRun(t *testing.T) {
	origClient := defaultRestClient
	defer func() { defaultRestClient = origClient }()

	mock := &mockGhClient{
		releases: []Release{
			{ID: 1, TagName: "v1.0.0"},
		},
		assets: map[int64][]ReleaseAsset{
			1: {{ID: 1, Name: "my-asset.deb"}},
		},
	}
	defaultRestClient = func() (ghRestClient, error) {
		return mock, nil
	}

	cli := &params.CLI{
		Show: params.ShowCmd{
			Repository: "test/repo",
			Assets:     50,
		},
	}

	// Ensure RunCommand routes to ShowInfo and returns without error
	out := captureOutput(func() {
		err := RunCommand("show", cli)
		assert.NoError(t, err)
	})
	assert.Contains(t, out, "my-asset.deb")
}

func TestShowInfo_HeadersWithIcons(t *testing.T) {
	origClient := defaultRestClient
	defer func() { defaultRestClient = origClient }()

	mock := &mockGhClient{
		releases: []Release{{ID: 1, TagName: "v1.0.0"}},
		assets:   map[int64][]ReleaseAsset{1: {{ID: 1, Name: "asset.deb"}}},
	}
	defaultRestClient = func() (ghRestClient, error) {
		return mock, nil
	}

	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository: "test/repo",
			Show:       true, ShowVersions: 1, ShowAssets: 1, ShowDescription: -1, ShowReadme: -1,
		},
	}

	out := captureOutput(func() {
		err := showInfoWithClient(r, mock)
		assert.NoError(t, err)
	})

	assert.Contains(t, out, "--- 📦 VERSIONS 📦 ---")
	assert.Contains(t, out, "--- 📂 ASSETS 📂 ---")

	// Now with DisableIcons = true
	// We need to write a config file to set disableIcons, or we can just mock loadConfig?
	// loadConfig() reads from XDG_CONFIG_HOME
	tmpDir := t.TempDir()
	xdg.ConfigHome = tmpDir
	defer func() { xdg.ConfigHome = "" }()
	cfgPath := filepath.Join(tmpDir, "gh-pt", "config.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0755))
	require.NoError(t, os.WriteFile(cfgPath, []byte("disable_icons: true\n"), 0644))

	outDisabled := captureOutput(func() {
		err := showInfoWithClient(r, mock)
		assert.NoError(t, err)
	})

	assert.NotContains(t, outDisabled, "📦")
	assert.NotContains(t, outDisabled, "📂")
	assert.Contains(t, outDisabled, "--- VERSIONS ---")
	assert.Contains(t, outDisabled, "--- ASSETS ---")
}

func TestCliParams_ShowStruct(t *testing.T) {
	cli := &params.CliParams{
		Show: params.Show{
			Repository:       "owner/repo",
			Assets:           10,
			Versions:         5,
			Description:      3,
			Readme:           20,
			Prerelease:       true,
			Stable:           false,
			Version:          "v1.0.0",
			DiscoverSidecars: true,
		},
	}
	assert.Equal(t, "owner/repo", cli.Show.Repository)
	assert.Equal(t, 10, cli.Show.Assets)
	assert.Equal(t, 5, cli.Show.Versions)
	assert.Equal(t, "v1.0.0", cli.Show.Version)
	assert.True(t, cli.Show.DiscoverSidecars)

	cmd := cli.Show
	assert.Equal(t, "owner/repo", cmd.Repository)
}

type mockShowTreeClient struct {
	defaultBranch string
	treeItems     []gitTreeItem
	repoErr       error
	treeErr       error
	requestedPath string
}

func (m *mockShowTreeClient) Get(path string, response interface{}) error {
	trimmed := strings.TrimPrefix(path, "/")
	if strings.Contains(trimmed, "git/trees/") {
		m.requestedPath = path
		if m.treeErr != nil {
			return m.treeErr
		}
		data, err := json.Marshal(gitTreeResponse{
			SHA:  "mock-sha-tree",
			Tree: m.treeItems,
		})
		if err != nil {
			return err
		}
		return json.Unmarshal(data, response)
	}

	if strings.HasPrefix(trimmed, "repos/") {
		m.requestedPath = path
		if m.repoErr != nil {
			return m.repoErr
		}
		data, err := json.Marshal(map[string]interface{}{
			"default_branch": m.defaultBranch,
		})
		if err != nil {
			return err
		}
		return json.Unmarshal(data, response)
	}

	return fmt.Errorf("unhandled mock path: %s", path)
}

func TestHandleShow_Success(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	xdg.DataHome = tmpDir

	mockClient := &mockShowTreeClient{
		defaultBranch: "main",
		treeItems: []gitTreeItem{
			{Path: "plugins/core.red", Type: "blob", Size: 100},
			{Path: "config/app.json", Type: "blob", Size: 50},
			{Path: "README.md", Type: "blob", Size: 200},
			{Path: "plugins", Type: "tree"},
			{Path: "submodule", Type: "commit"},
		},
	}

	origClient := defaultRestClient
	origMultiselect := multiselectFiles
	origDownload := downloadRawFile
	defer func() {
		defaultRestClient = origClient
		multiselectFiles = origMultiselect
		downloadRawFile = origDownload
	}()

	defaultRestClient = func() (ghRestClient, error) {
		return mockClient, nil
	}

	var presentedOptions []string
	multiselectFiles = func(r *RootCLI, prompt string, options []string) ([]string, error) {
		presentedOptions = options
		// Select the plugin and config files
		return []string{"plugins/core.red", "config/app.json"}, nil
	}

	downloadedURLs := make(map[string]bool)
	downloadRawFile = func(url string) ([]byte, error) {
		downloadedURLs[url] = true
		if strings.HasSuffix(url, "plugins/core.red") {
			return []byte("plugin-binary-content"), nil
		}
		if strings.HasSuffix(url, "config/app.json") {
			return []byte(`{"enabled": true}`), nil
		}
		return nil, fmt.Errorf("unexpected URL: %s", url)
	}

	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository: "my-org/cool-tool",
		},
		CliParams: &params.ExecContext{
			Repository: "my-org/cool-tool",
			Sidecars:   `plugins/.*|config/.*`, // Regex pattern for sidecars
		},
	}

	err := r.handleShow()
	require.NoError(t, err)

	// Verify only blob files were presented
	assert.Equal(t, []string{"README.md", "config/app.json", "plugins/core.red"}, presentedOptions)

	// Verify URLs requested for download
	assert.True(t, downloadedURLs["https://raw.githubusercontent.com/my-org/cool-tool/main/plugins/core.red"])
	assert.True(t, downloadedURLs["https://raw.githubusercontent.com/my-org/cool-tool/main/config/app.json"])

	// Verify files written to SidecarTargetPath
	targetDir := filepath.Join(tmpDir, "gh-pt", "sidecars", "my-org", "cool-tool")
	pluginPath := filepath.Join(targetDir, "plugins", "core.red")
	configPath := filepath.Join(targetDir, "config", "app.json")

	pluginBytes, err := os.ReadFile(pluginPath)
	require.NoError(t, err)
	assert.Equal(t, "plugin-binary-content", string(pluginBytes))

	configBytes, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, `{"enabled": true}`, string(configBytes))

	// Verify state.json updated
	st, err := state.LoadState()
	require.NoError(t, err)
	app, exists := st.Apps["my-org/cool-tool"]
	require.True(t, exists)
	// SidecarTargetPath removed - sidecars now use IncludeSidecars mode
	assert.Contains(t, app.InstalledSidecars, pluginPath)
	assert.Contains(t, app.InstalledSidecars, configPath)
	// Sidecars is now a regex pattern string, not a slice
	// The test should verify the regex pattern is stored
	assert.NotEmpty(t, app.Sidecars)

	// Verify r.InstalledSidecars
	assert.Contains(t, r.InstalledSidecars, pluginPath)
	assert.Contains(t, r.InstalledSidecars, configPath)
}

func TestHandleShow_CustomReleaseVersion(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	xdg.DataHome = tmpDir

	mockClient := &mockShowTreeClient{
		defaultBranch: "main",
		treeItems: []gitTreeItem{
			{Path: "plugin.red", Type: "blob"},
		},
	}

	origClient := defaultRestClient
	origMultiselect := multiselectFiles
	origDownload := downloadRawFile
	defer func() {
		defaultRestClient = origClient
		multiselectFiles = origMultiselect
		downloadRawFile = origDownload
	}()

	defaultRestClient = func() (ghRestClient, error) {
		return mockClient, nil
	}

	multiselectFiles = func(r *RootCLI, prompt string, options []string) ([]string, error) {
		return []string{"plugin.red"}, nil
	}

	var downloadedURL string
	downloadRawFile = func(url string) ([]byte, error) {
		downloadedURL = url
		return []byte("data"), nil
	}

	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository: "owner/repo",
			CommonInstallFlags: params.CommonInstallFlags{
				ReleaseVersion: "v2.5.0",
			},
		},
	}

	err := r.handleShow()
	require.NoError(t, err)

	assert.Contains(t, mockClient.requestedPath, "git/trees/v2.5.0")
	assert.Equal(t, "https://raw.githubusercontent.com/owner/repo/v2.5.0/plugin.red", downloadedURL)
}

func TestHandleShow_NoBlobsFound(t *testing.T) {
	mockClient := &mockShowTreeClient{
		defaultBranch: "main",
		treeItems: []gitTreeItem{
			{Path: "scripts", Type: "tree"},
			{Path: "submodule", Type: "commit"},
		},
	}

	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository: "owner/repo",
		},
	}

	err := r.handleShowWithClient(mockClient)
	assert.NoError(t, err)
	assert.Empty(t, r.InstalledSidecars)
}

func TestHandleShow_UserSelectsNothing(t *testing.T) {
	mockClient := &mockShowTreeClient{
		defaultBranch: "main",
		treeItems: []gitTreeItem{
			{Path: "plugin.red", Type: "blob"},
		},
	}

	origMultiselect := multiselectFiles
	defer func() { multiselectFiles = origMultiselect }()
	multiselectFiles = func(r *RootCLI, prompt string, options []string) ([]string, error) {
		return nil, nil
	}

	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository: "owner/repo",
		},
	}

	err := r.handleShowWithClient(mockClient)
	assert.NoError(t, err)
	assert.Empty(t, r.InstalledSidecars)
}

func TestHandleShow_Errors(t *testing.T) {
	// Empty repository
	rEmpty := &RootCLI{}
	err := rEmpty.handleShow()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "repository must be provided")

	// Invalid repository format
	rInvalid := &RootCLI{ExecContext: params.ExecContext{Repository: "noslash"}}
	err = rInvalid.handleShow()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "repository must be in 'owner/repo' format")

	// API error fetching repository info
	mockRepoErr := &mockShowTreeClient{repoErr: fmt.Errorf("API rate limit exceeded")}
	r := &RootCLI{ExecContext: params.ExecContext{Repository: "owner/repo"}}
	err = r.handleShowWithClient(mockRepoErr)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch repository info")

	// API error fetching git tree
	mockTreeErr := &mockShowTreeClient{defaultBranch: "main", treeErr: fmt.Errorf("Tree not found")}
	err = r.handleShowWithClient(mockTreeErr)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch git tree")

	// Multiselect error
	mockClient := &mockShowTreeClient{
		defaultBranch: "main",
		treeItems:     []gitTreeItem{{Path: "file.txt", Type: "blob"}},
	}
	origMultiselect := multiselectFiles
	defer func() { multiselectFiles = origMultiselect }()
	multiselectFiles = func(r *RootCLI, prompt string, options []string) ([]string, error) {
		return nil, fmt.Errorf("terminal error")
	}
	err = r.handleShowWithClient(mockClient)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "selection failed")

	// Download error
	multiselectFiles = func(r *RootCLI, prompt string, options []string) ([]string, error) {
		return []string{"file.txt"}, nil
	}
	origDownload := downloadRawFile
	defer func() { downloadRawFile = origDownload }()
	downloadRawFile = func(url string) ([]byte, error) {
		return nil, fmt.Errorf("network connection refused")
	}
	err = r.handleShowWithClient(mockClient)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to download")
}

func TestShowInfo_NoFlags_DoesNotDelegateToHandleShow(t *testing.T) {
	mockClient := &mockGhClient{
		releases: []Release{{ID: 1, TagName: "v1.0.0", Prerelease: false}},
	}

	origClient := defaultRestClient
	origMultiselect := multiselectFiles
	defer func() {
		defaultRestClient = origClient
		multiselectFiles = origMultiselect
	}()

	defaultRestClient = func() (ghRestClient, error) {
		return mockClient, nil
	}

	var called bool
	multiselectFiles = func(r *RootCLI, prompt string, options []string) ([]string, error) {
		called = true
		return nil, nil
	}

	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository:      "owner/repo",
			ShowAssets:      -1,
			ShowVersions:    -1,
			ShowDescription: -1,
			ShowReadme:      -1,
		},
	}

	err := ShowInfo(r)
	assert.NoError(t, err)
	assert.False(t, called, "file browser multiselect must not be invoked")
}

// TestShowInfo_NoFlags_ShowsReleaseInfo: gh-pt show <repo> with no flags must show release info,
// NOT the interactive file browser.
func TestShowInfo_NoFlags_ShowsReleaseInfo(t *testing.T) {
	origMultiselect := multiselectFiles
	defer func() { multiselectFiles = origMultiselect }()

	fileBrowserCalled := false
	multiselectFiles = func(r *RootCLI, prompt string, options []string) ([]string, error) {
		fileBrowserCalled = true
		return nil, nil
	}

	mock := &mockGhClient{
		releases: []Release{{ID: 1, TagName: "v1.0.0", Prerelease: false}},
		assets:   map[int64][]ReleaseAsset{1: {{ID: 1, Name: "app-linux.tar.gz"}}},
	}

	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository:      "owner/repo",
			ShowAssets:      -1,
			ShowVersions:    -1,
			ShowDescription: -1,
			ShowReadme:      -1,
		},
	}

	out := captureOutput(func() {
		err := showInfoWithClient(r, mock)
		assert.NoError(t, err)
	})

	assert.False(t, fileBrowserCalled, "file browser must NOT be shown")
	assert.Contains(t, out, "v1.0.0")
	assert.Contains(t, out, "app-linux.tar.gz")
}

// TestShowInfo_VersionFlag_SelectsSpecificRelease: --version v1.3.1 must fetch assets from that tag.
func TestShowInfo_VersionFlag_SelectsSpecificRelease(t *testing.T) {
	mock := &mockGhClient{
		releases: []Release{
			{ID: 2, TagName: "v1.3.2", Prerelease: false},
			{ID: 1, TagName: "v1.3.1", Prerelease: false},
		},
		assets: map[int64][]ReleaseAsset{
			2: {{ID: 10, Name: "app-v1.3.2-linux.tar.gz"}},
			1: {{ID: 11, Name: "app-v1.3.1-linux.tar.gz"}},
		},
	}

	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository: "owner/repo",
			ShowAssets: 50,
			CommonInstallFlags: params.CommonInstallFlags{
				ReleaseVersion: "v1.3.1",
			},
		},
	}

	out := captureOutput(func() {
		err := showInfoWithClient(r, mock)
		assert.NoError(t, err)
	})

	assert.Contains(t, out, "app-v1.3.1-linux.tar.gz")
	assert.NotContains(t, out, "app-v1.3.2-linux.tar.gz")
}

// TestShowInfo_Default_BothReleaseTypes: no --stable/--prerelease -> versions list has both,
// assets come from latest STABLE.
func TestShowInfo_Default_BothReleaseTypes(t *testing.T) {
	mock := &mockGhClient{
		releases: []Release{
			{ID: 2, TagName: "v2.0.0-rc1", Prerelease: true},
			{ID: 1, TagName: "v1.0.0", Prerelease: false},
		},
		assets: map[int64][]ReleaseAsset{
			2: {{ID: 20, Name: "prerelease-asset.tar.gz"}},
			1: {{ID: 10, Name: "stable-asset.tar.gz"}},
		},
	}

	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository:   "owner/repo",
			ShowVersions: 20,
			ShowAssets:   50,
		},
	}

	out := captureOutput(func() {
		err := showInfoWithClient(r, mock)
		assert.NoError(t, err)
	})

	assert.Contains(t, out, "v2.0.0-rc1", "prerelease must appear in versions list")
	assert.Contains(t, out, "v1.0.0", "stable must appear in versions list")
	assert.Contains(t, out, "stable-asset.tar.gz", "assets must be from latest stable")
	assert.NotContains(t, out, "prerelease-asset.tar.gz")
}

// TestShowInfo_StableFlag_OverridesConfigPrerelease: --stable shows stable assets even
// when config has allow_prerelease: true.
func TestShowInfo_StableFlag_OverridesConfigPrerelease(t *testing.T) {
	tmpDir := t.TempDir()
	xdg.ConfigHome = tmpDir
	defer func() { xdg.ConfigHome = "" }()
	cfgPath := filepath.Join(tmpDir, "gh-pt", "config.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0755))
	require.NoError(t, os.WriteFile(cfgPath, []byte("allow_prerelease: true\n"), 0644))

	mock := &mockGhClient{
		releases: []Release{
			{ID: 2, TagName: "v2.0.0-rc1", Prerelease: true},
			{ID: 1, TagName: "v1.0.0", Prerelease: false},
		},
		assets: map[int64][]ReleaseAsset{
			2: {{ID: 20, Name: "prerelease-asset.tar.gz"}},
			1: {{ID: 10, Name: "stable-asset.tar.gz"}},
		},
	}

	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository:         "owner/repo",
			ShowAssets:         50,
			CommonInstallFlags: params.CommonInstallFlags{Stable: true},
		},
	}

	out := captureOutput(func() {
		err := showInfoWithClient(r, mock)
		assert.NoError(t, err)
	})

	assert.Contains(t, out, "stable-asset.tar.gz")
	assert.NotContains(t, out, "prerelease-asset.tar.gz")
}

// TestShowInfo_PrereleaseFlag_AssetsFromLatestPrerelease: --prerelease returns prerelease assets.
func TestShowInfo_PrereleaseFlag_AssetsFromLatestPrerelease(t *testing.T) {
	mock := &mockGhClient{
		releases: []Release{
			{ID: 2, TagName: "v2.0.0-rc1", Prerelease: true},
			{ID: 1, TagName: "v1.0.0", Prerelease: false},
		},
		assets: map[int64][]ReleaseAsset{
			2: {{ID: 20, Name: "prerelease-asset.tar.gz"}},
			1: {{ID: 10, Name: "stable-asset.tar.gz"}},
		},
	}

	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository:         "owner/repo",
			ShowAssets:         50,
			CommonInstallFlags: params.CommonInstallFlags{Prerelease: true},
		},
	}

	out := captureOutput(func() {
		err := showInfoWithClient(r, mock)
		assert.NoError(t, err)
	})

	assert.Contains(t, out, "prerelease-asset.tar.gz")
	assert.NotContains(t, out, "stable-asset.tar.gz")
}

// mockReadmeClient serves releases + readme content for readme tests.
type mockReadmeClient struct {
	releases      []Release
	readmeContent string
}

func (m *mockReadmeClient) Get(path string, response interface{}) error {
	if strings.HasSuffix(path, "/releases") {
		data, _ := json.Marshal(m.releases)
		return json.Unmarshal(data, response)
	}
	if strings.HasSuffix(path, "/readme") {
		data, _ := json.Marshal(map[string]string{"content": m.readmeContent})
		return json.Unmarshal(data, response)
	}
	return nil
}

// TestShowInfo_Readme_RendersMarkdown: --readme must render markdown, not dump raw syntax.
func TestShowInfo_Readme_RendersMarkdown(t *testing.T) {
	rawMD := "# Title\n\nSome **bold** text.\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(rawMD))

	mock := &mockReadmeClient{
		releases:      []Release{{ID: 1, TagName: "v1.0.0"}},
		readmeContent: encoded,
	}

	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository: "owner/repo",
			ShowReadme: 100,
		},
	}

	out := captureOutput(func() {
		err := showInfoWithClient(r, mock)
		assert.NoError(t, err)
	})

	assert.NotContains(t, out, "# Title", "must not print raw markdown headers")
	assert.NotContains(t, out, "**bold**", "must not print raw markdown bold")
	assert.Contains(t, out, "Title")
	assert.Contains(t, out, "bold")
}

func TestShowInfo_Assets_Visual4Categories(t *testing.T) {
	mock := &mockGhClient{
		releases: []Release{
			{ID: 200, TagName: "v1.0.0"},
		},
		assets: map[int64][]ReleaseAsset{
			200: {
				{ID: 1, Name: "mytool_linux_amd64.deb"},
				{ID: 2, Name: "mytool_linux_amd64.rpm"},
				{ID: 3, Name: "libhelper.so"},
				{ID: 4, Name: "mytool_windows_amd64.exe"},
				{ID: 5, Name: "mytool_android.apk"},
				{ID: 6, Name: "mytool.sha256"},
			},
		},
	}

	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository: "owner/mytool",
			ShowAssets: 50,
		},
	}

	out := captureOutput(func() {
		err := showInfoWithClient(r, mock)
		assert.NoError(t, err)
	})

	// 1. Categories present
	assert.Contains(t, out, "--- 📂 ASSETS 📂 ---")
	assert.Contains(t, out, "Will be installed by default")
	assert.Contains(t, out, "Will be installed with -s (--include-sidecars)")
	assert.Contains(t, out, "Can be installed but will not be")
	assert.Contains(t, out, "Should not / cannot be installed")

	// 2. Subcategories present
	assert.Contains(t, out, "native [NAT]:")
	assert.Contains(t, out, "sidecar [SIDE]:")
	assert.Contains(t, out, "checksum/hash [SUM]:")
	assert.Contains(t, out, "incompatible/unsupported [UNS]:")

	// 3. Emojis present by default
	assert.Contains(t, out, "🐧")
	assert.Contains(t, out, "🤖")
}

func TestShowInfo_Assets_NoEmojisAndNoColor(t *testing.T) {
	mock := &mockGhClient{
		releases: []Release{
			{ID: 200, TagName: "v1.0.0"},
		},
		assets: map[int64][]ReleaseAsset{
			200: {
				{ID: 1, Name: "mytool_linux_amd64.deb"},
				{ID: 2, Name: "mytool_windows_amd64.exe"},
				{ID: 3, Name: "mytool_android.apk"},
			},
		},
	}

	// Test with NoEmojis: true
	rNoEmojis := &RootCLI{
		ExecContext: params.ExecContext{
			Repository: "owner/mytool",
			ShowAssets: 50,
			CommonInstallFlags: params.CommonInstallFlags{
				NoEmojis: true,
			},
		},
	}

	outEmojis := captureOutput(func() {
		err := showInfoWithClient(rNoEmojis, mock)
		assert.NoError(t, err)
	})

	assert.Contains(t, outEmojis, "--- ASSETS ---")
	assert.Contains(t, outEmojis, "[linux] mytool_linux_amd64.deb")
	assert.Contains(t, outEmojis, "[android] mytool_android.apk")
	assert.NotContains(t, outEmojis, "🐧")
	assert.NotContains(t, outEmojis, "🤖")

	// Test with NoColor: true (which implies no emojis)
	rNoColor := &RootCLI{
		ExecContext: params.ExecContext{
			Repository: "owner/mytool",
			ShowAssets: 50,
			CommonInstallFlags: params.CommonInstallFlags{
				NoColor: true,
			},
		},
	}

	outColor := captureOutput(func() {
		err := showInfoWithClient(rNoColor, mock)
		assert.NoError(t, err)
	})

	assert.Contains(t, outColor, "[linux] mytool_linux_amd64.deb")
	assert.NotContains(t, outColor, "🐧")
}

func TestShowInfo_Assets_EmptyCategoriesOmitted(t *testing.T) {
	mock := &mockGhClient{
		releases: []Release{
			{ID: 200, TagName: "v1.0.0"},
		},
		assets: map[int64][]ReleaseAsset{
			200: {
				{ID: 1, Name: "single_binary_linux_amd64.tar.gz"},
			},
		},
	}

	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository: "owner/single",
			ShowAssets: 50,
		},
	}

	out := captureOutput(func() {
		err := showInfoWithClient(r, mock)
		assert.NoError(t, err)
	})

	assert.Contains(t, out, "Will be installed by default")
	assert.NotContains(t, out, "Will be installed with -s (--include-sidecars)", "empty sidecars category must be omitted")
	assert.NotContains(t, out, "Should not / cannot be installed", "empty cannot install category must be omitted")
}

func TestShowInfo_Assets_ThoriumAndOpenVINO_Presentation(t *testing.T) {
	thoriumMock := &mockGhClient{
		releases: []Release{{ID: 300, TagName: "v1.0.0"}},
		assets: map[int64][]ReleaseAsset{
			300: {
				{ID: 1, Name: "thorium-browser_154.0.8037.45_AVX2.deb"},
				{ID: 2, Name: "thorium-browser_154.0.8037.45_arm64.deb"},
				{ID: 3, Name: "SystemWebView_arm32.apk"},
				{ID: 4, Name: "Thorium_MacOS_ARM64.dmg"},
				{ID: 5, Name: "thorium_AVX2_mini_installer.exe"},
			},
		},
	}

	rThorium := &RootCLI{
		ExecContext: params.ExecContext{
			Repository: "gz83/thorium",
			ShowAssets: 50,
		},
	}

	outThorium := captureOutput(func() {
		err := showInfoWithClient(rThorium, thoriumMock)
		assert.NoError(t, err)
	})

	assert.Contains(t, outThorium, "thorium-browser_154.0.8037.45_AVX2.deb")
	assert.Contains(t, outThorium, "SystemWebView_arm32.apk")

	ovmsMock := &mockGhClient{
		releases: []Release{{ID: 400, TagName: "v2026.4.1"}},
		assets: map[int64][]ReleaseAsset{
			400: {
				{ID: 1, Name: "ovms_ubuntu22_2026.4.1_python_on.tar.gz"},
				{ID: 2, Name: "ovms_ubuntu22_2026.4.1_python_on.tar.gz.sha256"},
				{ID: 3, Name: "ovms_windows_2026.4.1_python_on.zip"},
			},
		},
	}

	rOvms := &RootCLI{
		ExecContext: params.ExecContext{
			Repository: "openvinotoolkit/model_server",
			ShowAssets: 50,
		},
	}

	outOvms := captureOutput(func() {
		err := showInfoWithClient(rOvms, ovmsMock)
		assert.NoError(t, err)
	})

	assert.Contains(t, outOvms, "ovms_ubuntu22_2026.4.1_python_on.tar.gz")
	assert.Contains(t, outOvms, "checksum/hash [SUM]:")
	assert.Contains(t, outOvms, "ovms_ubuntu22_2026.4.1_python_on.tar.gz.sha256")
}
