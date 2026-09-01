package release

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"github.com/stretchr/testify/assert"
)

func TestVerifyHashWithVirusTotal_Malicious(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-api-key", r.Header.Get("x-apikey"))
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"data":{"attributes":{"last_analysis_stats":{"malicious": 3}}}}`)
	}))
	defer server.Close()

	// Override the base URL for testing (we'll implement this variable)
	vtBaseURL = server.URL

	err := verifyHashWithVirusTotal("badhash", "test-api-key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "malicious")
}

func TestVerifyHashWithVirusTotal_Clean(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"data":{"attributes":{"last_analysis_stats":{"malicious": 0}}}}`)
	}))
	defer server.Close()
	vtBaseURL = server.URL

	err := verifyHashWithVirusTotal("goodhash", "test-api-key")
	assert.NoError(t, err)
}

func TestVerifyHashWithVirusTotal_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintln(w, `{"error":{"code": "NotFoundError"}}`)
	}))
	defer server.Close()
	vtBaseURL = server.URL

	err := verifyHashWithVirusTotal("unknownhash", "test-api-key")
	assert.NoError(t, err) // By default, we shouldn't block zero-day releases if they just aren't found
}
