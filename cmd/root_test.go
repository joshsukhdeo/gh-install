package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildRegexFromTypes_PrioritizationAndMusl(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Testing Linux regex rules")
	}

	types := []string{"deb", "snap", "flatpak", "appimage", "7z", "tar.gz", "zip", "none"}
	matchers := buildRegexFromTypes(types, false)

	// Sample assets from PowerShell/PowerShell
	assets := []string{
		"powershell-7.6.5-1.cm.aarch64.rpm",
		"powershell-7.6.5-1.cm.x86_64.rpm",
		"powershell-7.6.5-1.rh.x86_64.rpm",
		"powershell-7.6.5-linux-arm32.tar.gz",
		"powershell-7.6.5-linux-arm64.tar.gz",
		"powershell-7.6.5-linux-musl-x64.tar.gz",
		"powershell-7.6.5-linux-x64-fxdependent.tar.gz",
		"powershell-7.6.5-linux-x64-musl-noopt-fxdependent.tar.gz",
		"powershell-7.6.5-linux-x64.tar.gz",
		"powershell-7.6.5-osx-arm64.pkg",
		"PowerShell-7.6.5-win-x64.msi",
		"powershell-lts_7.6.5-1.deb_amd64.deb",
		"powershell-lts_7.6.5-1.deb_arm64.deb",
		"powershell_7.6.5-1.deb_amd64.deb",
		"powershell_7.6.5-1.deb_arm64.deb",
	}

	// Find the first matcher that matches any asset
	var firstMatchedAsset string
	var firstMatchedRegex string

	for _, rx := range matchers {
		for _, asset := range assets {
			matched, _ := regexp.MatchString(rx, asset)
			if matched {
				firstMatchedAsset = asset
				firstMatchedRegex = rx
				break
			}
		}
		if firstMatchedAsset != "" {
			break
		}
	}

	assert.NotEmpty(t, firstMatchedAsset)
	assert.Equal(t, "powershell-lts_7.6.5-1.deb_amd64.deb", firstMatchedAsset, "deb should be chosen first over tar.gz. Matched regex: %s", firstMatchedRegex)
}

func TestGetDefaultPaths_CloneAndFork(t *testing.T) {
	clonePath := GetDefaultClonePath()
	forkPath := GetDefaultForkPath()

	assert.NotEmpty(t, clonePath)
	assert.NotEmpty(t, forkPath)
	assert.Contains(t, clonePath, "src")
	assert.Contains(t, forkPath, "projects")
}

func TestResolveRepoPath(t *testing.T) {
	home, _ := os.UserHomeDir()

	p1 := resolveRepoPath("cli/cli", true, false, "", "")
	assert.Equal(t, filepath.Join(home, "src", "cli"), p1)

	p2 := resolveRepoPath("cli/cli", false, true, "", "")
	assert.Equal(t, filepath.Join(home, "projects", "cli"), p2)

	p3 := resolveRepoPath("cli/cli", true, false, "/custom/src", "")
	assert.Equal(t, filepath.Join("/custom/src", "cli"), p3)

	p4 := resolveRepoPath("cli/cli", false, true, "", "/custom/forks")
	assert.Equal(t, filepath.Join("/custom/forks", "cli"), p4)
}

func TestGetCompileScriptPath(t *testing.T) {
	scriptPath := getCompileScriptPath("neovim/neovim")
	if runtime.GOOS == "windows" {
		assert.True(t, strings.HasSuffix(scriptPath, "compile-neovim.ps1"))
	} else {
		assert.True(t, strings.HasSuffix(scriptPath, "compile-neovim.sh"))
	}
	assert.Contains(t, scriptPath, "scripts")
}

func TestBuildCompilePrompt(t *testing.T) {
	prompt := buildCompilePrompt("neovim/neovim", "/tmp/gh-compile-123", "/custom/script.sh", "/usr/local/bin")
	assert.Contains(t, prompt, "neovim/neovim")
	assert.Contains(t, prompt, "/tmp/gh-compile-123")
	assert.Contains(t, prompt, "/custom/script.sh")
	assert.Contains(t, prompt, "/usr/local/bin")
	assert.Contains(t, prompt, "test and then attempt to run the compile script and it is only done when script runs successfully")
}

func TestBuildCompileFixPrompt(t *testing.T) {
	prompt := buildCompileFixPrompt("neovim/neovim", "/tmp/gh-compile-123", "/custom/script.sh", "/usr/local/bin", "ninja: command not found", 1)
	assert.Contains(t, prompt, "neovim/neovim")
	assert.Contains(t, prompt, "/custom/script.sh")
	assert.Contains(t, prompt, "ninja: command not found")
	assert.Contains(t, prompt, "attempt 1 of 2")
	assert.Contains(t, prompt, "fix and then attempt to run the compile script and it is only done when script runs successfully")
}
