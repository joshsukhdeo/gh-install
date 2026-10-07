package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/joshsukhdeo/gh-pt/params"
	"github.com/joshsukhdeo/gh-pt/state"
	"github.com/joshsukhdeo/gh-pt/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRegexFromTypes_PrioritizationAndMusl(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Testing Linux regex rules")
	}

	types := []string{"deb", "snap", "flatpak", "appimage", "7z", "tar.gz", "zip", "none"}
	matchers := buildRegexFromTypes(types, "off")

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
	assert.Equal(t, filepath.Join(home, "src", "repos", "cli"), p1)

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
	prompt := buildCompilePrompt("neovim/neovim", "/tmp/gh-compile-123", "/custom/script.sh", "/usr/local/bin", "")
	assert.Contains(t, prompt, "neovim/neovim")
	assert.Contains(t, prompt, "/tmp/gh-compile-123")
	assert.Contains(t, prompt, "/custom/script.sh")
	assert.Contains(t, prompt, "/usr/local/bin")
	assert.Contains(t, prompt, "test and then attempt to run the compile script and it is only done when script runs successfully")

	symlinkPrompt := buildCompilePrompt("neovim/neovim", "/tmp/gh-compile-123", "/custom/script.sh", "/usr/local/bin", "/home/user/src/apps/neovim/neovim")
	assert.Contains(t, symlinkPrompt, "/home/user/src/apps/neovim/neovim")
	assert.Contains(t, symlinkPrompt, "/usr/local/bin")
	assert.Contains(t, symlinkPrompt, "symlink")
}

func TestBuildCompileFixPrompt(t *testing.T) {
	prompt := buildCompileFixPrompt("neovim/neovim", "/tmp/gh-compile-123", "/custom/script.sh", "/usr/local/bin", "", "ninja: command not found", 1)
	assert.Contains(t, prompt, "neovim/neovim")
	assert.Contains(t, prompt, "/custom/script.sh")
	assert.Contains(t, prompt, "ninja: command not found")
	assert.Contains(t, prompt, "attempt 1 of 2")
	assert.Contains(t, prompt, "fix and then attempt to run the compile script and it is only done when script runs successfully")

	symlinkFix := buildCompileFixPrompt("neovim/neovim", "/tmp/gh-compile-123", "/custom/script.sh", "/usr/local/bin", "/home/user/src/apps/neovim/neovim", "error", 1)
	assert.Contains(t, symlinkFix, "/home/user/src/apps/neovim/neovim")
	assert.Contains(t, symlinkFix, "symlink")
}

func TestBuildCloneOrForkArgs(t *testing.T) {
	// Clone without depth
	args := buildCloneOrForkArgs("cli/cli", false, "/path/to/target", 0)
	assert.Equal(t, []string{"repo", "clone", "cli/cli", "/path/to/target"}, args)

	// Clone with depth
	args = buildCloneOrForkArgs("cli/cli", false, "/path/to/target", 1)
	assert.Equal(t, []string{"repo", "clone", "cli/cli", "/path/to/target", "--", "--depth", "1"}, args)

	// Fork without depth
	args = buildCloneOrForkArgs("cli/cli", true, "/path/to/target", 0)
	assert.Equal(t, []string{"repo", "fork", "cli/cli", "--clone", "/path/to/target"}, args)

	// Fork with depth
	args = buildCloneOrForkArgs("cli/cli", true, "/path/to/target", 5)
	assert.Equal(t, []string{"repo", "fork", "cli/cli", "--clone", "/path/to/target", "--", "--depth", "5"}, args)
}

func TestRouter_MaxDepth(t *testing.T) {
	cliClone := params.CLI{}
	cliClone.Repo.Clone.Repository = "joshsukhdeo/gh-pt"
	cliClone.Repo.Clone.MaxDepth = 3
	cliClone.Install.DryRun = true

	err := error(nil)
	assert.NoError(t, err)

	cliFork := params.CLI{}
	cliFork.Repo.Fork.Repository = "joshsukhdeo/gh-pt"
	cliFork.Repo.Fork.MaxDepth = 4
	cliFork.Install.DryRun = true

	err = error(nil)
	assert.NoError(t, err)
}

