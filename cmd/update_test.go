package cmd

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adrg/xdg"
	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/joshsukhdeo/gh-pt/params"
	"github.com/joshsukhdeo/gh-pt/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockGQLClient implements GQLClient interface for testing
type mockGQLClient struct {
	doFunc    func(query string, variables map[string]interface{}, response interface{}) error
	callCount atomic.Int32
	lastQuery string
}

func (m *mockGQLClient) Do(query string, variables map[string]interface{}, response interface{}) error {
	m.callCount.Add(1)
	m.lastQuery = query
	if m.doFunc != nil {
		return m.doFunc(query, variables, response)
	}
	return nil
}

func TestBuildBatchReleaseQuery(t *testing.T) {
	t.Run("empty input returns empty query and map", func(t *testing.T) {
		query, aliasMap := BuildBatchReleaseQuery(nil)
		assert.Empty(t, query)
		assert.Empty(t, aliasMap)

		query, aliasMap = BuildBatchReleaseQuery([]string{})
		assert.Empty(t, query)
		assert.Empty(t, aliasMap)
	})

	t.Run("single repository", func(t *testing.T) {
		query, aliasMap := BuildBatchReleaseQuery([]string{"cli/cli"})
		assert.NotEmpty(t, query)
		assert.Equal(t, map[string]string{"repo_0": "cli/cli"}, aliasMap)
		assert.Contains(t, query, `repo_0: repository(owner: "cli", name: "cli")`)
		assert.Contains(t, query, "latestRelease")
		assert.Contains(t, query, "tagName")
	})

	t.Run("multiple repositories with sorting and deduplication", func(t *testing.T) {
		repos := []string{"sharkdp/fd", "cli/cli", "sharkdp/fd", "junegunn/fzf"}
		query, aliasMap := BuildBatchReleaseQuery(repos)

		assert.Len(t, aliasMap, 3)
		// Sorted: cli/cli (repo_0), junegunn/fzf (repo_1), sharkdp/fd (repo_2)
		assert.Equal(t, "cli/cli", aliasMap["repo_0"])
		assert.Equal(t, "junegunn/fzf", aliasMap["repo_1"])
		assert.Equal(t, "sharkdp/fd", aliasMap["repo_2"])

		assert.Contains(t, query, `repo_0: repository(owner: "cli", name: "cli")`)
		assert.Contains(t, query, `repo_1: repository(owner: "junegunn", name: "fzf")`)
		assert.Contains(t, query, `repo_2: repository(owner: "sharkdp", name: "fd")`)
	})

	t.Run("invalid repository formats are skipped", func(t *testing.T) {
		repos := []string{"invalid", "", "too/many/parts/here", "cli/cli"}
		query, aliasMap := BuildBatchReleaseQuery(repos)

		assert.Len(t, aliasMap, 1)
		assert.Equal(t, "cli/cli", aliasMap["repo_0"])
		assert.Contains(t, query, `repo_0: repository(owner: "cli", name: "cli")`)
		assert.NotContains(t, query, "invalid")
	})
}

