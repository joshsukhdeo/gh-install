package params

import (
	"os"
	"os/exec"
	"strings"

	"github.com/alecthomas/kong"
)

// ProgressBarMode represents the progress bar display mode.
type ProgressBarMode string

const (
	ProgressBarPacman   ProgressBarMode = "pacman"
	ProgressBarStandard ProgressBarMode = "standard"
	ProgressBarNone     ProgressBarMode = "none"
	ProgressBarConveyor ProgressBarMode = "conveyor"
)

// Valid spinner styles
var validSpinnerStyles = map[string]bool{
	"dots":    true,
	"line":    true,
	"jump":    true,
	"pulse":   true,
	"points":  true,
	"miniDot": true,
	"step":    true,
}

// IsValid checks if the progress bar mode is valid.
func (m ProgressBarMode) IsValid() bool {
	// Check base modes
	switch m {
	case ProgressBarPacman, ProgressBarStandard, ProgressBarNone, ProgressBarConveyor:
		return true
	}

	// Check spinner variants
	if strings.HasPrefix(string(m), "spinner:") {
		style := strings.TrimPrefix(string(m), "spinner:")
		return validSpinnerStyles[style]
	}

	return false
}

type CLI struct {
	// Subcommands
	Install     InstallCmd     `cmd:"" default:"withargs" help:"Install a GitHub release or clone a repository (default)."`
	Ls          StateLsCmd     `cmd:"" aliases:"list" help:"List saved state (short format)."`
	Ll          StateLlCmd     `cmd:"" help:"List saved state (long format)."`
	Rm          StateRmCmd     `cmd:"" help:"Uninstall an application and remove it from state."`
	Upgrade     UpgradeCmd     `cmd:"" help:"Update installations."`
	State       StateCmd       `cmd:"" help:"Manage saved state and installations."`
	Config      ConfigCmd      `cmd:"" help:"Manage configuration."`
	Repo        RepoCmd        `cmd:"" help:"Manage source repositories."`
	Scan        ScanCmd        `cmd:"" help:"Security and AI scanning."`
	Vt          VtCmd          `cmd:"" help:"VirusTotal integration."`
	Show        Show           `cmd:"" help:"Show release information."`
	Hook        Hook           `cmd:"" help:"Configure repository lifecycle hooks."`
	Source      SourceCmd      `cmd:"" help:"Compile repository from source."`
	SourceClean SourceCleanCmd `cmd:"source-clean" help:"Clean source build artifacts."`
	Completions CompletionsCmd `cmd:"" help:"Generate shell completions."`
	Search      SearchCmd      `cmd:"" help:"Search for repositories on GitHub."`
	Helper      HelperCmd      `cmd:"" help:"AI-safe helper functions."`
	Test        bool           `help:"Hidden flag for testing to dump parsed parameters." hidden:""`

	// Global flags
	LogLevel            string           `default:"info" enum:"error,warn,info,debug" short:"l" help:"Log level."`
	LogFormat           string           `default:"console" enum:"console,json" help:"Log output format."`
	LogQuietInteractive bool             `default:"true" negatable:"" help:"Quiet log in interactive mode"`
	Verbose             bool             `short:"V" help:"Enable verbose output (sets log level to debug)."`
	Version             kong.VersionFlag `help:"Show version." env:""`
}

type SourceCleanCmd struct {
	Repository string `arg:"" optional:"" predictor:"github_repos" predict:"github_repos" help:"Repository to clean (owner/repo). If omitted, cleans all builds."`
	All        bool   `help:"Clean all build artifacts."`
}

// CliParams is an alias for CLI.
type CliParams = CLI

