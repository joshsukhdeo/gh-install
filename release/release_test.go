package release

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

func TestGetScore(t *testing.T) {
	types := []string{"tar.gz", "zip", "deb"}
	assert.Equal(t, 30, getScore("app.tar.gz", types))
	assert.Equal(t, 20, getScore("app.zip", types))
	assert.Equal(t, 10, getScore("app.deb", types))
	assert.Equal(t, -1, getScore("app.rpm", types))
}

func TestGenerateStrictAssetRegex(t *testing.T) {
    assert.Equal(t, "^app\\.tar\\.gz$", generateStrictAssetRegex("app.tar.gz", ""))
    assert.Contains(t, generateStrictAssetRegex("app-v1.0.0.tar.gz", "v1.0.0"), ".*")
}
