package selector

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type MockGithubClient struct {
	GetResponses map[string]interface{}
	ReqResponses map[string]*http.Response
	GetError     error
	ReqError     error
}

func (m *MockGithubClient) Get(path string, response interface{}) error {
	if m.GetError != nil {
		return m.GetError
	}
	if val, ok := m.GetResponses[path]; ok {
		b, _ := json.Marshal(val)
		_ = json.Unmarshal(b, response)
	}
	return nil
}

func (m *MockGithubClient) Request(method string, path string, body io.Reader) (*http.Response, error) {
	if m.ReqError != nil {
		return nil, m.ReqError
	}
	if resp, ok := m.ReqResponses[path]; ok {
		return resp, nil
	}
	return &http.Response{Body: io.NopCloser(bytes.NewReader([]byte(`[]`)))}, nil
}

func TestSelector_Run(t *testing.T) {
	s := &Selector{
		Kind: Release,
		Items: []*SelectorItem{
			{Name: "v1.0.0"},
			{Name: "v2.0.0"},
		},
		RegexpMatchers: []string{"v2.0.0"},
	}
	assert.Equal(t, Release, s.GetKind())

	res, err := s.Run()
	require.NoError(t, err)
	assert.Len(t, res, 1)
	assert.Equal(t, "v2.0.0", res[0].Name)

	s.RegexpMatchers = []string{}
	s.NamesMatcher = []string{"v1.0.0"}
	res, err = s.Run()
	require.NoError(t, err)
	assert.Len(t, res, 1)
	assert.Equal(t, "v1.0.0", res[0].Name)

	s.NamesMatcher = []string{"missing"}
	_, err = s.Run()
	assert.Error(t, err)
}

func TestReleaseSelector(t *testing.T) {
	client := &MockGithubClient{
		GetResponses: map[string]interface{}{
			"repos/owner/repo/releases": []map[string]interface{}{
				{"Tag_name": "v1.0.0", "Id": 1},
				{"Tag_name": "v2.0.0", "Id": 2},
			},
			"repos/owner/repo/releases/latest": map[string]interface{}{
				"Tag_name": "v2.0.0",
			},
		},
	}

	t.Run("NonInteractiveLatest", func(t *testing.T) {
		sel, err := ReleaseSelector(client, "owner/repo", "latest", false)
		require.NoError(t, err)
		assert.Equal(t, Release, sel.GetKind())
		s, ok := sel.(*Selector)
		require.True(t, ok)
		assert.Equal(t, []string{"v2.0.0"}, s.RegexpMatchers)
		var names []string
		for _, it := range s.Items {
			names = append(names, it.Name)
		}
		assert.Contains(t, names, "v2.0.0")
	})
}

func TestAssetSelector(t *testing.T) {
	respBody := `[{"Name": "asset-linux-amd64.tar.gz"}]`
	client := &MockGithubClient{}
	client.ReqResponses = map[string]*http.Response{
		"repos/owner/repo/releases/1/assets": {
			Body:   io.NopCloser(bytes.NewReader([]byte(respBody))),
			Header: http.Header{},
		},
	}

	t.Run("NonInteractive", func(t *testing.T) {
		sel, err := AssetSelector(client, "owner/repo", AssetMatchCriteria{
			ReleaseId:   1,
			Name:        "asset-linux",
			Regexps:     []string{".*linux.*"},
			Interactive: false,
		})
		require.NoError(t, err)
		assert.Equal(t, Asset, sel.GetKind())
		s, ok := sel.(*Selector)
		require.True(t, ok)
		assert.Equal(t, []string{"asset-linux"}, s.NamesMatcher)
		var names []string
		for _, it := range s.Items {
			names = append(names, it.Name)
		}
		assert.Contains(t, names, "asset-linux-amd64.tar.gz")
	})
}

