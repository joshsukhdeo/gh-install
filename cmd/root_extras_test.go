package cmd

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

func TestGetDefaultPaths(t *testing.T) {
	assert.NotEmpty(t, GetDefaultTargetPath())
	assert.NotEmpty(t, GetDefaultClonePath())
	assert.NotEmpty(t, GetDefaultForkPath())
	assert.NotEmpty(t, GetDefaultInstallTypes())
}

func TestGetEnvPrefix(t *testing.T) {
	assert.Equal(t, "GH_INSTALL", GetEnvPrefix())
}

