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
	}

	msg, err := GenerateStatusMessage(state)
	require.NoError(t, err)
	expected := "INSTALLED ( -> 33.0.0-beta6 obsproject/obs-studio [deb] from obsproject/obs-studio [ OBS-Studio-33.0.0-beta6-Ubuntu-26.04-x86_64.deb ] )"
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
	}

	msg, err := GenerateStatusMessage(state)
	require.NoError(t, err)
	expected := "REINSTALLED ~~> ✨UPGRADED✨ ( 32.0.0 -> 33.0.0-beta6 obsproject/obs-studio [deb] from obsproject/obs-studio [ OBS-Studio-33.0.0-beta6-Ubuntu-26.04-x86_64.deb ] )"
	assert.Equal(t, expected, msg)
}
