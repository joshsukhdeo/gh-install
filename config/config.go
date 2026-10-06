package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/adrg/xdg"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Paths                PathsConfig                `yaml:",inline"`
	AI                   AIConfig                   `yaml:",inline"`
	Core                 CoreConfig                 `yaml:",inline"`
	DependencyResolution DependencyResolutionConfig `yaml:"dependency_resolution"`
}

type PathsConfig struct {
	InstallPath   string `yaml:"install_path"`
	GlobalPath    string `yaml:"global_path"`
	ClonePath     string `yaml:"clone_path"`
	ForkPath      string `yaml:"fork_path"`
	SidecarPath   string `yaml:"sidecar_path"`
	TargetBaseDir string `yaml:"target_base_dir"`
	RepoDir       string `yaml:"repo_dir"`
	PackagePath   string `yaml:"package_path"`
}

type AIConfig struct {
	AICmd            string `yaml:"ai_cmd"`
	AIInteractiveCmd string `yaml:"ai_interactive_cmd"`
}

type CoreConfig struct {
	InstallTypes                                  string `yaml:"install_types"`
	ResolveDeps                                   bool   `yaml:"resolve_deps"`
	NoDeps                                        bool   `yaml:"no_deps"`
	DisablePrompts                                bool   `yaml:"disable_propts"`
	NoSaveState                                   bool   `yaml:"no_save_state"`
	Global                                        bool   `yaml:"global"`
	Wine                                          string `yaml:"wine"`
	Extractor                                     string `yaml:"extractor"`
	KeepSuffixes                                  bool   `yaml:"keep_suffixes"`
	VTApiKey                                      string `yaml:"vt_api_key"`
	AllowPrerelease                               bool   `yaml:"allow_prerelease"`
	DisableIcons                                  bool   `yaml:"disable_icons"`
	LogToFile                                     bool   `yaml:"log_to_file"`
	Symlink                                       bool   `yaml:"symlink"`
	SearchForInstallInstructionsIfNoReleaseAssets bool   `yaml:"readme_fallback"`
	WarnUnmappedAssets                            bool   `yaml:"warn_unmapped_assets"`
	ProgressBar                                   string `yaml:"progress_bar"`
	NoColor                                       bool   `yaml:"no_color"`
	NoEmojis                                      bool   `yaml:"no_emojis"`
	AvxLevel                                      string `yaml:"avx_level"`
}

type DependencyResolutionConfig struct {
	Priorities map[string][]string `yaml:"priorities"`
}

type DependencyResolution = DependencyResolutionConfig

func GetConfigPath() string {
	return filepath.Join(xdg.ConfigHome, "gh-pt", "config.yml")
}

func LoadConfig() (*Config, error) {
	path := GetConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}

	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}

	applyConfigDefaults(&cfg)

	return &cfg, nil
}

func SaveConfig(cfg *Config) error {
	path := GetConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func applyConfigDefaults(cfg *Config) {
	if cfg.Paths.TargetBaseDir == "" {
		if cfg.Paths.InstallPath != "" {
			cfg.Paths.TargetBaseDir = strings.TrimSuffix(cfg.Paths.InstallPath, "/bin")
		} else if home, err := os.UserHomeDir(); err == nil {
			cfg.Paths.TargetBaseDir = filepath.Join(home, ".local")
		}
	}
	if cfg.Paths.RepoDir == "" {
		cfg.Paths.RepoDir = filepath.Join(xdg.DataHome, "gh-pt", "repos")
	}
}
