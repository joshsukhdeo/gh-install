package config

import (
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
	"gopkg.in/yaml.v3"
)

type Config struct {
	InstallTypes string `yaml:"install_types"`
	InstallPath  string `yaml:"install_path"`
	GlobalPath   string `yaml:"global_path"`
	AddDeps      bool   `yaml:"add_deps"`
	NoDeps       bool   `yaml:"no_deps"`
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