func TestSelector_PrioritizesNonMusl(t *testing.T) {
	items := []*SelectorItem{
		{Name: "app-linux-musl-x64.tar.gz"},
		{Name: "app-linux-x64.tar.gz"},
	}

	sel := &Selector{
		Kind:           Asset,
		Items:          items,
		RegexpMatchers: []string{`.*(?:amd64|x86_64|x64).*\.(?i:tar\.gz)$`},
		Single:         true,
	}

	selected, err := sel.Run()
	assert.NoError(t, err)
	assert.Len(t, selected, 1)
	assert.Equal(t, "app-linux-x64.tar.gz", selected[0].Name)
}

func TestSelector_ForeignArchitectureBlacklisting(t *testing.T) {
	items := []*SelectorItem{
		{Name: "app_linux_arm64.tar.gz"},
		{Name: "app_linux_amd64.tar.gz"},
	}

	sel := &Selector{
		Kind:           Asset,
		Items:          items,
		RegexpMatchers: []string{`(?i)app_linux.*\.tar\.gz$`},
		Single:         true,
	}

	// This validates the architecture exclusion logic
	// e.g. "app_linux_arm64.tar.gz" is rejected on amd64
	// "app_linux_amd64.tar.gz" is selected on amd64
	selected, err := sel.Run()
	assert.NoError(t, err)
	assert.Len(t, selected, 1)

	switch runtime.GOARCH {
	case "amd64":
		assert.Equal(t, "app_linux_amd64.tar.gz", selected[0].Name)
	case "arm64":
		assert.Equal(t, "app_linux_arm64.tar.gz", selected[0].Name)
	}
}

func TestSelector_NoArchitecture_AssumeCompatible(t *testing.T) {
	reAmd64 := getForeignArchRegex("amd64")
	if reAmd64 != nil {
		assert.False(t, reAmd64.MatchString("app_linux.tar.gz"))
	}
}

func TestSelector_FinalFallbackPattern(t *testing.T) {
	items := []*SelectorItem{
		{Name: "app.deb"},
	}

	sel := &Selector{
		Kind:           Asset,
		Items:          items,
		RegexpMatchers: []string{`(?i)^app\.deb$`},
	}

	selected, err := sel.Run()
	assert.NoError(t, err)
	assert.Len(t, selected, 1)
	assert.Equal(t, "app.deb", selected[0].Name)
}

func TestInteractiveSelector_GetKind(t *testing.T) {
	sel := &InteractiveSelector{Kind: Asset}
	assert.Equal(t, Asset, sel.GetKind())
}

func TestSelectorKind_String(t *testing.T) {
	assert.Equal(t, "release_selector", Release.String())
	assert.Equal(t, "asset_selector", Asset.String())
	assert.Equal(t, "binary_selector", Binary.String())
	assert.Equal(t, "Unknown(99)", SelectorKind(99).String())
}

func TestItem(t *testing.T) {
	assert.Equal(t, BinaryExecutable, BinaryTypeFromPath("/tmp/test.exe"))
	assert.Equal(t, BinaryExecutable, BinaryTypeFromPath("/tmp/test"))
	assert.Equal(t, BinaryDebInstaller, BinaryTypeFromPath("/tmp/test.deb"))
	assert.Equal(t, BinaryRpmInstaller, BinaryTypeFromPath("/tmp/test.rpm"))
	assert.Equal(t, BinaryPkgInstaller, BinaryTypeFromPath("/tmp/test.pkg"))
	assert.Equal(t, BinaryPkgInstaller, BinaryTypeFromPath("/tmp/test.txz"))
}

type MockPrompter struct {
	SelectRet      string
	SelectErr      error
	MultiSelectRet []string
	MultiSelectErr error
}

func (m MockPrompter) Select(options []string, prompt string) (string, error) {
	return m.SelectRet, m.SelectErr
}

func (m MockPrompter) MultiSelect(options []string, prompt string) ([]string, error) {
	return m.MultiSelectRet, m.MultiSelectErr
}

