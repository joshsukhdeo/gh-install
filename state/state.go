package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/adrg/xdg"
)

type InstalledApp struct {
	Repository    string            `json:"repository"`
	TargetPath    string            `json:"target_path"`
	Global        bool              `json:"global"`
	ReleaseAsset  string            `json:"release_asset"`
	ReleaseRegexp string            `json:"release_regexp"`
	Version       string            `json:"version"`
	Rename        map[string]string `json:"target_binaries"`
	Disabled      bool              `json:"disabled"`
}

type State struct {
	Apps map[string]*InstalledApp `json:"apps"`
	mu   sync.Mutex
}

func GetStatePath() string {
	return filepath.Join(xdg.DataHome, "gh-install", "state.json")
}

func LoadState() (*State, error) {
	path := GetStatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{Apps: make(map[string]*InstalledApp)}, nil
		}
		return nil, err
	}

	var s State
	err = json.Unmarshal(data, &s)
	if err != nil {
		return nil, err
	}
	if s.Apps == nil {
		s.Apps = make(map[string]*InstalledApp)
	}
	return &s, nil
}

func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := GetStatePath()
	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func (s *State) AddApp(app *InstalledApp) error {
	s.mu.Lock()
	s.Apps[app.Repository] = app
	s.mu.Unlock()
	return s.Save()
}
