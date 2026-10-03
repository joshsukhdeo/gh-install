package status

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateStatusMessage_NewInstall(t *testing.T) {
	state := InstallState{
		InState:          false,
		AlreadyInstalled: false,
		PrevVersion:      "",
		NewVersion:       "33.0.0-beta6",
		AppName:          "obsproject/obs-studio",
		Type:             "deb",
		Repo:             "obsproject/obs-studio",
		AssetName:        "OBS-Studio-33.0.0-beta6-Ubuntu-26.04-x86_64.deb",
		ExtractedAssets:  []string{"obs-studio"},
	}

	msg, err := GenerateStatusMessage(state)
	require.NoError(t, err)
	expected := "INSTALLED ( -> 33.0.0-beta6 | obs-studio [deb] from obsproject/obs-studio )"
	assert.Equal(t, expected, msg)
}

func TestGenerateStatusMessage_Upgrade(t *testing.T) {
	state := InstallState{
		InState:          true,
		AlreadyInstalled: true,
		PrevVersion:      "32.0.0",
		NewVersion:       "33.0.0-beta6",
		AppName:          "obsproject/obs-studio",
		Type:             "deb",
		Repo:             "obsproject/obs-studio",
		AssetName:        "OBS-Studio-33.0.0-beta6-Ubuntu-26.04-x86_64.deb",
		ExtractedAssets:  []string{"obs-studio"},
	}

	msg, err := GenerateStatusMessage(state)
	require.NoError(t, err)
	expected := "REINSTALLED ~~> ✨UPGRADED✨ ( 32.0.0 -> 33.0.0-beta6 | obs-studio [deb] from obsproject/obs-studio )"
	assert.Equal(t, expected, msg)
}

func TestGenerateStatusMessage_ArchiveWithSidecars(t *testing.T) {
	state := InstallState{
		InState:          false,
		AlreadyInstalled: true,
		PrevVersion:      "v1.21.1",
		NewVersion:       "v1.21.1",
		AppName:          "bluenviron/mediamtx",
		Type:             "binary",
		Repo:             "bluenviron/mediamtx",
		ExtractedAssets:  []string{"mediamtx"},
		ArchiveType:      "tar.gz",
		ArchiveName:      "mediamtx_v1.21.1_linux_amd64.tar.gz",
		Sidecars:         []string{"mediamtx.yml"},
	}

	msg, err := GenerateStatusMessage(state)
	require.NoError(t, err)
	expected := "INSTALLED ( v1.21.1 -> v1.21.1 | mediamtx [tar.gz/binary] from bluenviron/mediamtx | mediamtx_v1.21.1_linux_amd64.tar.gz [sidecars: mediamtx.yml] )"
	assert.Equal(t, expected, msg)
}