// ResolutionFlags controls version, asset matching, and selection rules.
type ResolutionFlags struct {
	ReleaseVersion                                string   `default:"latest" short:"v" help:"Repository release tag (version) to install."`
	ReleaseAsset                                  string   `optional:"" short:"a" help:"Name of repository release asset to download."`
	ReleaseAssetRegexp                            string   `optional:"" short:"A" help:"Regular expression matching release asset to download."`
	ReleaseAssetRegexps                           []string `kong:"-"`
	Type                                          []string `default:"${install_types}" short:"T" name:"format" env:"GH_PT_TYPE" help:"Comma-separated list of types to match and prioritize."`
	All                                           bool     `default:"false" help:"Install all matched assets instead of just the first one."`
	OnlyFirstMatch                                bool     `name:"only-first-match" help:"Only install the first matched binary instead of all unique binaries."`
	MaxExeInstalls                                int      `name:"max-exe-installs" default:"0" help:"Maximum number of executables to install (0 = all unique)."`
	Prerelease                                    bool     `short:"P" help:"Include prereleases."`
	Stable                                        bool     `help:"Include only stable releases."`
	FallbackReleases                              int      `default:"0" help:"Try this many older releases if no assets found in latest (0=disabled, max 3)."`
	SearchForInstallInstructionsIfNoReleaseAssets bool     `env:"GH_PT_README_FALLBACK" help:"Extract alternative installation instructions from README if release asset matching fails."`
	AvxLevel                                      string   `default:"auto" enum:"auto,none,avx,avx2,avx512" env:"GH_PT_AVX_LEVEL" help:"AVX instruction set preference: auto, none, avx, avx2, avx512."`
}

// TargetFlags controls binary extraction destinations and symlink behavior.
type TargetFlags struct {
	TargetPath       string            `default:"${install_path}" short:"p" type:"path" help:"Target installation directory (default: ~/.local/bin or /usr/local/bin if --global)."`
	Global           bool              `short:"g" help:"Install globally (e.g. /usr/local/bin) instead of user bin."`
	Rename           map[string]string `optional:"" short:"t" help:"Rename binaries installed at target path."`
	KeepSuffixes     bool              `short:"k" help:"Keep OS/hardware suffixes on extracted binaries."`
	TargetPathCreate bool              `default:"true" negatable:"" help:"Create target installation directory if it does not exist."`
	Overwrite        bool              `default:"false" short:"f" name:"force" aliases:"overwrite" env:"GH_PT_FORCE" help:"Overwrite target binaries."`
	Symlink          bool              `env:"GH_PT_SYMLINK" help:"Extract entire release to package store and symlink executables."`
}

// DependencyFlags controls automatic and prompted package dependency resolution.
type DependencyFlags struct {
	ResolveDeps bool `short:"y" name:"resolve-deps" help:"Automatically resolve and install dependencies without prompting."`
	PromptDeps  bool `name:"prompt-deps" help:"Prompt before installing dependencies."`
	NoDeps      bool `short:"n" help:"Do not install dependencies."`
}

// VerificationFlags controls cryptographic, hash, and security sandbox verifications.
type VerificationFlags struct {
	VerifyChecksum        bool `default:"true" help:"Verify asset checksums."`
	InsecureAllowUnsigned bool `name:"insecure-allow-unsigned" aliases:"skip-checksums" env:"GH_PT_INSECURE_ALLOW_UNSIGNED" help:"Allow unsigned release assets without cryptographic verification."`
	SkipVtSandbox         bool `default:"true" name:"skip-vt-sandbox" help:"Skip VirusTotal sandbox scan (default: true, use --skip-vt-sandbox=false to enable)."`
	AI                    bool `help:"Use AI to scan and analyze releases."`
}

// SidecarFlags controls non-binary companion assets, plugins, and runtime data.
type SidecarFlags struct {
	Sidecars           string   `optional:"" name:"sidecars" short:"S" help:"Regex pattern for sidecar assets to capture (default: \\\\.so.*|\\\\.h.*|\\\\.pak|\\\\.bin|\\\\.red)."`
	SidecarSymlinkTo   []string `optional:"" help:"Create symlinks from sidecars to these directories (can be specified multiple times)."`
	IncludeSidecars    bool     `name:"include-sidecars" short:"s" negatable:"" env:"GH_PT_INCLUDE_SIDECARS" help:"Include companion sidecar assets (auto-detects placement)."`
	SidecarMode        string   `optional:"" name:"sidecar-mode" env:"GH_PT_SIDECAR_MODE" help:"Sidecar placement mode: auto (maps standard Unix layout to prefix, else xdg_data_home), local-map, xdg_data_home, or custom-path:/path/to/ (default: auto)."`
	Driver             string   `optional:"" name:"driver" enum:"auto,vulkan,opencl,vaapi,udev,ldso,none" default:"none" help:"Register hardware/userspace driver manifests: auto, vulkan, opencl, vaapi, udev, ldso (Linux only)."`
	Plugin             string   `optional:"" name:"plugin" aliases:"plugins" enum:"auto,obs,gimp,vst,lv2,clap,audio,none" default:"none" help:"Register host application plugin manifests or symlinks: auto, obs, gimp, vst, lv2, clap, audio (Linux only)."`
	EnvInject          []string `optional:"" help:"Environment variables pointing to sidecar directory (KEY=VALUE)."`
	WarnUnmappedAssets bool     `default:"true" negatable:"" help:"Warn about suspected unmapped sidecar assets."`
	AISetupSidecars    bool     `help:"Use AI to analyze sidecars and generate post-install setup commands."`
}

