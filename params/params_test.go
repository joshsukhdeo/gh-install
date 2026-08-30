package params

import (
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLIDefaults(t *testing.T) {
	var cli CLI
	parser, err := kong.New(&cli, kong.Vars{
		"install_types": "deb,rpm",
		"install_path":  "/test/path",
	})
	require.NoError(t, err)

	_, err = parser.Parse([]string{})
	require.NoError(t, err)

	assert.False(t, cli.Interactive)
	assert.False(t, cli.UpdateAll)
	assert.False(t, cli.Update)
	assert.Equal(t, "latest", cli.ReleaseVersion)
	assert.False(t, cli.All)
	assert.True(t, cli.TargetPathCreate)
	assert.False(t, cli.Overwrite)
	assert.Equal(t, "info", cli.LogLevel)
	assert.Equal(t, "console", cli.LogFormat)
	assert.True(t, cli.LogQuietInteractive)
}

func TestCLIParse(t *testing.T) {
	var cli CLI
	parser, err := kong.New(&cli, kong.Vars{
		"install_types": "deb,rpm",
		"install_path":  "/test/path",
	})
	require.NoError(t, err)

	_, err = parser.Parse([]string{"joshsukhdeo/gh-install", "-i", "--update-all"})
	require.NoError(t, err)

	assert.Equal(t, "joshsukhdeo/gh-install", cli.Repository)
	assert.True(t, cli.Interactive)
	assert.True(t, cli.UpdateAll)
}
