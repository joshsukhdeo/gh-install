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