// AfterApply is a kong hook that resolves sidecar settings after CLI parsing.
func (s *SidecarFlags) AfterApply(ctx *kong.Context) error {
	s.ResolveSidecars(ctx)
	return nil
}

// ResolveSidecars auto-enables IncludeSidecars if SidecarMode, Driver, or Plugin is explicitly provided,
// non-empty, and != "none" or "default", unless IncludeSidecars was explicitly set to false.
func (s *SidecarFlags) ResolveSidecars(ctx ...*kong.Context) {
	mode := strings.ToLower(strings.TrimSpace(s.SidecarMode))
	driver := strings.ToLower(strings.TrimSpace(s.Driver))
	plugin := strings.ToLower(strings.TrimSpace(s.Plugin))

	explicitlyProvided := (mode != "" && mode != "none" && mode != "default") ||
		(driver != "" && driver != "none") ||
		(plugin != "" && plugin != "none")

	if explicitlyProvided {
		explicitlyDisabled := false
		if len(ctx) > 0 && ctx[0] != nil {
			for _, f := range ctx[0].Flags() {
				if f.Name == "include-sidecars" && f.Value != nil && f.Set && !s.IncludeSidecars {
					explicitlyDisabled = true
					break
				}
			}
		}
		if !explicitlyDisabled {
			s.IncludeSidecars = true
		}
	}
	if s.IncludeSidecars && (s.SidecarMode == "" || s.SidecarMode == "default") {
		s.SidecarMode = "auto"
	}
}

// ExecutionFlags controls interactive runtime behavior and compatibility switches.
type ExecutionFlags struct {
	Interactive          bool     `default:"false" short:"i" help:"Use interactive installation."`
	DisablePrompts       bool     `short:"D" env:"GH_PT_DISABLE_PROMPTS" help:"Disable all interactive prompts."`
	NoSaveState          bool     `env:"GH_PT_NO_SAVE_STATE" help:"Do not save installation to state."`
	Wine                 string   `default:"off" enum:"force,priority,allow,off" env:"GH_PT_WINE" help:"Wine mode."`
	AllowForeignArch     bool     `env:"GH_PT_ALLOW_FOREIGN_ARCH" help:"Allow installing assets with foreign architectures."`
	AllowRootUserInstall bool     `help:"Allow installation to user-local paths when running as root."`
	ForceRoot            bool     `name:"force-root" env:"GH_PT_FORCE_ROOT" help:"Allow operations when running as root (suppresses root restriction)."`
	NoCompileContainer   bool     `name:"no-compile-container" env:"GH_PT_NO_COMPILE_CONTAINER" help:"Opt out of mandatory container isolation for AI source compilation (UNSAFE)."`
	Extractor            string   `env:"GH_PT_EXTRACTOR" help:"Archive extractor precedence (default, ouch, native, internal)." default:"${extractor}"`
	AllowDowngrade       bool     `help:"Allow downgrades when updating or installing."`
	SelfInflictedDebt    bool     `name:"self-inflicted-technical-debt" help:"Allow downgrades (alias for allow-downgrade)."`
	LeRetrogrouch        bool     `name:"LE-RETROGROUCH" help:"Exclusively downgrade and save unpinned with Le_RetroGrouch flag."`
	RetrogradeStopgap    bool     `name:"retrograde-stopgap" help:"Exclusively downgrade, unpin, and pin the resultant version."`
	Barbarous            bool     `name:"BARBAROUS" help:"Bypass VT security, skip hashes, allow wine, foreign arch, downgrades."`
	PinInstall           bool     `name:"pin-install" default:"false" help:"Pin this installation to the current version."`
	DryRun               bool     `default:"false" help:"Show what would be downloaded."`
	NoCompletionSetup    bool     `name:"no-completion-setup" help:"Do not automatically install shell autocompletion scripts for extracted binaries."`
	AssetBinaries        []string `optional:"" short:"b" help:"If release asset is an archive - names of a binaries in the archive to install."`
	AssetBinariesRegexp  string   `optional:"" short:"B" help:"If release asset is an archive - regular expression matching binaries in the archive to install."`
	IsUpgradeCmd         bool     `kong:"-"`
	SpecificallyTargeted bool     `kong:"-"`
}

