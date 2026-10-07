package release

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ulikunitz/xz"
)

func createTestTarGz(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0755,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		require.NoError(t, tw.WriteHeader(hdr))
		_, err := tw.Write([]byte(content))
		require.NoError(t, err)
	}

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	return buf.Bytes()
}

func createTestTarXz(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	xzw, err := xz.NewWriter(&buf)
	require.NoError(t, err)
	tw := tar.NewWriter(xzw)

	for name, content := range files {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0755,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		require.NoError(t, tw.WriteHeader(hdr))
		_, err := tw.Write([]byte(content))
		require.NoError(t, err)
	}

	require.NoError(t, tw.Close())
	require.NoError(t, xzw.Close())
	return buf.Bytes()
}

func createTestZip(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for name, content := range files {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(content))
		require.NoError(t, err)
	}

	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func TestStreamTarReader_GzAndXz(t *testing.T) {
	files := map[string]string{
		"bin/myapp":        "#!/bin/sh\necho hello",
		"README.md":        "# Docs",
		"share/doc/doc.1":  "man page",
		"bin/myapp-helper": "#!/bin/sh\necho helper",
	}

	// 1. Test tar.gz
	gzBytes := createTestTarGz(t, files)
	items, err := StreamTarReader(bytes.NewReader(gzBytes), "app-linux-amd64.tar.gz")
	require.NoError(t, err)
	require.NotEmpty(t, items)

	names := make(map[string]bool)
	for _, it := range items {
		names[it.Name] = true
	}
	assert.True(t, names["myapp"])
	assert.True(t, names["myapp-helper"])

	// 2. Test tar.xz
	xzBytes := createTestTarXz(t, files)
	xzItems, err := StreamTarReader(bytes.NewReader(xzBytes), "app-linux-amd64.tar.xz")
	require.NoError(t, err)
	require.NotEmpty(t, xzItems)

	xzNames := make(map[string]bool)
	for _, it := range xzItems {
		xzNames[it.Name] = true
	}
	assert.True(t, xzNames["myapp"])
	assert.True(t, xzNames["myapp-helper"])
}

func TestPeekRemoteZip_RangeServer(t *testing.T) {
	zipData := createTestZip(t, map[string]string{
		"bin/mytool": "binary data",
		"LICENSE":    "MIT License",
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		totalSize := len(zipData)

		if rangeHeader == "" {
			w.Header().Set("Content-Length", strconv.Itoa(totalSize))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(zipData)
			return
		}

		// Parse Range: bytes=start-end
		var start, end int
		if strings.HasPrefix(rangeHeader, "bytes=") {
			parts := strings.Split(strings.TrimPrefix(rangeHeader, "bytes="), "-")
			start, _ = strconv.Atoi(parts[0])
			if len(parts) > 1 && parts[1] != "" {
				end, _ = strconv.Atoi(parts[1])
			} else {
				end = totalSize - 1
			}
		}
		if end >= totalSize {
			end = totalSize - 1
		}

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, totalSize))
		w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(zipData[start : end+1])
	}))
	defer server.Close()

	ctx := context.Background()
	items, err := PeekRemoteZip(ctx, server.URL, int64(len(zipData)), server.Client(), "")
	require.NoError(t, err)
	require.NotEmpty(t, items)

	assert.Equal(t, "mytool", items[0].Name)
	assert.True(t, items[0].Compressed)
}

func TestPeekRemoteZip_RealGitHubRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}
	url := "https://github.com/cli/cli/releases/download/v2.45.0/gh_2.45.0_windows_amd64.zip"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	items, err := PeekRemoteZip(ctx, url, 0, http.DefaultClient, "")
	if err != nil {
		t.Skipf("network test skipped: %v", err)
	}
	require.NotEmpty(t, items)
	var foundGh bool
	for _, it := range items {
		if it.Name == "gh.exe" {
			foundGh = true
			break
		}
	}
	assert.True(t, foundGh, "gh.exe should be found in remote zip without downloading the full archive")
}
