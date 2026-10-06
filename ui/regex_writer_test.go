package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegexTruncatingWriter(t *testing.T) {
	t.Run("short regex not truncated", func(t *testing.T) {
		var buf bytes.Buffer
		writer := &RegexTruncatingWriter{Writer: &buf, MaxLen: 200}

		input := `DEBU installing with values regexp="^[a-z0-9_-]+$" target=/usr/local/bin`
		n, err := writer.Write([]byte(input))
		require.NoError(t, err)
		assert.Equal(t, len(input), n)
		assert.Equal(t, input, buf.String())
	})

	t.Run("long regex truncated to 200 chars plus ellipsis", func(t *testing.T) {
		var buf bytes.Buffer
		writer := &RegexTruncatingWriter{Writer: &buf, MaxLen: 200}

		longRegex := strings.Repeat("x", 250)
		input := `DEBU installing with values release asset regexp="` + longRegex + `" target=/usr/local/bin`
		_, err := writer.Write([]byte(input))
		require.NoError(t, err)

		output := buf.String()
		assert.NotContains(t, output, longRegex)
		assert.Contains(t, output, strings.Repeat("x", 200)+"...")
		assert.Contains(t, output, "target=/usr/local/bin")
	})

	t.Run("JSON format truncated", func(t *testing.T) {
		var buf bytes.Buffer
		writer := &RegexTruncatingWriter{Writer: &buf, MaxLen: 200}

		longRegex := strings.Repeat("y", 300)
		input := `{"level":"debug","release asset regexp": "` + longRegex + `","target":"/usr/local/bin"}`
		_, err := writer.Write([]byte(input))
		require.NoError(t, err)

		output := buf.String()
		assert.NotContains(t, output, longRegex)
		assert.Contains(t, output, strings.Repeat("y", 200)+"...")
	})
}
