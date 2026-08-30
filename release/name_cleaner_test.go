package release

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateCleanName(t *testing.T) {
	tests := []struct {
		name            string
		binaryName      string
		repoName        string
		resolvedVersion string
		expected        string
	}{
		{
			name:            "junegunn/fzf standard",
			binaryName:      "fzf-linux-amd64",
			repoName:        "fzf",
			resolvedVersion: "0.74.3",
			expected:        "fzf",
		},
		{
			name:            "BurntSushi/ripgrep short binary",
			binaryName:      "rg",
			repoName:        "ripgrep",
			resolvedVersion: "13.0.0",
			expected:        "rg",
		},
		{
			name:            "jesseduffield/lazygit",
			binaryName:      "lazygit_Linux_x86_64",
			repoName:        "lazygit",
			resolvedVersion: "v0.35.0",
			expected:        "lazygit",
		},
		{
			name:            "sharkdp/fd",
			binaryName:      "fd-v9.0.0-x86_64-unknown-linux-musl",
			repoName:        "fd",
			resolvedVersion: "v9.0.0",
			expected:        "fd", // version tag should now be explicitly stripped
		},
		{
			name:            "docker/compose multi-word",
			binaryName:      "docker-compose-linux-x86_64",
			repoName:        "compose",
			resolvedVersion: "v2.10.2",
			expected:        "docker-compose",
		},
		{
			name:            "eza-community/eza",
			binaryName:      "eza",
			repoName:        "eza",
			resolvedVersion: "v0.10.6",
			expected:        "eza",
		},
		{
			name:            "cli/cli (github cli)",
			binaryName:      "gh_2.32.0_linux_amd64",
			repoName:        "cli",
			resolvedVersion: "v2.32.0", // Even though it's 'v' in the release, the binary uses no 'v'
			expected:        "gh",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := GenerateCleanName(tt.binaryName, tt.repoName, tt.resolvedVersion)
			assert.Equal(t, tt.expected, actual)
		})
	}
}
