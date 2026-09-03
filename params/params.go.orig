package params

import "github.com/alecthomas/kong"

type CLI struct {
	Repository          string            `arg:"" optional:"" help:"Github repository in OWNER/REPOSITORY_NAME format."`
	Interactive         bool              `default:"false" short:"i" help:"Use interactive installation. If true, all non-log related flags are ignored." group:"Interactive Mode"`
	UpdateAll           bool              `short:"U" help:"Update all installed applications (user and global)."`
	Update              bool              `short:"u" help:"Update user installations (add -g for global only)."`
	ListSavedState      bool              `help:"List saved state in a user-friendly format." group:"State Management"`
	EditSavedState      bool              `help:"Edit saved state (enable/disable updates or remove apps)." group:"State Management"`
	RmSavedState        string            `help:"Remove a saved app from state by repository slug or binary name." group:"State Management"`
	ReleaseVersion      string            `default:"latest" short:"v" help:"Repository release tag (version) to install." group:"Non-interactive Mode"`
	ReleaseAsset        string            `optional:"" short:"a" help:"Name of repository release asset to download. If not set, --release-asset-regexp is used." group:"Non-interactive Mode"`
	ReleaseAssetRegexp  string            `optional:"" short:"A" help:"Regular expression matching release asset to download." group:"Non-interactive Mode"`
	ReleaseAssetRegexps []string          `kong:"-"`
	Type                []string          `default:"${install_types}" short:"T" name:"format" env:"GH_INSTALL_TYPE" help:"Comma-separated list of types to match and prioritize." group:"Non-interactive Mode"`
	All                 bool              `default:"false" help:"Install all matched assets instead of just the first one." group:"Non-interactive Mode"`
	AssetBinaries       []string          `optional:"" short:"b" help:"If release asset is an archive - names of a binaries in the archive to install. If not set, --install-binary-regexp is used." group:"Non-interactive Mode"`
	AssetBinariesRegexp string            `optional:"" short:"B" help:"If release asset is an archive - regular expression matching binaries in the archive to install. If not set, repository name is used." group:"Non-interactive Mode"`
	TargetPath          string            `default:"${install_path}" short:"p" type:"path" help:"Target installation directory (default: ~/.local/bin or /usr/local/bin if --global)."`
	Global              bool              `short:"g" help:"Install globally (e.g. /usr/local/bin) instead of user bin." group:"Non-interactive Mode"`
	AddDeps             bool              `short:"y" help:"Automatically resolve and install dependencies without prompting." group:"Non-interactive Mode"`
	NoDeps              bool              `short:"n" help:"Do not install dependencies (use dpkg/rpm directly)." group:"Non-interactive Mode"`
	Rename              map[string]string `optional:"" short:"t" help:"Rename binaries installed at target path, \"<asset archive binary | asset>=<renamed binary>;...\"" group:"Non-interactive Mode"`
	KeepSuffixes        bool              `short:"k" help:"Keep OS/hardware suffixes on extracted binaries instead of automatically stripping them." group:"Non-interactive Mode"`
	DisablePrompts      bool              `short:"D" env:"GH_INSTALL_DISABLE_PROMPTS" help:"Disable all interactive prompts." group:"Non-interactive Mode"`
	NoSaveState         bool              `short:"S" env:"GH_INSTALL_NO_SAVE_STATE" help:"Do not save installation to state (prevents tracking for updates)." group:"Non-interactive Mode"`
	AllowWine           bool              `env:"GH_INSTALL_ALLOW_WINE" help:"Allow installing Windows executables on Linux/macOS/FreeBSD." group:"Non-interactive Mode"`
	AllowForeignArch    bool              `env:"GH_INSTALL_ALLOW_FOREIGN_ARCH" help:"Allow installing assets with foreign architectures (e.g., arm64 on amd64)." group:"Non-interactive Mode"`
	AllowRootUserInstall bool             `help:"Allow installation to user-local paths when running as root (e.g. via sudo)." group:"Non-interactive Mode"`
	NativeExtract       bool              `env:"GH_INSTALL_NATIVE_EXTRACT" help:"Use native OS utilities (tar/7z) for archive extraction instead of pure Go." group:"Non-interactive Mode"`
	Clone               bool              `help:"Clone the repository into clone path (default: ~/src) and track for updates via git pull." group:"Repository Mode"`
	Fork                bool              `help:"Fork and clone the repository into fork path (default: ~/projects) and track for updates." group:"Repository Mode"`
	AI                  bool              `help:"Enable AI-assisted installation." group:"AI Mode"`
	AICmd               string            `default:"agy -p \"%s\"" env:"GH_INSTALL_AI_CMD" help:"Command template for AI agent prompt execution." group:"AI Mode"`
	AISafetyScan        bool              `help:"Use AI to scan the repository for safety concerns before installation (requires --ai)." group:"AI Mode"`
	CompileFromSource   bool              `help:"Compile repository from source via AI-generated build script (requires --ai)." group:"AI Mode"`
	TargetPathCreate    bool              `default:"true" negatable:"" help:"Create target installation directory if it does not exist." group:"Non-interactive Mode"`
	Overwrite           bool              `default:"false" short:"o" help:"Overwrite target binaries." group:"Non-interactive Mode"`
	Pin                 bool              `default:"false" help:"Pin this installation to the current version (skip during updates)." group:"State Management"`
	DryRun              bool              `default:"false" help:"Show what would be downloaded and installed without actually doing it." group:"Non-interactive Mode"`
	VerifyChecksum      bool              `default:"true" help:"Verify asset checksums if checksum files are available in the release." group:"Non-interactive Mode"`
	VTApiKey            string            `env:"VT_API_KEY" help:"VirusTotal API key for malicious binary checking." group:"Security Mode"`
	SkipVtSandbox       bool              `help:"Bypass VirusTotal sandbox upload for unknown zero-day hashes." group:"Security Mode"`
	LogLevel            string            `default:"info" enum:"error,warn,info,debug" short:"l" help:"Log level."`
	LogFormat           string            `default:"console" enum:"console,json" short:"f" help:"Log output format."`
	LogQuietInteractive bool              `default:"true" negatable:"" help:"Quiet log in interactive mode" group:"Interactive Mode"`
	Verbose             bool              `short:"V" help:"Enable verbose output (sets log level to debug)."`
	Version             kong.VersionFlag  `help:"Show version." env:""`
}