// OutputFlags controls UI progress display, styling, and colorization.
type OutputFlags struct {
	ProgressBar string `name:"progress-bar" env:"GH_PT_PROGRESS_BAR" help:"Progress bar style: pacman, standard, spinner:<style>, or none. Spinner styles: dots, line, jump, pulse, points, miniDot, step"`
	NoColor     bool   `name:"no-color" aliases:"no-colors" env:"GH_PT_NO_COLOR" help:"Disable color output."`
	NoEmojis    bool   `name:"no-emojis" aliases:"no-emoji" env:"GH_PT_NO_EMOJIS" help:"Disable emoji/icon output."`
}

// Shared flags across commands
type CommonInstallFlags struct {
	ResolutionFlags
	TargetFlags
	DependencyFlags
	VerificationFlags
	SidecarFlags
	ExecutionFlags
	OutputFlags
}

type InstallCmd struct {
	Repository string `arg:"" env:"GH_PT_REPOSITORY" optional:"" predictor:"github_repos" predict:"github_repos" help:"Github repository in OWNER/REPOSITORY_NAME format."`
	CommonInstallFlags
}

// ToExecContext converts InstallCmd into an ExecContext with resolved parameters.
func (c *InstallCmd) ToExecContext() ExecContext {
	c.ResolveSidecars()
	ctx := ExecContext{
		CommonInstallFlags: c.CommonInstallFlags,
		Repository:         c.Repository,
	}
	ctx.SpecificallyTargeted = c.Repository != ""
	return ctx
}

// ToExecContext converts CommonInstallFlags into an ExecContext with resolved parameters.
func (c *CommonInstallFlags) ToExecContext() ExecContext {
	c.ResolveSidecars()
	return ExecContext{
		CommonInstallFlags: *c,
	}
}

// ToExecContext converts CLI into an ExecContext using the InstallCmd context.
func (c *CLI) ToExecContext() ExecContext {
	return c.Install.ToExecContext()
}

type StateCmd struct {
	Cat    StateCatCmd    `cmd:"" help:"Dump raw state file to stdout."`
	View   StateViewCmd   `cmd:"" help:"Show state file path."`
	Add    StateAddCmd    `cmd:"" help:"Add state entry for a repository."`
	Rm     StateRmCmd     `cmd:"" help:"Remove state entry."`
	Update StateUpdateCmd `cmd:"" help:"Update state entry fields."`
	Edit   StateEditCmd   `cmd:"" help:"Edit saved state interactively."`
}

type StateCatCmd struct{}

type StateViewCmd struct{}

type StateAddCmd struct {
	Repository string `arg:"" predictor:"github_repos" predict:"github_repos" help:"Repository in owner/repo format."`
	Force      bool   `short:"f" help:"Overwrite existing state entry."`
	// Install params
	ReleaseVersion string `short:"v" name:"release-version" optional:"" help:"Version tag."`
	TargetPath     string `short:"p" optional:"" type:"path" help:"Target installation directory."`
	Global         bool   `short:"g" help:"Install globally."`
	Pinned         bool   `help:"Pin this version."`
	Extractor      string `optional:"" help:"Extractor precedence."`
	Type           string `short:"T" optional:"" help:"Comma-separated list of types."`
	ReleaseAsset   string `short:"a" optional:"" help:"Release asset name."`
}