func TestFetchLatestReleaseTags(t *testing.T) {
	t.Run("successful fetch for multiple repositories in one round-trip", func(t *testing.T) {
		mockClient := &mockGQLClient{
			doFunc: func(query string, variables map[string]interface{}, response interface{}) error {
				resp, ok := response.(*map[string]GQLRepoResult)
				require.True(t, ok)
				*resp = map[string]GQLRepoResult{
					"repo_0": {LatestRelease: &struct {
						TagName string `json:"tagName"`
						Name    string `json:"name"`
					}{TagName: "v2.40.0"}},
					"repo_1": {LatestRelease: &struct {
						TagName string `json:"tagName"`
						Name    string `json:"name"`
					}{TagName: "v0.52.0"}},
				}
				return nil
			},
		}

		repos := []string{"cli/cli", "junegunn/fzf"}
		tags, err := FetchLatestReleaseTags(mockClient, repos)
		require.NoError(t, err)
		assert.Equal(t, int32(1), mockClient.callCount.Load(), "Must fetch in exactly one network round-trip")
		assert.Equal(t, "v2.40.0", tags["cli/cli"])
		assert.Equal(t, "v0.52.0", tags["junegunn/fzf"])
	})

	t.Run("network error returns error", func(t *testing.T) {
		mockClient := &mockGQLClient{
			doFunc: func(query string, variables map[string]interface{}, response interface{}) error {
				return errors.New("network failure")
			},
		}

		_, err := FetchLatestReleaseTags(mockClient, []string{"cli/cli"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "network failure")
	})

	t.Run("partial GraphQL errors preserve valid results", func(t *testing.T) {
		mockClient := &mockGQLClient{
			doFunc: func(query string, variables map[string]interface{}, response interface{}) error {
				resp, ok := response.(*map[string]GQLRepoResult)
				require.True(t, ok)
				*resp = map[string]GQLRepoResult{
					"repo_0": {LatestRelease: &struct {
						TagName string `json:"tagName"`
						Name    string `json:"name"`
					}{TagName: "v2.40.0"}},
				}
				return &api.GraphQLError{
					Errors: []api.GraphQLErrorItem{
						{Message: "Could not resolve to a Repository with the name 'deleted/repo'."},
					},
				}
			},
		}

		repos := []string{"cli/cli", "deleted/repo"}
		tags, err := FetchLatestReleaseTags(mockClient, repos)
		require.NoError(t, err)
		assert.Equal(t, "v2.40.0", tags["cli/cli"])
		assert.NotContains(t, tags, "deleted/repo")
	})
}

func TestDoUpdate_TargetedUpgradeFiltersGraphQLQuery(t *testing.T) {
	t.Run("targeted upgrade only queries specified repo", func(t *testing.T) {
		tmpDir := t.TempDir()
		origDataHome := os.Getenv("XDG_DATA_HOME")
		t.Setenv("XDG_DATA_HOME", tmpDir)
		xdg.Reload()
		defer func() {
			_ = os.Setenv("XDG_DATA_HOME", origDataHome)
			xdg.Reload()
		}()

		st := &state.State{
			Version: 2,
			Apps: map[string]*state.InstalledApp{
				"owner/repo":     {Repository: "owner/repo", Version: "v1.0.0"},
				"another/repo":   {Repository: "another/repo", Version: "v2.0.0"},
				"third/fakerepo": {Repository: "third/fakerepo", Version: "v0.1.0"},
			},
		}
		require.NoError(t, st.Save())

		var capturedQuery string
		mockGQL := &mockGQLClient{
			doFunc: func(query string, variables map[string]interface{}, response interface{}) error {
				capturedQuery = query
				resp, ok := response.(*map[string]GQLRepoResult)
				require.True(t, ok)
				*resp = map[string]GQLRepoResult{
					"repo_0": {LatestRelease: &struct {
						TagName string `json:"tagName"`
						Name    string `json:"name"`
					}{TagName: "v2.0.0"}},
				}
				return nil
			},
		}

		origNewGQL := newGraphQLClient
		newGraphQLClient = func() (GQLClient, error) { return mockGQL, nil }
		defer func() { newGraphQLClient = origNewGQL }()

		origInstall := installReleaseFunc
		installReleaseFunc = func(appParams *params.ExecContext, ghClient *api.RESTClient) error {
			return nil
		}
		defer func() { installReleaseFunc = origInstall }()

		r := &RootCLI{}
		r.Repository = "owner/repo"
		r.Update = true
		r.VerifyChecksum = true

		ghClient, _ := api.DefaultRESTClient()
		err := DoUpdate(r, ghClient)
		require.NoError(t, err)

		assert.Contains(t, capturedQuery, `"owner"`)
		assert.Contains(t, capturedQuery, `"repo"`)
		assert.NotContains(t, capturedQuery, `"another"`)
		assert.NotContains(t, capturedQuery, `"third"`)
		assert.NotContains(t, capturedQuery, `"fakerepo"`)
	})
}

func TestDoUpdate_GraphQLBatchingAndFilterUpToDate(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	xdg.Reload()

	st, err := state.LoadState()
	require.NoError(t, err)

	// App 1: Already up to date (v1.0.0 == v1.0.0)
	require.NoError(t, st.AddApp(&state.InstalledApp{
		Repository: "test/app-uptodate",
		Version:    "v1.0.0",
		TargetPath: tmpDir,
	}))

	// App 2: Already up to date with normalized prefix (1.2.0 == v1.2.0)
	require.NoError(t, st.AddApp(&state.InstalledApp{
		Repository: "test/app-normalized",
		Version:    "1.2.0",
		TargetPath: tmpDir,
	}))

	// App 3: Outdated (v2.0.0 -> v2.1.0)
	require.NoError(t, st.AddApp(&state.InstalledApp{
		Repository: "test/app-outdated",
		Version:    "v2.0.0",
		TargetPath: tmpDir,
	}))

	mockGQL := &mockGQLClient{
		doFunc: func(query string, variables map[string]interface{}, response interface{}) error {
			resp, ok := response.(*map[string]GQLRepoResult)
			require.True(t, ok)
			*resp = map[string]GQLRepoResult{
				"repo_0": {LatestRelease: &struct {
					TagName string `json:"tagName"`
					Name    string `json:"name"`
				}{TagName: "v1.2.0"}}, // test/app-normalized
				"repo_1": {LatestRelease: &struct {
					TagName string `json:"tagName"`
					Name    string `json:"name"`
				}{TagName: "v2.1.0"}}, // test/app-outdated
				"repo_2": {LatestRelease: &struct {
					TagName string `json:"tagName"`
					Name    string `json:"name"`
				}{TagName: "v1.0.0"}}, // test/app-uptodate
			}
			return nil
		},
	}

	origNewGQL := newGraphQLClient
	newGraphQLClient = func() (GQLClient, error) {
		return mockGQL, nil
	}
	defer func() { newGraphQLClient = origNewGQL }()

	var installedRepos []string
	var mu sync.Mutex
	origInstall := installReleaseFunc
	installReleaseFunc = func(appParams *params.ExecContext, ghClient *api.RESTClient) error {
		mu.Lock()
		installedRepos = append(installedRepos, appParams.Repository)
		mu.Unlock()
		assert.True(t, appParams.NoSaveState, "Worker must NOT save state individually")
		return nil
	}
	defer func() { installReleaseFunc = origInstall }()

	r := &RootCLI{}
	r.Update = true

	err = DoUpdate(r, nil)
	require.NoError(t, err)

	assert.Equal(t, int32(1), mockGQL.callCount.Load(), "Exactly one GraphQL batch query must be executed")
	assert.Equal(t, []string{"test/app-outdated"}, installedRepos, "Only outdated app should be installed")

	// Verify state saved once with updated version for outdated app
	freshSt, err := state.LoadState()
	require.NoError(t, err)
	assert.Equal(t, "v2.1.0", freshSt.Apps["test/app-outdated"].Version)
	assert.Equal(t, "v1.0.0", freshSt.Apps["test/app-uptodate"].Version)
	assert.Equal(t, "1.2.0", freshSt.Apps["test/app-normalized"].Version)
}

func TestDoUpdate_AllUpToDate(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	xdg.Reload()

	st, err := state.LoadState()
	require.NoError(t, err)

	require.NoError(t, st.AddApp(&state.InstalledApp{
		Repository: "test/app-a",
		Version:    "v1.0.0",
		TargetPath: tmpDir,
	}))
	require.NoError(t, st.AddApp(&state.InstalledApp{
		Repository: "test/app-b",
		Version:    "v2.0.0",
		TargetPath: tmpDir,
	}))

	mockGQL := &mockGQLClient{
		doFunc: func(query string, variables map[string]interface{}, response interface{}) error {
			resp, ok := response.(*map[string]GQLRepoResult)
			require.True(t, ok)
			*resp = map[string]GQLRepoResult{
				"repo_0": {LatestRelease: &struct {
					TagName string `json:"tagName"`
					Name    string `json:"name"`
				}{TagName: "v1.0.0"}},
				"repo_1": {LatestRelease: &struct {
					TagName string `json:"tagName"`
					Name    string `json:"name"`
				}{TagName: "v2.0.0"}},
			}
			return nil
		},
	}

	origNewGQL := newGraphQLClient
	newGraphQLClient = func() (GQLClient, error) {
		return mockGQL, nil
	}
	defer func() { newGraphQLClient = origNewGQL }()

	origInstall := installReleaseFunc
	installReleaseFunc = func(appParams *params.ExecContext, ghClient *api.RESTClient) error {
		t.Fatalf("installReleaseFunc should not be called when all apps are up to date")
		return nil
	}
	defer func() { installReleaseFunc = origInstall }()

	r := &RootCLI{}
	r.Update = true
	err = DoUpdate(r, nil)
	require.NoError(t, err)
	assert.Equal(t, int32(1), mockGQL.callCount.Load())
}

func TestDoUpdate_ConcurrentWorkerPool(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	xdg.Reload()

	st, err := state.LoadState()
	require.NoError(t, err)

	// Create 6 outdated apps
	rawResponseMap := make(map[string]GQLRepoResult)
	for i := 0; i < 6; i++ {
		repo := fmt.Sprintf("test/concurrent-app-%d", i)
		require.NoError(t, st.AddApp(&state.InstalledApp{
			Repository: repo,
			Version:    "v1.0.0",
			TargetPath: tmpDir,
		}))
		alias := fmt.Sprintf("repo_%d", i)
		rawResponseMap[alias] = GQLRepoResult{
			LatestRelease: &struct {
				TagName string `json:"tagName"`
				Name    string `json:"name"`
			}{TagName: "v2.0.0"},
		}
	}

	mockGQL := &mockGQLClient{
		doFunc: func(query string, variables map[string]interface{}, response interface{}) error {
			resp, ok := response.(*map[string]GQLRepoResult)
			require.True(t, ok)
			*resp = rawResponseMap
			return nil
		},
	}

	origNewGQL := newGraphQLClient
	newGraphQLClient = func() (GQLClient, error) {
		return mockGQL, nil
	}
	defer func() { newGraphQLClient = origNewGQL }()

	var activeConcurrency atomic.Int32
	var maxObservedConcurrency atomic.Int32
	var installedCount atomic.Int32

	origConcurrencyLimit := UpdateConcurrencyLimit
	UpdateConcurrencyLimit = 3
	defer func() { UpdateConcurrencyLimit = origConcurrencyLimit }()

	origInstall := installReleaseFunc
	installReleaseFunc = func(appParams *params.ExecContext, ghClient *api.RESTClient) error {
		cur := activeConcurrency.Add(1)
		for {
			oldMax := maxObservedConcurrency.Load()
			if cur <= oldMax || maxObservedConcurrency.CompareAndSwap(oldMax, cur) {
				break
			}
		}

		time.Sleep(30 * time.Millisecond) // simulate download/install latency
		activeConcurrency.Add(-1)
		installedCount.Add(1)
		return nil
	}
	defer func() { installReleaseFunc = origInstall }()

	r := &RootCLI{}
	r.Update = true
	err = DoUpdate(r, nil)
	require.NoError(t, err)

	assert.Equal(t, int32(6), installedCount.Load(), "All 6 apps should be updated")
	assert.Greater(t, maxObservedConcurrency.Load(), int32(1), "Workers should run concurrently")
	assert.LessOrEqual(t, maxObservedConcurrency.Load(), int32(3), "Concurrency should not exceed worker pool limit")

	// Verify all 6 versions saved in state
	freshSt, err := state.LoadState()
	require.NoError(t, err)
	for i := 0; i < 6; i++ {
		repo := fmt.Sprintf("test/concurrent-app-%d", i)
		assert.Equal(t, "v2.0.0", freshSt.Apps[repo].Version)
	}
}

func TestDoUpdate_WorkerErrorHandling(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	xdg.Reload()

	st, err := state.LoadState()
	require.NoError(t, err)

	require.NoError(t, st.AddApp(&state.InstalledApp{
		Repository: "test/failing-app",
		Version:    "v1.0.0",
		TargetPath: tmpDir,
	}))
	require.NoError(t, st.AddApp(&state.InstalledApp{
		Repository: "test/succeeding-app",
		Version:    "v1.0.0",
		TargetPath: tmpDir,
	}))

	mockGQL := &mockGQLClient{
		doFunc: func(query string, variables map[string]interface{}, response interface{}) error {
			resp, ok := response.(*map[string]GQLRepoResult)
			require.True(t, ok)
			*resp = map[string]GQLRepoResult{
				"repo_0": {LatestRelease: &struct {
					TagName string `json:"tagName"`
					Name    string `json:"name"`
				}{TagName: "v2.0.0"}}, // failing-app
				"repo_1": {LatestRelease: &struct {
					TagName string `json:"tagName"`
					Name    string `json:"name"`
				}{TagName: "v2.0.0"}}, // succeeding-app
			}
			return nil
		},
	}

	origNewGQL := newGraphQLClient
	newGraphQLClient = func() (GQLClient, error) {
		return mockGQL, nil
	}
	defer func() { newGraphQLClient = origNewGQL }()

	origInstall := installReleaseFunc
	installReleaseFunc = func(appParams *params.ExecContext, ghClient *api.RESTClient) error {
		if appParams.Repository == "test/failing-app" {
			return errors.New("download failed: 404 not found")
		}
		return nil
	}
	defer func() { installReleaseFunc = origInstall }()

	r := &RootCLI{}
	r.Update = true
	err = DoUpdate(r, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "download failed: 404 not found")

	// Succeeding app should still have its state saved
	freshSt, err := state.LoadState()
	require.NoError(t, err)
	assert.Equal(t, "v2.0.0", freshSt.Apps["test/succeeding-app"].Version)
	assert.Equal(t, "v1.0.0", freshSt.Apps["test/failing-app"].Version)
}

func TestDoUpdate_PinnedAndDisabledSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	xdg.Reload()

	st, err := state.LoadState()
	require.NoError(t, err)

	require.NoError(t, st.AddApp(&state.InstalledApp{
		Repository: "test/pinned-app",
		Version:    "v1.0.0",
		Pinned:     true,
		TargetPath: tmpDir,
	}))
	require.NoError(t, st.AddApp(&state.InstalledApp{
		Repository: "test/disabled-app",
		Version:    "v1.0.0",
		Disabled:   true,
		TargetPath: tmpDir,
	}))

	mockGQL := &mockGQLClient{
		doFunc: func(query string, variables map[string]interface{}, response interface{}) error {
			resp, ok := response.(*map[string]GQLRepoResult)
			require.True(t, ok)
			*resp = map[string]GQLRepoResult{
				"repo_0": {LatestRelease: &struct {
					TagName string `json:"tagName"`
					Name    string `json:"name"`
				}{TagName: "v2.0.0"}},
				"repo_1": {LatestRelease: &struct {
					TagName string `json:"tagName"`
					Name    string `json:"name"`
				}{TagName: "v2.0.0"}},
			}
			return nil
		},
	}

	origNewGQL := newGraphQLClient
	newGraphQLClient = func() (GQLClient, error) {
		return mockGQL, nil
	}
	defer func() { newGraphQLClient = origNewGQL }()

	origInstall := installReleaseFunc
	installReleaseFunc = func(appParams *params.ExecContext, ghClient *api.RESTClient) error {
		t.Fatalf("Pinned or disabled apps should not trigger installation")
		return nil
	}
	defer func() { installReleaseFunc = origInstall }()

	r := &RootCLI{}
	r.Update = true
	err = DoUpdate(r, nil)
	require.NoError(t, err)
}

