package cmd

import (
	"regexp"
	"runtime"
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