func TestInstalledApp_MaxDepthSerialization(t *testing.T) {
	app := state.InstalledApp{
		Repository: "joshsukhdeo/gh-pt",
		MaxDepth:   2,
	}
	data, err := json.Marshal(app)
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"max_depth":2`)

	var loaded state.InstalledApp
	err = json.Unmarshal(data, &loaded)
	assert.NoError(t, err)
	assert.Equal(t, 2, loaded.MaxDepth)
}

func TestGetSourcePaths(t *testing.T) {
	manifest, compile := getSourcePaths("obsproject/obs-studio")
	assert.True(t, strings.HasSuffix(manifest, "manifest-obsproject-obs-studio.json"))
	assert.True(t, strings.HasSuffix(compile, "compile-obsproject-obs-studio.sh"))
	assert.Contains(t, manifest, filepath.Join("gh-pt", "source"))
	assert.Contains(t, compile, filepath.Join("gh-pt", "source"))
}

func TestInstallDirWatcherAndSymlinking(t *testing.T) {
	tmpDir := t.TempDir()
	stagedDir := filepath.Join(tmpDir, ".ghpt")
	// simulate .ghpt staging structure:
	// stagedPath: <tmpDir>/.ghpt/bin/myapp
	// activePath: <tmpDir>/bin/myapp

	require.NoError(t, os.MkdirAll(filepath.Join(stagedDir, "bin"), 0755))
	before, err := snapshotDirFiles(stagedDir)
	require.NoError(t, err)
	assert.Empty(t, before)

	// Create a new file in staged dir
	stagedFile := filepath.Join(stagedDir, "bin", "myapp")
	require.NoError(t, os.WriteFile(stagedFile, []byte("#!/bin/sh\necho ok"), 0755))

	// Diff reveals new file
	newFiles, err := diffDirFiles(stagedDir, before)
	require.NoError(t, err)
	require.Len(t, newFiles, 1)
	assert.Equal(t, stagedFile, newFiles[0])

	// Process symlinks
	r := &RootCLI{}
	recorded := r.processNewFilesAndSymlinks(newFiles)
	require.Len(t, recorded, 2)
	assert.Equal(t, stagedFile, recorded[0])

	expectedActive := strings.Replace(stagedFile, ".ghpt/", "", 1)
	assert.Equal(t, expectedActive, recorded[1])
	assert.FileExists(t, expectedActive)

	// Verify it is a symlink pointing to stagedFile
	fi, err := os.Lstat(expectedActive)
	require.NoError(t, err)
	assert.True(t, fi.Mode()&os.ModeSymlink != 0)
	target, err := os.Readlink(expectedActive)
	require.NoError(t, err)
	assert.Equal(t, stagedFile, target)

	// Test moving when no --symlink
	// Staged file should overwrite symlink
	_ = os.Remove(expectedActive)
	err = moveFile(stagedFile, expectedActive)
	require.NoError(t, err)
	assert.FileExists(t, expectedActive)
	fiAfter, err := os.Lstat(expectedActive)
	require.NoError(t, err)
	assert.False(t, fiAfter.Mode()&os.ModeSymlink != 0, "should now be regular file, not symlink")
	assert.NoFileExists(t, stagedFile)
}

func TestRootCLI_Verbose_TerminalLoggingAndRegexTruncation(t *testing.T) {
	cli := &params.CLI{
		Verbose: true,
	}
	r := &RootCLI{}
	r.Verbose = cli.Verbose
	if r.Verbose {
		r.ProgressBar = "none"
	}
	r.ensureCliParams()

	assert.True(t, r.Verbose)
	assert.Equal(t, "none", r.ProgressBar)

	// Verify RegexTruncatingWriter truncates long regex in log events
	var buf bytes.Buffer
	writer := &ui.RegexTruncatingWriter{Writer: &buf, MaxLen: 200}

	longRegex := strings.Repeat("z", 250)
	testLog := `DEBU installing with values repository=cli/cli release asset regexp="` + longRegex + `" target=/usr/local/bin`
	_, err := writer.Write([]byte(testLog))
	require.NoError(t, err)

	out := buf.String()
	assert.NotContains(t, out, longRegex)
	assert.Contains(t, out, strings.Repeat("z", 200)+"...")
	assert.Contains(t, out, "target=/usr/local/bin")
}

func TestRootCLI_Validate_WinePlatformAndAvailabilityCheck(t *testing.T) {
	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository: "owner/repo",
			Wine:       "allow",
		},
	}
	_ = r.Validate()
	if runtime.GOOS == "windows" {
		assert.Equal(t, "off", r.Wine, "Wine must be disabled on Windows")
	}
}
