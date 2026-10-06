package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripEmojis(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "REINSTALLED ~~> ✨UPGRADED✨ ( 32.0.0 )",
			expected: "REINSTALLED ~~> UPGRADED ( 32.0.0 )",
		},
		{
			input:    "REINSTALLED ~~> 🟰ZEROGRADE🟰 ( 32.0.0 )",
			expected: "REINSTALLED ~~> ZEROGRADE ( 32.0.0 )",
		},
		{
			input:    "⚠️REINSTALLED ~~> 💣DOWNGRADED💥⚠️ ( 32.0.0 )",
			expected: "REINSTALLED ~~> DOWNGRADED ( 32.0.0 )",
		},
		{
			input:    "--- 📦 VERSIONS 📦 ---",
			expected: "--- VERSIONS ---",
		},
		{
			input:    "--- 📂 ASSETS 📂 ---",
			expected: "--- ASSETS ---",
		},
		{
			input:    "--- 📝 DESCRIPTION 📝 ---",
			expected: "--- DESCRIPTION ---",
		},
		{
			input:    "--- 📖 README 📖 ---",
			expected: "--- README ---",
		},
		{
			input:    "📌🗻🧪🎯",
			expected: "",
		},
		{
			input:    "Clean text without emojis",
			expected: "Clean text without emojis",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, StripEmojis(tt.input))
		})
	}
}