type StateRmCmd struct {
	Target                 string `arg:"" predictor:"installed_apps" predict:"installed_apps" help:"Application or repository to remove."`
	Force                  bool   `short:"f" help:"Skip confirmation prompt."`
	Purge                  bool   `help:"Completely uninstall and purge."`
	StateOnly              bool   `help:"Remove from state only without uninstalling."`
	UnsafeSkipDirFlagCheck bool   `name:"unsafe-skip-dir-flag-check" help:"Bypass .gh-pt-managed verification to force purge legacy directories."`
}

type StateUpdateCmd struct {
	Target string   `arg:"" predictor:"installed_apps" predict:"installed_apps" help:"Application or repository to update."`
	Fields []string `arg:"" optional:"" help:"Field=value pairs to update (e.g., version=1.0.0 pinned=true)."`
}

type RmCmd = StateRmCmd

type StateEditCmd struct{}

type UpgradeFlags struct {
	DisablePrompts        bool   `short:"D" env:"GH_PT_DISABLE_PROMPTS" help:"Disable all interactive prompts."`
	Extractor             string `env:"GH_PT_EXTRACTOR" help:"Archive extractor precedence (default, ouch, native, internal)." default:"${extractor}"`
	Overwrite             bool   `default:"false" short:"f" name:"force" env:"GH_PT_FORCE" help:"Reinstall packages overwriting existing files."`
	DryRun                bool   `default:"false" help:"Show what would be upgraded."`
	VerifyChecksum        bool   `default:"true" help:"Verify asset checksums."`
	InsecureAllowUnsigned bool   `name:"insecure-allow-unsigned" aliases:"skip-checksums" env:"GH_PT_INSECURE_ALLOW_UNSIGNED" help:"Allow unsigned release assets without cryptographic verification."`
	SkipVtSandbox         bool   `default:"true" name:"skip-vt-sandbox" help:"Skip VirusTotal sandbox scan (default: true, use --skip-vt-sandbox=false to enable)."`
	FallbackReleases      int    `default:"0" help:"Try this many older releases if no assets found in latest (0=disabled)."`
	WarnUnmappedAssets    bool   `default:"true" negatable:"" help:"Warn about suspected unmapped sidecar assets."`
	ProgressBar           string `name:"progress-bar" env:"GH_PT_PROGRESS_BAR" help:"Progress bar style: pacman, standard, spinner:<style>, or none. Spinner styles: dots, line, jump, pulse, points, miniDot, step"`
	NoColor               bool   `name:"no-color" aliases:"no-colors" env:"GH_PT_NO_COLOR" help:"Disable color output."`
	NoEmojis              bool   `name:"no-emojis" aliases:"no-emoji" env:"GH_PT_NO_EMOJIS" help:"Disable emoji/icon output."`
}

type UpgradeCmd struct {
	Repository string `arg:"" optional:"" predictor:"installed_apps" predict:"installed_apps" help:"Optional repository to update."`
	User       bool   `short:"u" name:"user" help:"Update only user installations."`
	Global     bool   `short:"g" name:"global" help:"Update only global installations."`
	UpgradeFlags
}

type StateLsCmd struct {
	Filter   string `arg:"" optional:"" help:"Optional filter."`
	Global   bool   `short:"g" help:"Show global installs only."`
	Format   string `short:"o" default:"auto" enum:"auto,table,list,tui" env:"GH_PT_LIST_FORMAT" help:"Output format: auto (table if it fits the terminal, else list), table, list, tui."`
	NoColor  bool   `name:"no-color" aliases:"no-colors" env:"GH_PT_NO_COLOR" help:"Disable color output."`
	NoEmojis bool   `name:"no-emojis" aliases:"no-emoji" env:"GH_PT_NO_EMOJIS" help:"Disable emoji/icon output."`
}

type StateLlCmd struct {
	Filter   string `arg:"" optional:"" help:"Optional filter."`
	Global   bool   `short:"g" help:"Show global installs only."`
	Format   string `short:"o" default:"auto" enum:"auto,table,list,tui" env:"GH_PT_LIST_FORMAT" help:"Output format: auto (table if it fits the terminal, else list), table, list, tui."`
	NoColor  bool   `name:"no-color" aliases:"no-colors" env:"GH_PT_NO_COLOR" help:"Disable color output."`
	NoEmojis bool   `name:"no-emojis" aliases:"no-emoji" env:"GH_PT_NO_EMOJIS" help:"Disable emoji/icon output."`
}