func TestDoUpdate_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	xdg.Reload()

	st, err := state.LoadState()
	require.NoError(t, err)

	require.NoError(t, st.AddApp(&state.InstalledApp{
		Repository: "test/dryrun-app",
		Version:    "v1.0.0",
		TargetPath: tmpDir,
	}))

	mockGQL := &mockGQLClient{
		doFunc: func(query string, variables map[string]interface{}, response interface{}) error {
			resp, ok := response.(*map[string]GQLRepoResult)
			require.True(t, ok)
			*resp = map[string]GQLRepoResult{
				"repo_0": {LatestRelease: &struct {
					TagName string `json:"tagName"`
					Name    string `json:"name"`
				}{TagName: "v2.0.0"}},
			}
			return nil
		},
	}

	origNewGQL := newGraphQLClient
	newGraphQLClient = func() (GQLClient, error) {
		return mockGQL, nil
	}
	defer func() { newGraphQLClient = origNewGQL }()

	origInstall := installReleaseFunc
	installReleaseFunc = func(appParams *params.ExecContext, ghClient *api.RESTClient) error {
		t.Fatalf("Install should not be called during dry run")
		return nil
	}
	defer func() { installReleaseFunc = origInstall }()

	r := &RootCLI{}
	r.Update = true
	r.DryRun = true
	err = DoUpdate(r, nil)
	require.NoError(t, err)

	// Verify state was NOT updated
	freshSt, err := state.LoadState()
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", freshSt.Apps["test/dryrun-app"].Version)
}

