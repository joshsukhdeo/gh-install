package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/posener/complete"
	"github.com/stretchr/testify/assert"
)

func TestPredictAcceptedOptions(t *testing.T) {
	// PredictExtractors
	exts := PredictExtractors.Predict(complete.Args{})
	assert.Contains(t, exts, "default")
	assert.Contains(t, exts, "ouch")
	assert.Contains(t, exts, "native")
	assert.Contains(t, exts, "internal")

	// PredictProgressBars
	pb := PredictProgressBars.Predict(complete.Args{})
	assert.Contains(t, pb, "pacman")
	assert.Contains(t, pb, "standard")
	assert.Contains(t, pb, "conveyor")
	assert.Contains(t, pb, "none")
	assert.Contains(t, pb, "spinner:dots")

	// PredictSidecarModes
	sm := PredictSidecarModes.Predict(complete.Args{})
	assert.Contains(t, sm, "auto")
	assert.Contains(t, sm, "xdg_data_home")
	assert.Contains(t, sm, "local-map")
	assert.Contains(t, sm, "bin")
	assert.Contains(t, sm, "none")
}

func TestPredictConfigKeys(t *testing.T) {
	keys := PredictConfigKeys.Predict(complete.Args{})
	assert.Contains(t, keys, "extractor")
	assert.Contains(t, keys, "progress_bar")
	assert.Contains(t, keys, "fork_path")
}

func TestPredictGithubRepos_MockGH(t *testing.T) {
	tmpDir := t.TempDir()
	fakeBinDir := filepath.Join(tmpDir, "bin")
	assert.NoError(t, os.MkdirAll(fakeBinDir, 0755))

	fakeGhScript := `#!/usr/bin/env bash
if [ "$1" = "api" ]; then
    if [ "$4" = "search/users" ]; then
        echo "openvinotoolkit"
        exit 0
    fi
    if [ "$4" = "search/repositories" ]; then
        if [[ "$*" == *"model_s"* ]]; then
            echo "openvinotoolkit/model_server"
            exit 0
        fi
        echo "openvinotoolkit/openvino"
        echo "openvinotoolkit/model_server"
        exit 0
    fi
fi
exit 1
`
	fakeGhPath := filepath.Join(fakeBinDir, "gh")
	assert.NoError(t, os.WriteFile(fakeGhPath, []byte(fakeGhScript), 0755))

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", fakeBinDir+":"+origPath)

	// 1. Owner completion: openvinotoolk -> openvinotoolkit
	resOwner := PredictGithubRepos.Predict(complete.Args{Last: "openvinotoolk"})
	assert.Contains(t, resOwner, "openvinotoolkit")

	// 2. Repos completion for openvinotoolkit/
	resRepos := PredictGithubRepos.Predict(complete.Args{Last: "openvinotoolkit/"})
	assert.Contains(t, resRepos, "openvinotoolkit/openvino")
	assert.Contains(t, resRepos, "openvinotoolkit/model_server")

	// 3. Repos completion with prefix: openvinotoolkit/model_s -> openvinotoolkit/model_server
	resPrefixed := PredictGithubRepos.Predict(complete.Args{Last: "openvinotoolkit/model_s"})
	assert.Contains(t, resPrefixed, "openvinotoolkit/model_server")
}