type ConfigCmd struct {
	Ls   ConfigLsCmd   `cmd:"" help:"List config settings."`
	Get  ConfigGetCmd  `cmd:"" help:"Get config setting."`
	Set  ConfigSetCmd  `cmd:"" help:"Set config setting."`
	Rm   ConfigRmCmd   `cmd:"" help:"Remove config setting."`
	Menu ConfigMenuCmd `cmd:"" default:"withargs" help:"Interactive config menu (default)."`
}
type ConfigLsCmd struct{}
type ConfigGetCmd struct {
	Key string `arg:"" predictor:"config_keys" predict:"config_keys"`
}
type ConfigSetCmd struct {
	Key   string `arg:"" predictor:"config_keys" predict:"config_keys"`
	Value string `arg:""`
}
type ConfigRmCmd struct {
	Key string `arg:"" predictor:"config_keys" predict:"config_keys"`
}
type ConfigMenuCmd struct{}

type RepoCmd struct {
	Clone RepoCloneCmd `cmd:"" help:"Clone the repository."`
	Fork  RepoForkCmd  `cmd:"" help:"Fork and clone the repository."`
}

type RepoCloneCmd struct {
	Repository string `arg:"" env:"GH_PT_REPOSITORY" optional:"" help:"Github repository in OWNER/REPOSITORY_NAME format."`
	TargetPath string `short:"p" optional:"" type:"path" help:"Target installation directory."`
	Force      bool   `short:"f" help:"Overwrite existing."`
	MaxDepth   int    `help:"Max clone depth."`
}

type RepoForkCmd struct {
	Repository string `arg:"" env:"GH_PT_REPOSITORY" optional:"" help:"Github repository in OWNER/REPOSITORY_NAME format."`
	TargetPath string `short:"p" optional:"" type:"path" help:"Target installation directory."`
	Force      bool   `short:"f" help:"Overwrite existing."`
	MaxDepth   int    `help:"Max clone depth."`
}

type ScanCmd struct {
	Ai ScanAiCmd `cmd:"" help:"AI safety scan."`
	Vt ScanVtCmd `cmd:"" help:"VirusTotal scan."`
}

type ScanAiCmd struct {
	Target      string `arg:"" optional:"" predictor:"github_repos" predict:"github_repos" help:"Target to scan."`
	Interactive bool   `short:"i" help:"Use interactive AI command from config."`
	AICmd       string `name:"ai-cmd" help:"Command template for AI execution."`
}

type ScanVtCmd struct {
	Target string `arg:"" optional:"" predictor:"github_repos" predict:"github_repos" help:"Target to scan."`
}

type VtCmd struct {
	SetKey VtSetKeyCmd `cmd:"" help:"Set VirusTotal API key."`
}
type VtSetKeyCmd struct {
	Key string `arg:"" help:"VirusTotal API key."`
}

type Show struct {
	Repository       string `arg:"" env:"GH_PT_REPOSITORY" predictor:"github_repos" predict:"github_repos" help:"Github repository."`
	Assets           int    `default:"-1" optional:"" help:"Show available assets (max number)."`
	Versions         int    `default:"-1" optional:"" help:"Show release versions (max number)."`
	Description      int    `default:"-1" optional:"" help:"Show repository description (max lines)."`
	Readme           int    `default:"-1" optional:"" help:"Show repository readme (max lines)."`
	Prerelease       bool   `help:"Include prereleases."`
	Stable           bool   `help:"Include only stable releases."`
	Version          string `short:"v" default:"latest" help:"Version to show."`
	DiscoverSidecars bool   `help:"Discover potential sidecar assets in the repository."`
	NoColor          bool   `name:"no-color" aliases:"no-colors" env:"GH_PT_NO_COLOR" help:"Disable color output."`
	NoEmojis         bool   `name:"no-emojis" aliases:"no-emoji" env:"GH_PT_NO_EMOJIS" help:"Disable emoji/icon output."`
}

type ShowCmd = Show