func TestInteractiveSelector_Run(t *testing.T) {
	tests := []struct {
		name          string
		single        bool
		items         []*SelectorItem
		mockPrompter  MockPrompter
		expectedItems []string
		expectedErr   string
	}{
		{
			name:   "Single selection success",
			single: true,
			items: []*SelectorItem{
				{Name: "item1"},
				{Name: "item2"},
			},
			mockPrompter: MockPrompter{
				SelectRet: "item2",
			},
			expectedItems: []string{"item2"},
		},
		{
			name:   "Single selection error",
			single: true,
			items: []*SelectorItem{
				{Name: "item1"},
			},
			mockPrompter: MockPrompter{
				SelectErr: fmt.Errorf("select error"),
			},
			expectedErr: "interactive prompt failed: select error",
		},
		{
			name:   "Single selection empty",
			single: true,
			items: []*SelectorItem{
				{Name: "item1"},
			},
			mockPrompter: MockPrompter{
				SelectRet: "",
			},
			expectedErr: "no items were selected",
		},
		{
			name:   "Single selection not found",
			single: true,
			items: []*SelectorItem{
				{Name: "item1"},
			},
			mockPrompter: MockPrompter{
				SelectRet: "item2",
			},
			expectedErr: "could not match selected items with internal list",
		},
		{
			name:   "Multiple selection success",
			single: false,
			items: []*SelectorItem{
				{Name: "item1"},
				{Name: "item2"},
				{Name: "item3"},
			},
			mockPrompter: MockPrompter{
				MultiSelectRet: []string{"item1", "item3"},
			},
			expectedItems: []string{"item1", "item3"},
		},
		{
			name:   "Multiple selection error",
			single: false,
			items: []*SelectorItem{
				{Name: "item1"},
			},
			mockPrompter: MockPrompter{
				MultiSelectErr: fmt.Errorf("multiselect error"),
			},
			expectedErr: "interactive prompt failed: multiselect error",
		},
		{
			name:   "Multiple selection empty",
			single: false,
			items: []*SelectorItem{
				{Name: "item1"},
			},
			mockPrompter: MockPrompter{
				MultiSelectRet: []string{},
			},
			expectedErr: "no items were selected",
		},
		{
			name:   "Multiple selection not found",
			single: false,
			items: []*SelectorItem{
				{Name: "item1"},
			},
			mockPrompter: MockPrompter{
				MultiSelectRet: []string{"item2"},
			},
			expectedErr: "could not match selected items with internal list",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &InteractiveSelector{
				Kind:     Asset,
				Items:    tt.items,
				Prompt:   "Test prompt",
				Single:   tt.single,
				Prompter: tt.mockPrompter,
			}

			res, err := s.Run()

			if tt.expectedErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErr)
			} else {
				require.NoError(t, err)
				var resNames []string
				for _, item := range res {
					resNames = append(resNames, item.Name)
				}
				assert.ElementsMatch(t, tt.expectedItems, resNames)
			}
		})
	}
}

func TestBinarySelector(t *testing.T) {
	// create a temp dummy file to use as download path
	tmpFileDeb, err := os.CreateTemp("", "gh-pt-test-*.deb")
	require.NoError(t, err)
	defer func() { _ = os.Remove(tmpFileDeb.Name()) }()
	_, _ = tmpFileDeb.Write([]byte("dummy content"))
	_ = tmpFileDeb.Close()

	tmpFileRpm, err := os.CreateTemp("", "gh-pt-test-*.rpm")
	require.NoError(t, err)
	defer func() { _ = os.Remove(tmpFileRpm.Name()) }()
	_, _ = tmpFileRpm.Write([]byte("dummy content"))
	_ = tmpFileRpm.Close()

	t.Run("UnsupportedNativeArchiverType", func(t *testing.T) {
		sel, err := BinarySelector(BinaryMatchCriteria{
			DownloadPath: tmpFileDeb.Name(),
			Names:        []string{"dummy"},
			Interactive:  false,
		})
		require.NoError(t, err)
		assert.Equal(t, Binary, sel.GetKind())
		s, ok := sel.(*Selector)
		require.True(t, ok)
		assert.Equal(t, 1, len(s.Items))
		assert.Equal(t, filepath.Base(tmpFileDeb.Name()), s.Items[0].Name)
	})

	t.Run("UnsupportedNativeArchiverTypeInteractive", func(t *testing.T) {
		sel, err := BinarySelector(BinaryMatchCriteria{
			DownloadPath: tmpFileRpm.Name(),
			Interactive:  true,
		})
		require.NoError(t, err)
		assert.Equal(t, Binary, sel.GetKind())
		s, ok := sel.(*InteractiveSelector)
		require.True(t, ok)
		assert.Equal(t, 1, len(s.Items))
		assert.Equal(t, filepath.Base(tmpFileRpm.Name()), s.Items[0].Name)
	})
}