func TestDoUpdate_InsecureAllowUnsigned_PerItemEnforcement(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	xdg.Reload()

	st, err := state.LoadState()
	require.NoError(t, err)

	// App A: was previously signed
	require.NoError(t, st.AddApp(&state.InstalledApp{
		Repository: "test/signed-app",
		Version:    "v1.0.0",
		TargetPath: tmpDir,
		WasSigned:  true,
	}))

	// App B: genuinely lacked signatures
	require.NoError(t, st.AddApp(&state.InstalledApp{
		Repository: "test/unsigned-app",
		Version:    "v1.0.0",
		TargetPath: tmpDir,
		WasSigned:  false,
	}))

	mockGQL := &mockGQLClient{
		doFunc: func(query string, variables map[string]interface{}, response interface{}) error {
			resp, ok := response.(*map[string]GQLRepoResult)
			require.True(t, ok)
			*resp = map[string]GQLRepoResult{
				"repo_0": {LatestRelease: &struct {
					TagName string `json:"tagName"`
					Name    string `json:"name"`
				}{TagName: "v2.0.0"}},
				"repo_1": {LatestRelease: &struct {
					TagName string `json:"tagName"`
					Name    string `json:"name"`
				}{TagName: "v2.0.0"}},
			}
			return nil
		},
	}

	origNewGQL := newGraphQLClient
	newGraphQLClient = func() (GQLClient, error) {
		return mockGQL, nil
	}
	defer func() { newGraphQLClient = origNewGQL }()

	var mu sync.Mutex
	capturedInsecure := make(map[string]bool)

	origInstall := installReleaseFunc
	installReleaseFunc = func(appParams *params.ExecContext, ghClient *api.RESTClient) error {
		mu.Lock()
		capturedInsecure[appParams.Repository] = appParams.InsecureAllowUnsigned
		mu.Unlock()
		return nil
	}
	defer func() { installReleaseFunc = origInstall }()

	// Case 1: Bulk upgrade with --insecure-allow-unsigned
	// Guarantees flag only permits unsigned for packages that genuinely lack signatures,
	// while strictly enforcing cryptographic checks on packages that previously possessed them.
	r := &RootCLI{}
	r.Update = true
	r.InsecureAllowUnsigned = true
	err = DoUpdate(r, nil)
	require.NoError(t, err)

	mu.Lock()
	assert.False(t, capturedInsecure["test/signed-app"], "previously signed app must not inherit allow-unsigned in bulk upgrade")
	assert.True(t, capturedInsecure["test/unsigned-app"], "unsigned app must allow unsigned in bulk upgrade when flag is set")
	mu.Unlock()

	// Case 2: Targeted upgrade specifically overrides per-item
	mockGQL.doFunc = func(query string, variables map[string]interface{}, response interface{}) error {
		resp, ok := response.(*map[string]GQLRepoResult)
		require.True(t, ok)
		*resp = map[string]GQLRepoResult{
			"repo_0": {LatestRelease: &struct {
				TagName string `json:"tagName"`
				Name    string `json:"name"`
			}{TagName: "v3.0.0"}},
		}
		return nil
	}

	mu.Lock()
	capturedInsecure = make(map[string]bool)
	mu.Unlock()

	rTargeted := &RootCLI{}
	rTargeted.Update = true
	rTargeted.Repository = "test/signed-app"
	rTargeted.InsecureAllowUnsigned = true
	err = DoUpdate(rTargeted, nil)
	require.NoError(t, err)

	mu.Lock()
	assert.True(t, capturedInsecure["test/signed-app"], "explicitly targeted repo must honor user override")
	mu.Unlock()
}