type Hook struct {
	Event      string `arg:"" enum:"post-install,pre-uninstall" help:"Hook event type (post-install, pre-uninstall)."`
	Repository string `arg:"" predictor:"github_repos" predict:"github_repos" help:"Github repository in OWNER/REPOSITORY_NAME format."`
	ScriptPath string `arg:"" type:"path" help:"Path to the hook script."`
}

type HookCmd = Hook

var HookRunner func(h *Hook) error

func (h *Hook) Run() error {
	if HookRunner != nil {
		return HookRunner(h)
	}
	return nil
}

type SourceCmd struct {
	Repository string `arg:"" env:"GH_PT_REPOSITORY" predictor:"github_repos" predict:"github_repos" help:"Github repository."`
	AICmd      string `help:"Command template for AI execution."`
	TargetOS   string `help:"Target operating system for cross-compilation (e.g., linux, windows, darwin)."`
	TargetArch string `help:"Target architecture for cross-compilation (e.g., amd64, arm64, arm)."`
	CommonInstallFlags
}

type CompletionsCmd struct {
	Bash       CompletionsBashCmd       `cmd:"" help:"Generate Bash completions."`
	Zsh        CompletionsZshCmd        `cmd:"" help:"Generate Zsh completions."`
	Powershell CompletionsPowershellCmd `cmd:"" help:"Generate PowerShell completions."`
}

type CompletionsBashCmd struct{}
type CompletionsZshCmd struct{}
type CompletionsPowershellCmd struct{}

type SearchCmd struct {
	Query       string `arg:""`
	Description bool   `short:"d"`
}

var SearchRunner func(c *SearchCmd, ctx *ExecContext) error

func (c *SearchCmd) Run(ctx *ExecContext) error {
	if SearchRunner != nil {
		return SearchRunner(c, ctx)
	}
	args := []string{"search", "repos", c.Query}
	if c.Description {
		args = append(args, "--match", "name,description")
	} else {
		args = append(args, "--match", "name")
	}
	cmd := exec.Command("gh", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// ExecContext holds the flattened execution parameters
type ExecContext struct {
	UpdateAll bool
	Update    bool
	CommonInstallFlags
	Repository          string
	Clone               bool
	Fork                bool
	MaxDepth            int
	CompileFromSource   bool
	AI                  bool
	AICmd               string
	AISafetyScan        bool
	Track               string // "stable", "prerelease", "latest-commit"
	TargetOS            string // Target OS for cross-compilation
	TargetArch          string // Target architecture for cross-compilation
	LogLevel            string
	LogFormat           string
	LogQuietInteractive bool
	Verbose             bool
	VTApiKey            string
	Ls                  string
	Ll                  string
	Full                bool
	ListFormat          string
	EditSavedState      bool
	RmSavedState        string
	Rm                  string
	Purge               string
	Pin                 string
	Show                bool
	ShowAssets          int
	ShowVersions        int
	ShowDescription     int
	ShowReadme          int
	DiscoverSidecars    bool
	DisableIcons        bool
	HookEvent           string
	HookScriptPath      string
}

type HelperCmd struct {
	GetSystemInfo         bool   `help:"Display system information for AI context."`
	GetManifest           bool   `help:"Display current manifest.json contents."`
	ValidateManifest      bool   `help:"Validate manifest.json schema."`
	ValidateCompileScript bool   `help:"Validate compile.sh for forbidden patterns."`
	ViewTargetDirs        bool   `help:"List contents of target base directory."`
	ViewInstalledFiles    bool   `help:"List paths of all installed files."`
	Install               string `help:"Install file/directory (format: dirname=source)."`
	GetBodyTemplate       bool   `help:"Display body.sh template." name:"get-body-template"`
	AppendManifest        string `help:"Add dependencies (format: manager=pkg@version)."`
	RemoveFromManifest    string `help:"Remove dependencies (format: manager=pkg)."`
	RunCompileScript      bool   `help:"Execute compile.sh."`
}

// TruncateRegex truncates a regex pattern to maxLen characters (default 200) if it is longer than maxLen.
func TruncateRegex(s string, maxLen ...int) string {
	limit := 200
	if len(maxLen) > 0 && maxLen[0] > 0 {
		limit = maxLen[0]
	}
	if len(s) > limit {
		return s[:limit] + "..."
	}
	return s
}