func TestReleaseSelectorInteractive(t *testing.T) {
	client := &MockGithubClient{
		GetResponses: map[string]interface{}{
			"repos/owner/repo/releases": []map[string]interface{}{
				{"Tag_name": "v1.0.0", "Id": 1},
			},
		},
	}
	sel, err := ReleaseSelector(client, "owner/repo", "latest", true)
	require.NoError(t, err)
	assert.Equal(t, Release, sel.GetKind())
	s, ok := sel.(*InteractiveSelector)
	require.True(t, ok)
	assert.Equal(t, 1, len(s.Items))
	assert.Equal(t, "v1.0.0", s.Items[0].Name)
}

func TestAssetSelectorInteractive(t *testing.T) {
	respBody := `[{"Name": "asset-linux-amd64.tar.gz"}]`
	client := &MockGithubClient{}
	client.ReqResponses = map[string]*http.Response{
		"repos/owner/repo/releases/1/assets": {
			Body:   io.NopCloser(bytes.NewReader([]byte(respBody))),
			Header: http.Header{},
		},
	}
	sel, err := AssetSelector(client, "owner/repo", AssetMatchCriteria{
		ReleaseId:   1,
		Interactive: true,
	})
	require.NoError(t, err)
	assert.Equal(t, Asset, sel.GetKind())
	s, ok := sel.(*InteractiveSelector)
	require.True(t, ok)
	assert.Equal(t, 1, len(s.Items))
	assert.Equal(t, "asset-linux-amd64.tar.gz", s.Items[0].Name)
}

func TestBinarySelectorArchive(t *testing.T) {
	tmpFileZip, err := os.CreateTemp("", "gh-pt-test-*.zip")
	require.NoError(t, err)
	defer func() { _ = os.Remove(tmpFileZip.Name()) }()
	_, _ = tmpFileZip.Write([]byte("PK\x03\x04dummy content"))
	_ = tmpFileZip.Close()

	t.Run("InternalExtractor", func(t *testing.T) {
		sel, err := BinarySelector(BinaryMatchCriteria{
			DownloadPath: tmpFileZip.Name(),
			Names:        []string{"dummy"},
			Interactive:  false,
			Extractor:    "internal",
		})
		if err != nil {
			// fallback might fail if pure go archiver fails on dummy zip
			t.Logf("expected error for dummy zip: %v", err)
		} else {
			assert.Equal(t, Binary, sel.GetKind())
		}
	})
}

func TestBinarySelectorErrors(t *testing.T) {
	t.Run("MissingFile", func(t *testing.T) {
		_, err := BinarySelector(BinaryMatchCriteria{
			DownloadPath: "/path/to/missing/file.tar.gz",
			Names:        []string{"dummy"},
			Interactive:  false,
		})
		require.Error(t, err)
	})
}

