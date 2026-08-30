package params

import "github.com/alecthomas/kong"

type CLI struct {
	Repository          string            `arg:"" optional:"" help:"Github repository in OWNER/REPOSITORY_NAME format."`
	Interactive         bool              `default:"false" short:"i" help:"Use interactive installation. If true, all non-log related flags are ignored." group:"Interactive Mode"`
	UpdateAll           bool              `short:"U" help:"Update all installed applications (user and global)."`
	Update              bool              `short:"u" help:"Update user installations (add -g for global only)."`
	SetupTopgradeStep   bool              `help:"Add an entry to the topgrade configuration for gh-install."`
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
	PromptRename        bool              `short:"r" help:"Prompt to strip OS/hardware affixes from long binary names." group:"Interactive Mode"`
	DisablePrompts      bool              `short:"D" env:"GH_INSTALL_DISABLE_PROMPTS" help:"Disable all interactive prompts." group:"Non-interactive Mode"`
	NoSaveState         bool              `short:"S" env:"GH_INSTALL_NO_SAVE_STATE" help:"Do not save installation to state (prevents tracking for updates)." group:"Non-interactive Mode"`
	TargetPathCreate    bool              `default:"true" negatable:"" help:"Create target installation directory if it does not exist." group:"Non-interactive Mode"`
	Overwrite           bool              `default:"false" short:"o" help:"Overwrite target binaries." group:"Non-interactive Mode"`
	LogLevel            string            `default:"info" enum:"error,warn,info,debug" short:"l" help:"Log level."`
	LogFormat           string            `default:"console" enum:"console,json" short:"f" help:"Log output format."`
	LogQuietInteractive bool              `default:"true" negatable:"" help:"Quiet log in interactive mode" group:"Interactive Mode"`
	Version             kong.VersionFlag  `help:"Show version." env:""`
}
