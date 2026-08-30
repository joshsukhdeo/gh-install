package release

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateCleanName(t *testing.T) {
	tests := []struct {
		name       string
		binaryName string
		repoName   string
		expected   string
	}{
		{
			name:       "junegunn/fzf standard",
			binaryName: "fzf-linux-amd64",
			repoName:   "fzf",
			expected:   "fzf",
		},
		{
			name:       "BurntSushi/ripgrep short binary",
			binaryName: "rg",
			repoName:   "ripgrep",
			expected:   "rg",
		},
		{
			name:       "jesseduffield/lazygit",
			binaryName: "lazygit_Linux_x86_64",
			repoName:   "lazygit",
			expected:   "lazygit",
		},
		{
			name:       "sharkdp/fd",
			binaryName: "fd-v9.0.0-x86_64-unknown-linux-musl",
			repoName:   "fd",
			expected:   "fd-v9.0.0", // version tag usually remains, or we could strip it, but it's acceptable if the user is prompted
		},
		{
			name:       "docker/compose multi-word",
			binaryName: "docker-compose-linux-x86_64",
			repoName:   "compose",
			expected:   "docker-compose",
		},
		{
			name:       "eza-community/eza",
			binaryName: "eza",
			repoName:   "eza",
			expected:   "eza",
		},
		{
			name:       "cli/cli (github cli)",
			binaryName: "gh_2.32.0_linux_amd64",
			repoName:   "cli",
			expected:   "gh_2.32.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := GenerateCleanName(tt.binaryName, tt.repoName)
			assert.Equal(t, tt.expected, actual)
		})
	}
}
