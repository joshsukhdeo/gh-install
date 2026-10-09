package config

import (
	"bytes"
	"crypto/sha256"
	"fmt"
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
	Checksum             string                     `yaml:"-"`
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
	AddDeps                                       bool   `yaml:"add_deps"`
	DisablePrompts                                bool   `yaml:"disable_prompts"`
	DisablePropts                                 bool   `yaml:"disable_propts,omitempty"`
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

func checkForAliases(node *yaml.Node) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.AliasNode {
		return fmt.Errorf("config file contains YAML aliases/anchors (*%s), which are forbidden for security reasons", node.Value)
	}
	for _, child := range node.Content {
		if err := checkForAliases(child); err != nil {
			return err
		}
	}
	return nil
}

func LoadConfig() (*Config, error) {
	path := GetConfigPath()
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}

	const maxConfigSize = 1024 * 1024 // 1 MB limit
	if info.Size() > maxConfigSize {
		return nil, fmt.Errorf("config file %s exceeds maximum size of %d bytes", path, maxConfigSize)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Validate AST for YAML anchors / aliases (billion laughs / expansion attacks)
	var rootNode yaml.Node
	if err := yaml.Unmarshal(data, &rootNode); err != nil {
		return nil, fmt.Errorf("failed to parse YAML syntax: %w", err)
	}
	if err := checkForAliases(&rootNode); err != nil {
		return nil, err
	}

	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("invalid config format: %w", err)
	}

	cfg.Checksum = fmt.Sprintf("%x", sha256.Sum256(data))

	applyConfigDefaults(&cfg)

	if err := ValidateConfig(&cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

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

// ValidateConfig validates configuration values to prevent injection and ensure safety.
func ValidateConfig(cfg *Config) error {
	// Validate path fields - no path traversal, no shell metacharacters, no sensitive system directories
	pathFields := map[string]string{
		"install_path":    cfg.Paths.InstallPath,
		"global_path":     cfg.Paths.GlobalPath,
		"clone_path":      cfg.Paths.ClonePath,
		"fork_path":       cfg.Paths.ForkPath,
		"sidecar_path":    cfg.Paths.SidecarPath,
		"target_base_dir": cfg.Paths.TargetBaseDir,
		"repo_dir":        cfg.Paths.RepoDir,
		"package_path":    cfg.Paths.PackagePath,
	}

	vitalDirs := []string{
		"/etc", "/root", "/sys", "/proc", "/dev", "/boot",
		"/bin", "/sbin", "/usr/bin", "/usr/sbin", "/var", "/lib", "/lib64", "/usr/lib",
	}

	for field, value := range pathFields {
		if value != "" {
			cleanPath := filepath.Clean(value)
			if cleanPath != value {
				return fmt.Errorf("config %s contains path traversal sequences: %q", field, value)
			}
			if strings.ContainsAny(value, "`$&|;(){}[]<>") {
				return fmt.Errorf("config %s contains shell metacharacters: %q", field, value)
			}
			if len(value) > 4096 {
				return fmt.Errorf("config %s exceeds maximum length (4096): %q", field, value)
			}
			if cleanPath == "/" {
				return fmt.Errorf("config %s cannot point to root filesystem: %q", field, value)
			}
			for _, vital := range vitalDirs {
				if cleanPath == vital || strings.HasPrefix(cleanPath, vital+"/") {
					return fmt.Errorf("config %s cannot point to sensitive system directory: %q", field, value)
				}
			}
		}
	}

	// Validate AI command templates - no shell metacharacters
	if cfg.AI.AICmd != "" && strings.ContainsAny(cfg.AI.AICmd, "`$&|;(){}[]<>\\") {
		return fmt.Errorf("config ai_cmd contains shell metacharacters: %q", cfg.AI.AICmd)
	}
	if cfg.AI.AIInteractiveCmd != "" && strings.ContainsAny(cfg.AI.AIInteractiveCmd, "`$&|;(){}[]<>\\") {
		return fmt.Errorf("config ai_interactive_cmd contains shell metacharacters: %q", cfg.AI.AIInteractiveCmd)
	}

	// Validate enum fields
	validWineModes := map[string]bool{"off": true, "allow": true, "priority": true, "force": true}
	if cfg.Core.Wine != "" && !validWineModes[cfg.Core.Wine] {
		return fmt.Errorf("config wine mode %q invalid (must be: off, allow, priority, force)", cfg.Core.Wine)
	}

	validExtractors := map[string]bool{"default": true, "ouch": true, "native": true, "internal": true}
	if cfg.Core.Extractor != "" {
		for _, part := range strings.Split(cfg.Core.Extractor, ",") {
			p := strings.TrimSpace(part)
			if !validExtractors[p] {
				return fmt.Errorf("config extractor %q invalid (must be comma-separated list or one of: default, ouch, native, internal)", cfg.Core.Extractor)
			}
		}
	}

	validProgressBars := map[string]bool{"pacman": true, "standard": true, "none": true, "conveyor": true}
	// Also allow spinner:* variants
	if cfg.Core.ProgressBar != "" {
		valid := validProgressBars[cfg.Core.ProgressBar]
		if !valid && !strings.HasPrefix(cfg.Core.ProgressBar, "spinner:") {
			return fmt.Errorf("config progress_bar %q invalid (must be: pacman, standard, none, conveyor, or spinner:STYLE)", cfg.Core.ProgressBar)
		}
	}

	validAvxLevels := map[string]bool{"auto": true, "none": true, "avx": true, "avx2": true, "avx512": true}
	if cfg.Core.AvxLevel != "" && !validAvxLevels[cfg.Core.AvxLevel] {
		return fmt.Errorf("config avx_level %q invalid (must be: auto, none, avx, avx2, avx512)", cfg.Core.AvxLevel)
	}

	// Validate dependency resolution priorities
	for mgr, pkgs := range cfg.DependencyResolution.Priorities {
		if strings.ContainsAny(mgr, "`$&|;(){}[]<>\\") {
			return fmt.Errorf("config dependency_resolution manager name contains shell metacharacters: %q", mgr)
		}
		for _, pkg := range pkgs {
			if strings.ContainsAny(pkg, "`$&|;(){}[]<>\\") {
				return fmt.Errorf("config dependency_resolution package %q contains shell metacharacters", pkg)
			}
		}
	}

	return nil
}