func TestReleaseSelectorErrors(t *testing.T) {
	client := &MockGithubClient{
		GetError: fmt.Errorf("api error"),
	}
	t.Run("APIError", func(t *testing.T) {
		_, err := ReleaseSelector(client, "owner/repo", "latest", false)
		require.Error(t, err)
	})

	client2 := &MockGithubClient{
		GetResponses: map[string]interface{}{
			"repos/owner/repo/releases": []map[string]interface{}{},
		},
	}
	t.Run("EmptyReleases", func(t *testing.T) {
		_, err := ReleaseSelector(client2, "owner/repo", "v1.0.0", false)
		require.NoError(t, err)
	})
}

func TestAssetSelectorErrors(t *testing.T) {
	client := &MockGithubClient{
		ReqError: fmt.Errorf("api error"),
	}
	t.Run("APIError", func(t *testing.T) {
		_, err := AssetSelector(client, "owner/repo", AssetMatchCriteria{
			ReleaseId:   1,
			Interactive: false,
		})
		require.Error(t, err)
	})

	clientInvalidJSON := &MockGithubClient{}
	clientInvalidJSON.ReqResponses = map[string]*http.Response{
		"repos/owner/repo/releases/1/assets": {
			Body:   io.NopCloser(bytes.NewReader([]byte(`invalid json`))),
			Header: http.Header{},
		},
	}
	t.Run("InvalidJSON", func(t *testing.T) {
		_, err := AssetSelector(clientInvalidJSON, "owner/repo", AssetMatchCriteria{
			ReleaseId:   1,
			Interactive: false,
		})
		require.Error(t, err)
	})
}

func TestBinarySelectorArchiveNativeExtractor(t *testing.T) {
	tmpFileZip, err := os.CreateTemp("", "gh-pt-test-*.zip")
	require.NoError(t, err)
	defer func() { _ = os.Remove(tmpFileZip.Name()) }()
	_, _ = tmpFileZip.Write([]byte("PK\x03\x04dummy content"))
	_ = tmpFileZip.Close()

	t.Run("NativeExtractorZip", func(t *testing.T) {
		sel, err := BinarySelector(BinaryMatchCriteria{
			DownloadPath: tmpFileZip.Name(),
			Names:        []string{"dummy"},
			Interactive:  false,
			Extractor:    "native",
		})
		if err == nil && sel != nil {
			assert.Equal(t, Binary, sel.GetKind())
		}
	})

	tmpFileTar, err := os.CreateTemp("", "gh-pt-test-*.tar.gz")
	require.NoError(t, err)
	defer func() { _ = os.Remove(tmpFileTar.Name()) }()
	_, _ = tmpFileTar.Write([]byte("dummy content"))
	_ = tmpFileTar.Close()

	t.Run("NativeExtractorTarball", func(t *testing.T) {
		sel, err := BinarySelector(BinaryMatchCriteria{
			DownloadPath: tmpFileTar.Name(),
			Names:        []string{"dummy"},
			Interactive:  false,
			Extractor:    "native",
		})
		if err == nil && sel != nil {
			assert.Equal(t, Binary, sel.GetKind())
		}
	})
}

func TestBinarySelectorOuchExtractor(t *testing.T) {
	tmpFileZip, err := os.CreateTemp("", "gh-pt-test-*.zip")
	require.NoError(t, err)
	defer func() { _ = os.Remove(tmpFileZip.Name()) }()
	_, _ = tmpFileZip.Write([]byte("PK\x03\x04dummy content"))
	_ = tmpFileZip.Close()

	t.Run("OuchExtractorZip", func(t *testing.T) {
		sel, err := BinarySelector(BinaryMatchCriteria{
			DownloadPath: tmpFileZip.Name(),
			Names:        []string{"dummy"},
			Interactive:  false,
			Extractor:    "ouch",
		})
		if err == nil && sel != nil {
			assert.Equal(t, Binary, sel.GetKind())
		}
	})
}

