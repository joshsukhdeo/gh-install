package config

import (
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
	"gopkg.in/yaml.v3"
)

type Config struct {
	InstallTypes   string `yaml:"install_types"`
	InstallPath    string `yaml:"install_path"`
	GlobalPath     string `yaml:"global_path"`
	ClonePath      string `yaml:"clone_path"`
	ForkPath       string `yaml:"fork_path"`
	AICmd          string `yaml:"ai_cmd"`
	AddDeps        bool   `yaml:"add_deps"`
	NoDeps         bool   `yaml:"no_deps"`
	PromptRename   bool   `yaml:"prompt_rename"`
	DisablePrompts bool   `yaml:"disable_prompts"`
	NoSaveState    bool   `yaml:"no_save_state"`
	AllowWine      bool   `yaml:"allow_wine"`
	NativeExtract  bool   `yaml:"native_extract"`
	VTApiKey       string `yaml:"vt_api_key"`
}

func GetConfigPath() string {
	return filepath.Join(xdg.ConfigHome, "gh-install", "config.yml")
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
