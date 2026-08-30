package selector

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"testing"
    "os"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type MockGithubClient struct {
	GetResponses map[string]interface{}
	GetError     error
	ReqResponses map[string]*http.Response
	ReqError     error
}

func (m *MockGithubClient) Get(path string, response interface{}) error {
	if m.GetError != nil {
		return m.GetError
	}
	if val, ok := m.GetResponses[path]; ok {
		bytes, _ := json.Marshal(val)
		json.Unmarshal(bytes, response)
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
        // Check items slice instead of map
        var names []string
        for _, it := range s.Items {
            names = append(names, it.Name)
        }
		assert.Contains(t, names, "v2.0.0")
	})

	t.Run("Interactive", func(t *testing.T) {
		sel, err := ReleaseSelector(client, "owner/repo", "latest", true)
		require.NoError(t, err)
		is, ok := sel.(*InteractiveSelector)
		require.True(t, ok)
        var names []string
        for _, it := range is.Items {
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
		sel, err := AssetSelector(client, "owner/repo", 1, "asset-linux", []string{".*linux.*"}, false)
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

	t.Run("Interactive", func(t *testing.T) {
		clientInteractive := &MockGithubClient{}
		clientInteractive.ReqResponses = map[string]*http.Response{
			"repos/owner/repo/releases/1/assets": {
				Body:   io.NopCloser(bytes.NewReader([]byte(respBody))),
				Header: http.Header{},
			},
		}
		sel, err := AssetSelector(clientInteractive, "owner/repo", 1, "", []string{}, true)
		require.NoError(t, err)
		is, ok := sel.(*InteractiveSelector)
		require.True(t, ok)
        var names []string
        for _, it := range is.Items {
            names = append(names, it.Name)
        }
		assert.Contains(t, names, "asset-linux-amd64.tar.gz")
	})
}

func TestBinarySelector(t *testing.T) {
	// Create a dummy plain text binary file for the no-match archiver path
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "dummy-binary")
	err := os.WriteFile(path, []byte("executable content"), 0755)
	require.NoError(t, err)

	t.Run("NonInteractive", func(t *testing.T) {
		sel, err := BinarySelector(path, []string{"dummy-binary"}, ".*", false, false)
		require.NoError(t, err)
		assert.Equal(t, Binary, sel.GetKind())
		s, ok := sel.(*Selector)
		require.True(t, ok)
        var names []string
        for _, it := range s.Items {
            names = append(names, it.Name)
        }
		assert.Contains(t, names, "dummy-binary")
	})

	t.Run("Interactive", func(t *testing.T) {
		sel, err := BinarySelector(path, []string{"dummy-binary"}, ".*", true, false)
		require.NoError(t, err)
		is, ok := sel.(*InteractiveSelector)
		require.True(t, ok)
        var names []string
        for _, it := range is.Items {
            names = append(names, it.Name)
        }
		assert.Contains(t, names, "dummy-binary")
	})
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
	item := &SelectorItem{
		Name: "myitem",
		Selected: false,
		Id: 1,
		Compressed: true,
		BinaryType: BinaryExecutable,
		DownloadPath: "/tmp/dwn",
		FsPath: "sub/file",
	}

	assert.Equal(t, 1, item.Id)
	assert.True(t, item.Compressed)
	assert.Equal(t, BinaryExecutable, item.BinaryType)
	assert.Equal(t, "/tmp/dwn", item.DownloadPath)
	assert.Equal(t, "sub/file", item.FsPath)

	// BinaryTypeFromPath
	assert.Equal(t, BinaryExecutable, BinaryTypeFromPath("/tmp/test.exe"))
	assert.Equal(t, BinaryExecutable, BinaryTypeFromPath("/tmp/test"))
	assert.Equal(t, BinaryDebInstaller, BinaryTypeFromPath("/tmp/test.deb"))
	assert.Equal(t, BinaryRpmInstaller, BinaryTypeFromPath("/tmp/test.rpm"))
	assert.Equal(t, BinaryPkgInstaller, BinaryTypeFromPath("/tmp/test.pkg"))
	assert.Equal(t, BinaryPkgInstaller, BinaryTypeFromPath("/tmp/test.txz"))
}
