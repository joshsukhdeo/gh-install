package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPeekRemoteTar(t *testing.T) {
	// Create a test tar.gz with known entries
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	entries := []struct {
		name string
		data string
	}{
		{"bin/mytool", "fake binary content"},
		{"lib/libfoo.so", "fake lib content"},
		{"share/doc/readme.txt", "docs"},
		{"bin/helper", "helper binary"},
		{"bin/another", "another binary"},
	}

	for _, e := range entries {
		hdr := &tar.Header{
			Name: e.name,
			Mode: 0755,
			Size: int64(len(e.data)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.data)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()

	t.Logf("Test archive size: %d bytes", buf.Len())

	// Create test server with range support
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		t.Logf("Request #%d: %s %s, Range: %s", requestCount, r.Method, r.URL.Path, r.Header.Get("Range"))
		if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
			var start, end int64
			if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end); err != nil {
				http.Error(w, "Invalid range", http.StatusBadRequest)
				return
			}
			if end < start || end >= int64(len(buf.Bytes())) {
				end = int64(len(buf.Bytes())) - 1
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(buf.Bytes())))
			w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start+1))
			w.WriteHeader(http.StatusPartialContent)
			n, _ := w.Write(buf.Bytes()[start : end+1])
			t.Logf("Server range response: bytes %d-%d (%d bytes)", start, end, n)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(buf.Bytes())))
		w.Write(buf.Bytes())
	}))
	defer server.Close()

	// Test PeekRemoteTar
	ctx := context.Background()
	client := &http.Client{Timeout: 30 * time.Second}

	cfg := &PeekRemoteTarConfig{
		MaxBytesToRead: 512 * 1024,
		MaxEntries:     100,
	}

	testURL := server.URL + "/test.tar.gz"
	t.Logf("Calling PeekRemoteTar with URL: %s, size: %d", testURL, int64(len(buf.Bytes())))
	items, err := PeekRemoteTar(ctx, testURL, int64(len(buf.Bytes())), client, "", cfg)
	if err != nil {
		t.Fatalf("PeekRemoteTar error: %v", err)
	}

	t.Logf("Total requests made: %d", requestCount)

	if len(items) == 0 {
		t.Fatal("Expected items, got none")
	}

	found := map[string]bool{}
	for _, item := range items {
		found[item.Name] = true
		t.Logf("Found: %s (type: %v)", item.Name, item.BinaryType)
	}

	// Verify expected binaries in allowed directories
	if !found["mytool"] {
		t.Error("Expected to find 'mytool' in bin/")
	}
	if !found["helper"] {
		t.Error("Expected to find 'helper' in bin/")
	}
	if !found["another"] {
		t.Error("Expected to find 'another' in bin/")
	}
	// libfoo.so IS found (lib/ is in allowed dirs) - this is correct behavior
	if !found["libfoo.so"] {
		t.Error("Expected to find 'libfoo.so' in lib/ (allowed)")
	}
	// readme.txt should NOT be found (share/doc/ not in allowed dirs)
	if found["readme.txt"] {
		t.Error("Should not find readme.txt (share/doc/ not in allowed dirs)")
	}
}
