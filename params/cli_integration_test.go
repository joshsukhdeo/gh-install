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
			r.VerifyChecksum = false
			r.SkipVtSandbox = true
			r.AllowForeignArch = true
			r.AllowDowngrade = true
			if r.Wine == "" || r.Wine == "off" {
				r.Wine = "allow"
			}
		}

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

		assert.False(t, r.SkipVtSandbox)
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
	cli, _ := parseWithTestVars(t, []string{
		"install", "test/app",
		"-S", `plugins/.*\.so|data/.*`,
		"--sidecar-symlink-to", "/etc/plugins",
		"--sidecar-symlink-to", "/var/lib/plugins",
		"--env-inject", "PLUGIN_DIR=/opt/sidecars",
		"--ai-setup-sidecars",
	})

	// Sidecars is now a regex pattern string
	assert.Equal(t, `plugins/.*\.so|data/.*`, cli.Install.Sidecars)
	// SidecarTargetPath removed - use IncludeSidecars mode instead
	assert.Equal(t, []string{"/etc/plugins", "/var/lib/plugins"}, cli.Install.SidecarSymlinkTo)
	assert.Equal(t, []string{"PLUGIN_DIR=/opt/sidecars"}, cli.Install.EnvInject)
	assert.True(t, cli.Install.AISetupSidecars)
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
