package params

import (
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLIFlags_CloneAndFork(t *testing.T) {
	var cli CLI
	parser, err := kong.New(&cli, kong.Vars{
		"install_types": "deb,rpm",
		"install_path":  "/test/path",
		"clone_path":    "~/src",
		"fork_path":     "~/projects",
	})
	require.NoError(t, err)

	_, err = parser.Parse([]string{"joshsukhdeo/gh-install", "--clone"})
	require.NoError(t, err)
	assert.True(t, cli.Clone)
	assert.False(t, cli.Fork)

	var cli2 CLI
	parser2, err := kong.New(&cli2, kong.Vars{
		"install_types": "deb,rpm",
		"install_path":  "/test/path",
		"clone_path":    "~/src",
		"fork_path":     "~/projects",
	})
	require.NoError(t, err)

	_, err = parser2.Parse([]string{"joshsukhdeo/gh-install", "--fork"})
	require.NoError(t, err)
	assert.True(t, cli2.Fork)
	assert.False(t, cli2.Clone)

	var cli3 CLI
	parser3, err := kong.New(&cli3, kong.Vars{
		"install_types": "deb,rpm",
		"install_path":  "/test/path",
		"clone_path":    "~/src",
		"fork_path":     "~/projects",
	})
	require.NoError(t, err)

	_, err = parser3.Parse([]string{"joshsukhdeo/gh-install", "--ai", "--compile-from-source", "--ai-cmd", "my-ai -p %s"})
	require.NoError(t, err)
	assert.True(t, cli3.AI)
	assert.True(t, cli3.CompileFromSource)
	assert.Equal(t, "my-ai -p %s", cli3.AICmd)
}

func TestCLIDefaults(t *testing.T) {
	var cli CLI
	parser, err := kong.New(&cli, kong.Vars{
		"install_types": "deb,rpm",
		"install_path":  "/test/path",
		"clone_path":    "~/src",
		"fork_path":     "~/projects",
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
		"clone_path":    "~/src",
		"fork_path":     "~/projects",
	})
	require.NoError(t, err)

	_, err = parser.Parse([]string{"joshsukhdeo/gh-install", "-i", "--update-all"})
	require.NoError(t, err)

	assert.Equal(t, "joshsukhdeo/gh-install", cli.Repository)
	assert.True(t, cli.Interactive)
	assert.True(t, cli.UpdateAll)
}
