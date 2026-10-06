package params

import (
	"strings"
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
		"extractor":     "default",
	})
	require.NoError(t, err)

	_, err = parser.Parse([]string{"repo", "clone", "joshsukhdeo/gh-pt"})
	require.NoError(t, err)
	assert.Equal(t, "joshsukhdeo/gh-pt", cli.Repo.Clone.Repository)

	var cli2 CLI
	parser2, err := kong.New(&cli2, kong.Vars{
		"install_types": "deb,rpm",
		"install_path":  "/test/path",
		"clone_path":    "~/src",
		"fork_path":     "~/projects",
		"extractor":     "default",
	})
	require.NoError(t, err)

	_, err = parser2.Parse([]string{"repo", "fork", "joshsukhdeo/gh-pt"})
	require.NoError(t, err)
	assert.Equal(t, "joshsukhdeo/gh-pt", cli2.Repo.Fork.Repository)

	var cli3 CLI
	parser3, err := kong.New(&cli3, kong.Vars{
		"install_types": "deb,rpm",
		"install_path":  "/test/path",
		"clone_path":    "~/src",
		"fork_path":     "~/projects",
		"extractor":     "default",
	})
	require.NoError(t, err)

	_, err = parser3.Parse([]string{"source", "joshsukhdeo/gh-pt", "--ai", "--ai-cmd", "my-ai -p %s"})
	require.NoError(t, err)
	assert.Equal(t, "joshsukhdeo/gh-pt", cli3.Source.Repository)
	assert.True(t, cli3.Source.AI)
	assert.Equal(t, "my-ai -p %s", cli3.Source.AICmd)
}

func TestCLIDefaults(t *testing.T) {
	var cli CLI
	parser, err := kong.New(&cli, kong.Vars{
		"install_types": "deb,rpm",
		"install_path":  "/test/path",
		"clone_path":    "~/src",
		"fork_path":     "~/projects",
		"extractor":     "default",
	})
	require.NoError(t, err)

	_, err = parser.Parse([]string{})
	require.NoError(t, err)

	assert.False(t, cli.Install.Interactive)

	assert.Equal(t, "latest", cli.Install.ReleaseVersion)
	assert.False(t, cli.Install.All)
	assert.True(t, cli.Install.TargetPathCreate)
	assert.False(t, cli.Install.Overwrite)
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
		"extractor":     "default",
	})
	require.NoError(t, err)

	_, err = parser.Parse([]string{"joshsukhdeo/gh-pt", "-i", "--all"})
	require.NoError(t, err)

	assert.Equal(t, "joshsukhdeo/gh-pt", cli.Install.Repository)
	assert.True(t, cli.Install.Interactive)
}

func TestTruncateRegex(t *testing.T) {
	t.Run("short regex not truncated", func(t *testing.T) {
		input := "^[a-z0-9_-]+$"
		assert.Equal(t, input, TruncateRegex(input))
	})

	t.Run("exactly 200 chars not truncated", func(t *testing.T) {
		input := ""
		for i := 0; i < 200; i++ {
			input += "a"
		}
		assert.Equal(t, 200, len(input))
		assert.Equal(t, input, TruncateRegex(input))
	})

	t.Run("longer than 200 chars truncated to 200 chars with ellipsis", func(t *testing.T) {
		input := ""
		for i := 0; i < 250; i++ {
			input += "b"
		}
		truncated := TruncateRegex(input)
		assert.Equal(t, 203, len(truncated))
		assert.True(t, strings.HasPrefix(truncated, input[:200]))
		assert.True(t, strings.HasSuffix(truncated, "..."))
	})

	t.Run("custom maxLen limit", func(t *testing.T) {
		input := "1234567890"
		truncated := TruncateRegex(input, 5)
		assert.Equal(t, "12345...", truncated)
	})
}
