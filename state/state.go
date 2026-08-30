package state

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
	"github.com/gofrs/flock"
)

type InstalledApp struct {
	Repository          string            `json:"repository"`
	TargetPath          string            `json:"target_path"`
	Global              bool              `json:"global"`
	ReleaseAsset        string            `json:"release_asset"`
	ReleaseRegexp       string            `json:"release_regexp"`
	Version             string            `json:"version"`
	Rename              map[string]string `json:"target_binaries"`
	Disabled            bool              `json:"disabled"`
	Type                []string          `json:"type"`
	All                 bool              `json:"all"`
	AssetBinaries       []string          `json:"asset_binaries"`
	AssetBinariesRegexp string            `json:"asset_binaries_regexp"`
	PackageNames        []string          `json:"package_names,omitempty"`
	Pinned              bool              `json:"pinned,omitempty"`
	NativeExtract       bool              `json:"native_extract"`
}

type State struct {
	Apps map[string]*InstalledApp `json:"apps"`
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
	path := GetStatePath()
	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		return err
	}

	lock := flock.New(path + ".lock")
	if err := lock.Lock(); err != nil {
		return err
	}
	defer func() { _ = lock.Unlock() }()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func (s *State) AddApp(app *InstalledApp) error {
	s.Apps[app.Repository] = app
	return s.Save()
}