func TestDoUpdate_InsecureAllowUnsigned_Force_TargetedVsGeneric(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	xdg.Reload()

	st, err := state.LoadState()
	require.NoError(t, err)

	require.NoError(t, st.AddApp(&state.InstalledApp{
		Repository: "test/signed-app",
		Version:    "v1.0.0",
		TargetPath: tmpDir,
		WasSigned:  true,
		Global:     false,
	}))

	require.NoError(t, st.AddApp(&state.InstalledApp{
		Repository: "test/signed-global-app",
		Version:    "v1.0.0",
		TargetPath: tmpDir,
		WasSigned:  true,
		Global:     true,
	}))

	mockGQL := &mockGQLClient{
		doFunc: func(query string, variables map[string]interface{}, response interface{}) error {
			resp, ok := response.(*map[string]GQLRepoResult)
			require.True(t, ok)
			*resp = map[string]GQLRepoResult{
				"repo_0": {LatestRelease: &struct {
					TagName string `json:"tagName"`
					Name    string `json:"name"`
				}{TagName: "v2.0.0"}},
				"repo_1": {LatestRelease: &struct {
					TagName string `json:"tagName"`
					Name    string `json:"name"`
				}{TagName: "v2.0.0"}},
			}
			return nil
		},
	}

	origNewGQL := newGraphQLClient
	newGraphQLClient = func() (GQLClient, error) {
		return mockGQL, nil
	}
	defer func() { newGraphQLClient = origNewGQL }()

	var mu sync.Mutex
	capturedInsecure := make(map[string]bool)
	capturedTargeted := make(map[string]bool)

	origInstall := installReleaseFunc
	installReleaseFunc = func(appParams *params.ExecContext, ghClient *api.RESTClient) error {
		mu.Lock()
		capturedInsecure[appParams.Repository] = appParams.InsecureAllowUnsigned
		capturedTargeted[appParams.Repository] = appParams.SpecificallyTargeted
		mu.Unlock()
		return nil
	}
	defer func() { installReleaseFunc = origInstall }()

	// 1. Generic Default User + Global (all applications): r.Repository == "", r.UpdateAll == true
	rAll := &RootCLI{}
	rAll.UpdateAll = true
	rAll.InsecureAllowUnsigned = true
	rAll.Overwrite = true // --force
	err = DoUpdate(rAll, nil)
	require.NoError(t, err)

	mu.Lock()
	assert.False(t, capturedInsecure["test/signed-app"], "all-applications context must NOT bypass verification for previously signed app")
	assert.False(t, capturedTargeted["test/signed-app"], "all-applications context must NOT be specifically targeted")
	assert.False(t, capturedInsecure["test/signed-global-app"], "all-applications context must NOT bypass verification for previously signed global app")
	mu.Unlock()

	// 2. Generic User context: r.Update == true, r.Global == false, r.Repository == ""
	mu.Lock()
	capturedInsecure = make(map[string]bool)
	capturedTargeted = make(map[string]bool)
	mu.Unlock()

	rUser := &RootCLI{}
	rUser.Update = true
	rUser.Global = false
	rUser.InsecureAllowUnsigned = true
	rUser.Overwrite = true // --force
	err = DoUpdate(rUser, nil)
	require.NoError(t, err)

	mu.Lock()
	assert.False(t, capturedInsecure["test/signed-app"], "generic user context must NOT bypass verification for previously signed app even with --force")
	assert.False(t, capturedTargeted["test/signed-app"], "generic user context must NOT be specifically targeted")
	mu.Unlock()

	// 3. Generic Global context: r.Update == true, r.Global == true, r.Repository == ""
	mu.Lock()
	capturedInsecure = make(map[string]bool)
	capturedTargeted = make(map[string]bool)
	mu.Unlock()

	rGlobal := &RootCLI{}
	rGlobal.Update = true
	rGlobal.Global = true
	rGlobal.InsecureAllowUnsigned = true
	rGlobal.Overwrite = true // --force
	err = DoUpdate(rGlobal, nil)
	require.NoError(t, err)

	mu.Lock()
	assert.False(t, capturedInsecure["test/signed-global-app"], "generic global context must NOT bypass verification for previously signed app even with --force")
	assert.False(t, capturedTargeted["test/signed-global-app"], "generic global context must NOT be specifically targeted")
	mu.Unlock()

	// 4. Specifically targeted named entry: r.Repository == "test/signed-app"
	mu.Lock()
	capturedInsecure = make(map[string]bool)
	capturedTargeted = make(map[string]bool)
	mu.Unlock()

	mockGQL.doFunc = func(query string, variables map[string]interface{}, response interface{}) error {
		resp, ok := response.(*map[string]GQLRepoResult)
		require.True(t, ok)
		*resp = map[string]GQLRepoResult{
			"repo_0": {LatestRelease: &struct {
				TagName string `json:"tagName"`
				Name    string `json:"name"`
			}{TagName: "v3.0.0"}},
		}
		return nil
	}

	rTargeted := &RootCLI{}
	rTargeted.Update = true
	rTargeted.Repository = "test/signed-app"
	rTargeted.InsecureAllowUnsigned = true
	rTargeted.Overwrite = true // --force
	err = DoUpdate(rTargeted, nil)
	require.NoError(t, err)

	mu.Lock()
	assert.True(t, capturedInsecure["test/signed-app"], "specifically targeted named entry with --force must allow bypass")
	assert.True(t, capturedTargeted["test/signed-app"], "specifically targeted named entry must set SpecificallyTargeted = true")
	mu.Unlock()
}