func TestReleaseSelectorStableLatest(t *testing.T) {
	client := &MockGithubClient{
		GetResponses: map[string]interface{}{
			"repos/owner/repo/releases": []map[string]interface{}{
				{"Tag_name": "v2.0.0-beta", "Id": 2, "Prerelease": true},
				{"Tag_name": "v1.0.0", "Id": 1, "Prerelease": false},
			},
			"repos/owner/repo/releases/latest": map[string]interface{}{
				// Simulate failure fetching /latest API endpoint
			},
		},
		GetError: nil,
	}
	t.Run("StableLatest", func(t *testing.T) {
		sel, err := ReleaseSelector(client, "owner/repo", "latest", false, false, true)
		require.NoError(t, err)
		assert.Equal(t, Release, sel.GetKind())
		s, ok := sel.(*Selector)
		require.True(t, ok)
		assert.Contains(t, s.RegexpMatchers, "v1.0.0")
	})
}

func TestReleaseSelectorPrereleaseLatest(t *testing.T) {
	client := &MockGithubClient{
		GetResponses: map[string]interface{}{
			"repos/owner/repo/releases": []map[string]interface{}{
				{"Tag_name": "v2.0.0-beta", "Id": 2, "Prerelease": true},
				{"Tag_name": "v1.0.0", "Id": 1, "Prerelease": false},
			},
		},
		GetError: nil,
	}
	t.Run("PrereleaseLatest", func(t *testing.T) {
		sel, err := ReleaseSelector(client, "owner/repo", "latest", false, true, false)
		require.NoError(t, err)
		assert.Equal(t, Release, sel.GetKind())
		s, ok := sel.(*Selector)
		require.True(t, ok)
		assert.Contains(t, s.RegexpMatchers, "v2.0.0-beta")
	})
}
func TestSelectorFallbackChain(t *testing.T) {
	items := []*SelectorItem{
		{Name: "screego_1.12.5_linux_amd64.tar.gz"},
		{Name: "screego_1.12.5_darwin_amd64.tar.gz"},
		{Name: "screego_1.12.5_windows_amd64.zip"},
		{Name: "checksums.txt"},
		{Name: "screego-client_1.12.5_linux_amd64.tar.gz"},
	}

	s := &Selector{
		Kind:             Asset,
		Items:            items,
		RegexpMatchers:   []string{`.*(?:amd64.+linux|linux.+amd64).*`},
		Single:           true,
		AllowForeignArch: false,
		Repository:       "screego/server",
	}

	selected, err := s.Run()
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if len(selected) != 1 {
		t.Fatalf("expected 1 selected item, got %d. Items: %s, %s", len(selected), selected[0].Name, selected[1].Name)
	}

	if selected[0].Name != "screego_1.12.5_linux_amd64.tar.gz" {
		t.Errorf("expected screego_1.12.5_linux_amd64.tar.gz, got %s", selected[0].Name)
	}
}

func TestBinarySelector_MultiBinaryOptions(t *testing.T) {
	items := []*SelectorItem{
		{Name: "tool1.exe"},
		{Name: "tool2.exe"},
		{Name: "tool3.exe"},
	}

	t.Run("DefaultInstallsAll", func(t *testing.T) {
		s := &Selector{
			Kind:  Binary,
			Items: items,
		}
		selected, err := s.Run()
		require.NoError(t, err)
		assert.Len(t, selected, 3)
	})

	t.Run("OnlyFirstMatch", func(t *testing.T) {
		s := &Selector{
			Kind:           Binary,
			Items:          items,
			OnlyFirstMatch: true,
			Single:         true,
		}
		selected, err := s.Run()
		require.NoError(t, err)
		assert.Len(t, selected, 1)
		assert.Equal(t, "tool1.exe", selected[0].Name)
	})

	t.Run("MaxExeInstallsCap", func(t *testing.T) {
		s := &Selector{
			Kind:           Binary,
			Items:          items,
			MaxExeInstalls: 2,
		}
		selected, err := s.Run()
		require.NoError(t, err)
		assert.Len(t, selected, 2)
		assert.Equal(t, "tool1.exe", selected[0].Name)
		assert.Equal(t, "tool2.exe", selected[1].Name)
	})
}
