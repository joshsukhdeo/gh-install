package release

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrg/xdg"
	"github.com/joshsukhdeo/gh-pt/params"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectVulkanManifests(t *testing.T) {
	tempData := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tempData)
	xdg.Reload()

	pkgDir := t.TempDir()
	libPath := filepath.Join(pkgDir, "libvulkan_intel.so")
	require.NoError(t, os.WriteFile(libPath, []byte("dummy-so"), 0755))

	jsonContent := `{
		"file_format_version": "1.0.0",
		"ICD": {
			"library_path": "./libvulkan_intel.so",
			"api_version": "1.3.268"
		}
	}`
	jsonPath := filepath.Join(pkgDir, "intel_icd.json")
	require.NoError(t, os.WriteFile(jsonPath, []byte(jsonContent), 0644))

	targets, err := DetectDriverTargets("intel", "compute-runtime", pkgDir, "vulkan", false)
	require.NoError(t, err)
	require.Len(t, targets, 1)

	assert.Equal(t, DriverVulkan, targets[0].Subsystem)
	assert.Contains(t, targets[0].DestPath, "vulkan/icd.d")
	assert.Contains(t, targets[0].DestPath, "gh-pt-intel-compute-runtime-intel_icd.json")

	var manifest map[string]interface{}
	require.NoError(t, json.Unmarshal(targets[0].Content, &manifest))
	icd, ok := manifest["ICD"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, libPath, icd["library_path"])
}

func TestDetectOpenCLManifests(t *testing.T) {
	tempConfig := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempConfig)
	xdg.Reload()

	pkgDir := t.TempDir()
	libDir := filepath.Join(pkgDir, "lib")
	require.NoError(t, os.MkdirAll(libDir, 0755))
	libPath := filepath.Join(libDir, "libigdrcl.so")
	require.NoError(t, os.WriteFile(libPath, []byte("dummy-opencl-so"), 0755))

	targets, err := DetectDriverTargets("intel", "compute-runtime", pkgDir, "opencl", false)
	require.NoError(t, err)
	require.Len(t, targets, 1)

	assert.Equal(t, DriverOpenCL, targets[0].Subsystem)
	assert.Contains(t, targets[0].DestPath, "OpenCL/vendors")
	assert.Contains(t, targets[0].DestPath, "gh-pt-intel-compute-runtime.icd")
	assert.Equal(t, libPath+"\n", string(targets[0].Content))
}

func TestDetectLdsoTargets(t *testing.T) {
	testSys := t.TempDir()
	t.Setenv("GH_PT_TEST_SYSDIR", testSys)

	pkgDir := t.TempDir()
	libDir := filepath.Join(pkgDir, "lib")
	require.NoError(t, os.MkdirAll(libDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(libDir, "libigc.so"), []byte("dummy-igc"), 0755))

	targets, err := DetectDriverTargets("intel", "intel-graphics-compiler", pkgDir, "ldso", false)
	require.NoError(t, err)
	require.Len(t, targets, 1)

	assert.Equal(t, DriverLdSo, targets[0].Subsystem)
	assert.Equal(t, "ldconfig", targets[0].PostHook)
	assert.Contains(t, targets[0].DestPath, "etc/ld.so.conf.d/gh-pt-intel-intel-graphics-compiler.conf")
	assert.Contains(t, string(targets[0].Content), libDir)
}

func TestDetectUdevTargets(t *testing.T) {
	testSys := t.TempDir()
	t.Setenv("GH_PT_TEST_SYSDIR", testSys)

	pkgDir := t.TempDir()
	ruleContent := `SUBSYSTEM=="accel", ATTR{device}=="0x7d1d", MODE="0666"`
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "99-intel-npu.rules"), []byte(ruleContent), 0644))

	targets, err := DetectDriverTargets("intel", "linux-npu-driver", pkgDir, "udev", false)
	require.NoError(t, err)
	require.Len(t, targets, 1)

	assert.Equal(t, DriverUdev, targets[0].Subsystem)
	assert.Equal(t, "udevadm", targets[0].PostHook)
	assert.Contains(t, targets[0].DestPath, "etc/udev/rules.d/99-gh-pt-intel-linux-npu-driver-99-intel-npu.rules")
	assert.Equal(t, ruleContent, string(targets[0].Content))
}

func TestDetectPluginTargets_OBS_GIMP_Audio(t *testing.T) {
	tempConfig := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempConfig)
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	xdg.Reload()

	pkgDir := t.TempDir()
	obsDir := filepath.Join(pkgDir, "obs-plugins", "64bit")
	require.NoError(t, os.MkdirAll(obsDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(obsDir, "obs-openvino.so"), []byte("dummy"), 0755))

	gimpDir := filepath.Join(pkgDir, "plug-ins")
	require.NoError(t, os.MkdirAll(gimpDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(gimpDir, "openvino-plugin.py"), []byte("dummy"), 0755))

	vstDir := filepath.Join(pkgDir, "synth.vst3")
	require.NoError(t, os.MkdirAll(vstDir, 0755))

	clapFile := filepath.Join(pkgDir, "delay.clap")
	require.NoError(t, os.WriteFile(clapFile, []byte("dummy"), 0755))

	// Test OBS
	obsTargets, err := DetectPluginTargets("intel", "openvino-plugins-for-obs-studio", pkgDir, "obs", false)
	require.NoError(t, err)
	require.Len(t, obsTargets, 1)
	assert.Equal(t, PluginOBS, obsTargets[0].Subsystem)
	assert.Contains(t, obsTargets[0].DestPath, "obs-studio/plugins/openvino-plugins-for-obs-studio")

	// Test GIMP
	gimpTargets, err := DetectPluginTargets("intel", "openvino-ai-plugins-gimp", pkgDir, "gimp", false)
	require.NoError(t, err)
	require.Len(t, gimpTargets, 1)
	assert.Equal(t, PluginGIMP, gimpTargets[0].Subsystem)
	assert.Contains(t, gimpTargets[0].DestPath, "GIMP/2.10/plug-ins/openvino-ai-plugins-gimp")

	// Test Audio
	audioTargets, err := DetectPluginTargets("vendor", "my-plugins", pkgDir, "audio", false)
	require.NoError(t, err)
	require.Len(t, audioTargets, 2) // VST3 and CLAP
}

func TestDeployDriverAndPluginManifests(t *testing.T) {
	tempData := t.TempDir()
	tempConfig := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tempData)
	t.Setenv("XDG_CONFIG_HOME", tempConfig)
	xdg.Reload()

	pkgDir := t.TempDir()
	libPath := filepath.Join(pkgDir, "libvulkan_intel.so")
	require.NoError(t, os.WriteFile(libPath, []byte("dummy-so"), 0755))
	jsonContent := `{"ICD": {"library_path": "libvulkan_intel.so"}}`
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "icd.json"), []byte(jsonContent), 0644))

	r := &GithubRelease{
		CliParams: &params.ExecContext{
			Repository: "intel/compute-runtime",
			CommonInstallFlags: params.CommonInstallFlags{
				SidecarFlags: params.SidecarFlags{
					Driver: "vulkan",
				},
			},
		},
	}

	installed, err := r.DeployDriverAndPluginManifests(pkgDir)
	require.NoError(t, err)
	require.Len(t, installed, 1)
	assert.FileExists(t, installed[0])

	// Verify file content has absolute path
	content, err := os.ReadFile(installed[0])
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(content), libPath))
}
