package release

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestVerifyHashWithVirusTotal_Unknown_SkipSandbox(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintln(w, `{"error":{"code": "NotFoundError"}}`)
	}))
	defer server.Close()
	vtBaseURL = server.URL

	err := verifyHashWithVirusTotal("unknownhash", "dummy.txt", "test-api-key", false, true)
	assert.NoError(t, err)
}

func TestVerifyHashWithVirusTotal_Unknown_Upload(t *testing.T) {
	// Create a dummy file
	f, _ := os.CreateTemp("", "vt-test")
	f.WriteString("dummy payload")
	f.Close()
	defer os.Remove(f.Name())

	vtPollDelay = 10 * time.Millisecond
	reqCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		if reqCount == 1 {
			// First request is to /files/hash -> 404
			w.WriteHeader(http.StatusNotFound)
		} else if reqCount == 2 {
			// Second request is POST to /files -> returns analysis ID
			assert.Equal(t, http.MethodPost, r.Method)
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, `{"data":{"id": "analysis-123"}}`)
		} else if reqCount == 3 {
			// Third request is GET to /analyses/analysis-123 -> return completed
			assert.Equal(t, http.MethodGet, r.Method)
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, `{"data":{"attributes":{"status": "completed", "stats":{"malicious": 1}}}}`)
		}
	}))
	defer server.Close()
	vtBaseURL = server.URL

	err := verifyHashWithVirusTotal("unknownhash", f.Name(), "test-api-key", false, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "malicious")
}

func TestVerifyHashWithVirusTotal_Unknown_UploadLarge(t *testing.T) {
	vtPollDelay = 10 * time.Millisecond
	f, _ := os.CreateTemp("", "vt-test-large")
	// Make it larger than 32MB so it triggers the large upload logic
	f.Truncate(33 * 1024 * 1024)
	f.Close()
	defer os.Remove(f.Name())

	reqCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		if reqCount == 1 {
			// First request is to /files/hash -> 404
			assert.Equal(t, http.MethodGet, r.Method)
			w.WriteHeader(http.StatusNotFound)
		} else if reqCount == 2 {
			// Second request should be to /files/upload_url
			assert.Equal(t, "/files/upload_url", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			// Returning the mock server URL to simulate the dynamic upload URL
			fmt.Fprintf(w, "{\"data\": \"http://%s/upload_target\"}\n", r.Host)
		} else if reqCount == 3 {
			// Third request is POST to /upload_target
			assert.Equal(t, "/upload_target", r.URL.Path)
			assert.Equal(t, http.MethodPost, r.Method)
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, `{"data":{"id": "analysis-large"}}`)
		} else if reqCount == 4 {
			// Fourth request is GET to /analyses/analysis-large
			assert.Equal(t, "/analyses/analysis-large", r.URL.Path)
			assert.Equal(t, http.MethodGet, r.Method)
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, `{"data":{"attributes":{"status": "completed", "stats":{"malicious": 0}}}}`)
		}
	}))
	defer server.Close()
	vtBaseURL = server.URL

	err := verifyHashWithVirusTotal("unknownhash_large", f.Name(), "test-api-key", false, false)
	assert.NoError(t, err)
}
