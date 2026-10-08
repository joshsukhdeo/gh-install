package params_test

import (
	"encoding/json"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/joshsukhdeo/gh-pt/cmd"
	"github.com/joshsukhdeo/gh-pt/params"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseWithTestVars(t *testing.T, args []string) (*params.CLI, *kong.Kong) {
	t.Helper()
	var cli params.CLI
	parser, err := kong.New(&cli,
		kong.Vars{
			"install_types": "deb,rpm,appimage,tar.gz",
			"install_path":  "/default/bin",
			"clone_path":    "~/src",
			"fork_path":     "~/projects",
			"extractor":     "default",
			"version":       "2.0.0",
		},
	)
	require.NoError(t, err)
	kCtx, err := parser.Parse(args)
	require.NoError(t, err)
	_ = kCtx
	return &cli, parser
}

func TestCLI_TestFlagSerialization(t *testing.T) {
	cli, _ := parseWithTestVars(t, []string{"install", "cli/cli", "--test", "--global", "-p", "/custom/bin"})
	assert.True(t, cli.Test)
	assert.Equal(t, "cli/cli", cli.Install.Repository)
	assert.True(t, cli.Install.Global)
	assert.Equal(t, "/custom/bin", cli.Install.TargetPath)

	data, err := json.Marshal(cli)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"Repository":"cli/cli"`)
	assert.Contains(t, string(data), `"Global":true`)
}

func TestCLI_SpecialtyFlagCascades(t *testing.T) {
	t.Run("Barbarous Cascade", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{"install", "test/app", "--BARBAROUS"})
		r := &cmd.RootCLI{ExecContext: params.ExecContext{CommonInstallFlags: cli.Install.CommonInstallFlags, Repository: cli.Install.Repository}}
		assert.True(t, r.Barbarous)

		// Dry-run run install validation to trigger cascade
		r.VerifyChecksum = true
		r.Wine = "off"
		if r.Barbarous {
			r.InsecureAllowUnsigned = true
			r.VerifyChecksum = false
			r.SkipVtSandbox = true
			r.AllowForeignArch = true
			r.AllowDowngrade = true
			if r.Wine == "" || r.Wine == "off" {
				r.Wine = "allow"
			}
		}

		assert.True(t, r.InsecureAllowUnsigned)
		assert.False(t, r.VerifyChecksum)
		assert.True(t, r.SkipVtSandbox)
		assert.True(t, r.AllowForeignArch)
		assert.True(t, r.AllowDowngrade)
		assert.Equal(t, "allow", r.Wine)
	})

	t.Run("LeRetrogrouch Cascade", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{"install", "test/app", "--LE-RETROGROUCH", "--pin-install"})
		r := &cmd.RootCLI{ExecContext: params.ExecContext{CommonInstallFlags: cli.Install.CommonInstallFlags, Repository: cli.Install.Repository}}
		assert.True(t, r.LeRetrogrouch)

		if r.LeRetrogrouch {
			r.AllowDowngrade = true
			r.PinInstall = false
		}

		// Default is now true (opt-in), so SkipVtSandbox remains true unless explicitly disabled
		assert.True(t, r.SkipVtSandbox)
		assert.True(t, r.AllowDowngrade)
		assert.False(t, r.PinInstall)
	})

	t.Run("RetrogradeStopgap Cascade", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{"install", "test/app", "--retrograde-stopgap"})
		r := &cmd.RootCLI{ExecContext: params.ExecContext{CommonInstallFlags: cli.Install.CommonInstallFlags, Repository: cli.Install.Repository}}
		assert.True(t, r.RetrogradeStopgap)

		if r.RetrogradeStopgap {
			r.AllowDowngrade = true
			r.PinInstall = true
		}

		assert.True(t, r.AllowDowngrade)
		assert.True(t, r.PinInstall)
	})

	t.Run("SelfInflictedDebt Cascade", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{"install", "test/app", "--self-inflicted-technical-debt"})
		r := &cmd.RootCLI{ExecContext: params.ExecContext{CommonInstallFlags: cli.Install.CommonInstallFlags, Repository: cli.Install.Repository}}
		assert.True(t, r.SelfInflictedDebt)

		if r.SelfInflictedDebt {
			r.AllowDowngrade = true
		}

		assert.True(t, r.AllowDowngrade)
	})
}

func TestCLI_SidecarFlags(t *testing.T) {
	t.Run("All Sidecar Flags Explicit", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{
			"install", "test/app",
			"-S", `plugins/.*\.so|data/.*`,
			"--sidecar-symlink-to", "/etc/plugins",
			"--sidecar-symlink-to", "/var/lib/plugins",
			"--include-sidecars",
			"--sidecar-mode", "custom-path:/my/sidecars",
			"--env-inject", "PLUGIN_DIR=/opt/sidecars",
			"--ai-setup-sidecars",
		})

		// Sidecars is now a regex pattern string
		assert.Equal(t, `plugins/.*\.so|data/.*`, cli.Install.Sidecars)
		assert.Equal(t, []string{"/etc/plugins", "/var/lib/plugins"}, cli.Install.SidecarSymlinkTo)
		assert.True(t, cli.Install.IncludeSidecars)
		assert.Equal(t, "custom-path:/my/sidecars", cli.Install.SidecarMode)
		assert.Equal(t, []string{"PLUGIN_DIR=/opt/sidecars"}, cli.Install.EnvInject)
		assert.True(t, cli.Install.AISetupSidecars)
	})

	t.Run("SidecarMode Auto-Enables IncludeSidecars", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{
			"install", "test/app",
			"--sidecar-mode=bin",
		})
		assert.Equal(t, "bin", cli.Install.SidecarMode)
		assert.True(t, cli.Install.IncludeSidecars, "specifying --sidecar-mode=bin should auto-enable IncludeSidecars")
	})

	t.Run("SidecarMode None Does Not Enable IncludeSidecars", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{
			"install", "test/app",
			"--sidecar-mode=none",
		})
		assert.Equal(t, "none", cli.Install.SidecarMode)
		assert.False(t, cli.Install.IncludeSidecars, "specifying --sidecar-mode=none should not auto-enable IncludeSidecars")
	})

	t.Run("SidecarMode Default Does Not Enable IncludeSidecars", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{
			"install", "test/app",
			"--sidecar-mode=default",
		})
		assert.Equal(t, "default", cli.Install.SidecarMode)
		assert.False(t, cli.Install.IncludeSidecars, "specifying --sidecar-mode=default should not auto-enable IncludeSidecars")
	})

	t.Run("SidecarMode With Explicit NoIncludeSidecars", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{
			"install", "test/app",
			"--sidecar-mode=bin",
			"--no-include-sidecars",
		})
		assert.Equal(t, "bin", cli.Install.SidecarMode)
		assert.False(t, cli.Install.IncludeSidecars, "explicit --no-include-sidecars should override auto-enabling")
	})

	t.Run("SidecarMode With Explicit IncludeSidecars False", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{
			"install", "test/app",
			"--sidecar-mode=bin",
			"--include-sidecars=false",
		})
		assert.Equal(t, "bin", cli.Install.SidecarMode)
		assert.False(t, cli.Install.IncludeSidecars, "explicit --include-sidecars=false should override auto-enabling")
	})

	t.Run("ToExecContext Resolution", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{
			"install", "test/app",
			"--sidecar-mode=bin",
		})
		execCtx := cli.ToExecContext()
		assert.Equal(t, "test/app", execCtx.Repository)
		assert.Equal(t, "bin", execCtx.SidecarMode)
		assert.True(t, execCtx.IncludeSidecars)

		// Manual struct without parsing
		manualCmd := params.InstallCmd{
			Repository: "owner/repo",
			CommonInstallFlags: params.CommonInstallFlags{
				SidecarFlags: params.SidecarFlags{
					SidecarMode: "same_dest",
				},
			},
		}
		manualExecCtx := manualCmd.ToExecContext()
		assert.Equal(t, "owner/repo", manualExecCtx.Repository)
		assert.Equal(t, "same_dest", manualExecCtx.SidecarMode)
		assert.True(t, manualExecCtx.IncludeSidecars)
	})
}

func TestCLI_Indicators(t *testing.T) {
	// Standard emoji outputs
	assert.Equal(t, "📌🎯", cmd.GetStateIndicator(true, true, false, false, false, false))
	assert.Equal(t, "📌🗻🎯", cmd.GetStateIndicator(true, true, false, false, true, false))
	assert.Equal(t, "🧪🎯", cmd.GetStateIndicator(true, false, true, false, false, false))

	// Text fallback outputs when disabled
	assert.Equal(t, "^@", cmd.GetStateIndicator(true, true, false, false, false, true))
	assert.Equal(t, "^*@", cmd.GetStateIndicator(true, true, false, false, true, true))
	assert.Equal(t, "!@", cmd.GetStateIndicator(true, false, true, false, false, true))
}

func TestCLI_NoColor_NoEmojis(t *testing.T) {
	t.Run("Install Flags", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{"install", "cli/cli", "--no-color", "--no-emojis"})
		assert.True(t, cli.Install.NoColor)
		assert.True(t, cli.Install.NoEmojis)
	})

	t.Run("Install Flag Aliases", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{"install", "cli/cli", "--no-colors", "--no-emoji"})
		assert.True(t, cli.Install.NoColor)
		assert.True(t, cli.Install.NoEmojis)
	})

	t.Run("Upgrade Flags", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{"upgrade", "cli/cli", "--no-color", "--no-emojis"})
		assert.True(t, cli.Upgrade.NoColor)
		assert.True(t, cli.Upgrade.NoEmojis)
	})

	t.Run("Show Flags", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{"show", "cli/cli", "--no-color", "--no-emojis"})
		assert.True(t, cli.Show.NoColor)
		assert.True(t, cli.Show.NoEmojis)
	})

	t.Run("Ls and Ll Flags", func(t *testing.T) {
		cliLs, _ := parseWithTestVars(t, []string{"ls", "--no-color", "--no-emojis"})
		assert.True(t, cliLs.Ls.NoColor)
		assert.True(t, cliLs.Ls.NoEmojis)

		cliLl, _ := parseWithTestVars(t, []string{"ll", "--no-color", "--no-emojis"})
		assert.True(t, cliLl.Ll.NoColor)
		assert.True(t, cliLl.Ll.NoEmojis)
	})
}

func TestCLI_InsecureAllowUnsignedFlags(t *testing.T) {
	t.Run("Install Flag", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{"install", "cli/cli", "--insecure-allow-unsigned"})
		assert.True(t, cli.Install.InsecureAllowUnsigned)
	})

	t.Run("Install Flag Alias skip-checksums", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{"install", "cli/cli", "--skip-checksums"})
		assert.True(t, cli.Install.InsecureAllowUnsigned)
	})

	t.Run("Upgrade Flag", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{"upgrade", "cli/cli", "--insecure-allow-unsigned"})
		assert.True(t, cli.Upgrade.InsecureAllowUnsigned)
	})

	t.Run("Upgrade Flag Alias skip-checksums", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{"upgrade", "cli/cli", "--skip-checksums"})
		assert.True(t, cli.Upgrade.InsecureAllowUnsigned)
	})
}

func TestCLI_DriverAndPluginFlags(t *testing.T) {
	t.Run("Driver flag auto-enables sidecars", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{"install", "intel/compute-runtime", "--driver=opencl"})
		assert.Equal(t, "opencl", cli.Install.Driver)
		assert.True(t, cli.Install.IncludeSidecars)

		execCtx := cli.ToExecContext()
		assert.Equal(t, "opencl", execCtx.Driver)
		assert.True(t, execCtx.IncludeSidecars)
	})

	t.Run("Plugin flag auto-enables sidecars", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{"install", "intel/openvino-plugins-for-obs-studio", "--plugin=obs"})
		assert.Equal(t, "obs", cli.Install.Plugin)
		assert.True(t, cli.Install.IncludeSidecars)

		execCtx := cli.ToExecContext()
		assert.Equal(t, "obs", execCtx.Plugin)
		assert.True(t, execCtx.IncludeSidecars)
	})

	t.Run("Plugins alias flag works", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{"install", "vendor/audio-pack", "--plugins=vst"})
		assert.Equal(t, "vst", cli.Install.Plugin)
		assert.True(t, cli.Install.IncludeSidecars)
	})

	t.Run("Driver none does not auto-enable sidecars", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{"install", "some/app", "--driver=none"})
		assert.Equal(t, "none", cli.Install.Driver)
		assert.False(t, cli.Install.IncludeSidecars)
	})

	t.Run("Explicit --no-include-sidecars overrides auto-enable", func(t *testing.T) {
		cli, _ := parseWithTestVars(t, []string{"install", "intel/compute-runtime", "--driver=vulkan", "--no-include-sidecars"})
		assert.Equal(t, "vulkan", cli.Install.Driver)
		assert.False(t, cli.Install.IncludeSidecars)
	})
}
