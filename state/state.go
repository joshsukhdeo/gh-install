package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/adrg/xdg"
	"github.com/gofrs/flock"
)

type InstalledApp struct {
	Repository               string            `json:"repository"`
	TargetPath               string            `json:"target_path"`
	Global                   bool              `json:"global"`
	ReleaseAsset             string            `json:"release_asset"`
	ReleaseRegexp            string            `json:"release_regexp"`
	Version                  string            `json:"version"`
	Rename                   map[string]string `json:"target_binaries"`
	Disabled                 bool              `json:"disabled"`
	Type                     []string          `json:"type"`
	All                      bool              `json:"all"`
	AssetBinaries            []string          `json:"asset_binaries"`
	AssetBinariesRegexp      string            `json:"asset_binaries_regexp"`
	InstalledBinaries        []string          `json:"installed_binaries,omitempty"`
	InstalledAssetNames      []string          `json:"installed_asset_names,omitempty"`
	InstalledAssetsFullNames []string          `json:"installed_assets_full_names,omitempty"`
	ContainingArchive        string            `json:"containing_archive,omitempty"`
	PackageNames             []string          `json:"package_names,omitempty"`
	Pinned                   bool              `json:"pinned,omitempty"`
	Extractor                string            `json:"extractor,omitempty"`
	Clone                    bool              `json:"clone,omitempty"`
	Fork                     bool              `json:"fork,omitempty"`
	MaxDepth                 int               `json:"max_depth,omitempty"`
	CompileScript            string            `json:"compile_script,omitempty"`
	IsPrerelease             bool              `json:"is_prerelease,omitempty"`
	LastVTScan               string            `json:"last_vt_scan,omitempty"`
	LastAIScan               string            `json:"last_ai_scan,omitempty"`
	SymlinkDir               string            `json:"symlink_dir,omitempty"`
	Hooks                    map[string]string `json:"hooks,omitempty"`
	SystemPackages           []string          `json:"system_packages,omitempty"`
	Sidecars                 string            `json:"sidecars,omitempty"`
	SidecarSymlinkTo         []string          `json:"sidecar_symlink_to,omitempty"`
	IncludeSidecars          string            `json:"include_sidecars,omitempty"`
	InstalledSidecars        []string          `json:"installed_sidecars,omitempty"`
	EnvInject                []string          `json:"env_inject,omitempty"`
	FallbackReleases         int               `json:"fallback_releases,omitempty"`
}

// UnmarshalJSON implements custom unmarshaling for backward compatibility.
// Handles the migration from bool to string for IncludeSidecars field.
// Handles the migration from []string to string for Sidecars field.
func (app *InstalledApp) UnmarshalJSON(data []byte) error {
	type Alias InstalledApp
	aux := &struct {
		IncludeSidecars interface{} `json:"include_sidecars,omitempty"`
		Sidecars        interface{} `json:"sidecars,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(app),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Handle IncludeSidecars migration from bool to string
	switch v := aux.IncludeSidecars.(type) {
	case bool:
		if v {
			app.IncludeSidecars = "xdg_data_home"
		} else {
			app.IncludeSidecars = ""
		}
	case string:
		app.IncludeSidecars = v
	case nil:
		app.IncludeSidecars = ""
	}

	// Handle Sidecars migration from []string to string
	switch v := aux.Sidecars.(type) {
	case []interface{}:
		// Convert array of patterns to a regex pattern
		if len(v) > 0 {
			// Join patterns with | (OR)
			patterns := make([]string, len(v))
			for i, p := range v {
				if s, ok := p.(string); ok {
					patterns[i] = s
				}
			}
			app.Sidecars = strings.Join(patterns, "|")
		}
	case string:
		app.Sidecars = v
	case nil:
		app.Sidecars = ""
	}

	return nil
}

type StateManager interface {
	Save() error
	AddApp(app *InstalledApp) error
}

type InstallPkg struct {
	Manager   string `json:"manager,omitempty"`
	PackageID string `json:"PackageId,omitempty"`
}

func (p *InstallPkg) UnmarshalJSON(data []byte) error {
	type Alias InstallPkg
	aux := &struct {
		AltPackageID string `json:"packageId,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(p),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if p.PackageID == "" && aux.AltPackageID != "" {
		p.PackageID = aux.AltPackageID
	}
	return nil
}

type InstallStep struct {
	Pkg   *InstallPkg `json:"pkg,omitempty"`
	Files []string    `json:"files,omitempty"`
}

type InstallMapEntry struct {
	Deps      *InstallStep `json:"deps,omitempty"`
	Installed *InstallStep `json:"installed,omitempty"`
}

type SourceRepo struct {
	Repository     string `json:"repository"`
	RepoPath       string `json:"repo_path"`
	IsFork         bool   `json:"is_fork"`
	CurrentVersion string `json:"current_version"`
	Track          string `json:"track"`
	LastUpdated    string `json:"last_updated"`
	CompileScript  string `json:"compile_script"`
	ManifestPath   string `json:"manifest_path"`
}

type State struct {
	Version        int                         `json:"version,omitempty"`
	TargetBaseDir  string                      `json:"target_base_dir,omitempty"`
	Global         bool                        `json:"global,omitempty"`
	Apps           map[string]*InstalledApp    `json:"apps"`
	Repos          map[string]*InstalledApp    `json:"repos,omitempty"`
	SourceRepos    map[string]*SourceRepo      `json:"source_repos,omitempty"`
	SystemPackages []string                    `json:"system_packages,omitempty"`
	Hooks          map[string]string           `json:"hooks,omitempty"`
	InstallMap     map[string]*InstallMapEntry `json:"install-map,omitempty"`
}

var _ StateManager = (*State)(nil)

func GetDataHome() string {
	// If a test directly modified xdg.DataHome to a temporary directory:
	if xdg.DataHome != "" && (strings.Contains(xdg.DataHome, "tmp") || strings.Contains(xdg.DataHome, "Test")) {
		return xdg.DataHome
	}
	// If a test set XDG_DATA_HOME environment variable to a temporary directory:
	env := os.Getenv("XDG_DATA_HOME")
	if env != "" && (strings.Contains(env, "tmp") || strings.Contains(env, "Test")) {
		return env
	}
	if xdg.DataHome != "" {
		return xdg.DataHome
	}
	if env != "" {
		return env
	}
	return filepath.Join(os.Getenv("HOME"), ".local", "share")
}

func GetStatePath() string {
	return filepath.Join(GetDataHome(), "gh-pt", "state.json")
}

func GetSourceDir() string {
	return filepath.Join(GetDataHome(), "gh-pt", "source")
}

func (s *State) migrateV1toV2() error {
	if s.Version >= 2 {
		return nil
	}
	s.Version = 2
	if s.Apps == nil {
		s.Apps = make(map[string]*InstalledApp)
	}
	if s.Repos == nil {
		s.Repos = make(map[string]*InstalledApp)
	}
	if s.Hooks == nil {
		s.Hooks = make(map[string]string)
	}
	if s.SystemPackages == nil {
		s.SystemPackages = []string{}
	}
	if s.InstallMap == nil {
		s.InstallMap = make(map[string]*InstallMapEntry)
	}

	sysPkgSet := make(map[string]bool)
	for _, pkg := range s.SystemPackages {
		sysPkgSet[pkg] = true
	}

	// Segregate apps and repos
	for repoName, app := range s.Apps {
		if len(app.PackageNames) > 0 && len(app.SystemPackages) == 0 {
			app.SystemPackages = append([]string{}, app.PackageNames...)
		}
		for _, pkg := range app.SystemPackages {
			if !sysPkgSet[pkg] {
				sysPkgSet[pkg] = true
				s.SystemPackages = append(s.SystemPackages, pkg)
			}
		}

		if app.Clone || app.Fork {
			s.Repos[repoName] = app
			delete(s.Apps, repoName)
		}
	}

	for _, repo := range s.Repos {
		if len(repo.PackageNames) > 0 && len(repo.SystemPackages) == 0 {
			repo.SystemPackages = append([]string{}, repo.PackageNames...)
		}
		for _, pkg := range repo.SystemPackages {
			if !sysPkgSet[pkg] {
				sysPkgSet[pkg] = true
				s.SystemPackages = append(s.SystemPackages, pkg)
			}
		}
	}

	return nil
}

func (s *State) migrateV2toV3() error {
	if s.Version >= 3 {
		return nil
	}
	s.Version = 3
	if s.SourceRepos == nil {
		s.SourceRepos = make(map[string]*SourceRepo)
	}

	// Infer TargetBaseDir from existing apps if not set
	if s.TargetBaseDir == "" && len(s.Apps) > 0 {
		for _, app := range s.Apps {
			if app.TargetPath != "" {
				dir := filepath.Dir(app.TargetPath)
				if dir != "" && dir != "." {
					s.TargetBaseDir = dir
					s.Global = app.Global
					break
				}
			}
		}
	}

	return nil
}

func migrateV1toV2(states ...*State) error {
	if len(states) == 0 {
		st, err := LoadState()
		if err != nil {
			return err
		}
		if err := st.migrateV1toV2(); err != nil {
			return err
		}
		return st.Save()
	}
	return states[0].migrateV1toV2()
}

func LoadState() (*State, error) {
	path := GetStatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{
				Version:        3,
				Apps:           make(map[string]*InstalledApp),
				Repos:          make(map[string]*InstalledApp),
				SourceRepos:    make(map[string]*SourceRepo),
				Hooks:          make(map[string]string),
				SystemPackages: []string{},
				InstallMap:     make(map[string]*InstallMapEntry),
			}, nil
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
	if s.Repos == nil {
		s.Repos = make(map[string]*InstalledApp)
	}
	if s.Hooks == nil {
		s.Hooks = make(map[string]string)
	}
	if s.SystemPackages == nil {
		s.SystemPackages = []string{}
	}
	if s.InstallMap == nil {
		s.InstallMap = make(map[string]*InstallMapEntry)
	}
	if s.SourceRepos == nil {
		s.SourceRepos = make(map[string]*SourceRepo)
	}

	if s.Version < 2 {
		// Create a durable backup of the V1 state before migrating
		if f, err := os.Create(path + ".v1.bak"); err == nil {
			_, _ = f.Write(data)
			_ = f.Sync()
			_ = f.Close()
		}
		_ = s.migrateV1toV2()
		_ = s.Save()
	}

	if s.Version < 3 {
		_ = s.migrateV2toV3()
		_ = s.Save()
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
	if s.Apps == nil {
		s.Apps = make(map[string]*InstalledApp)
	}
	s.Apps[app.Repository] = app
	return s.Save()
}

func (s *State) SetInstallMap(repo string, entry *InstallMapEntry) error {
	if s.InstallMap == nil {
		s.InstallMap = make(map[string]*InstallMapEntry)
	}
	s.InstallMap[repo] = entry
	return s.Save()
}

func (s *State) GetInstallMap(repo string) *InstallMapEntry {
	if s.InstallMap == nil {
		return nil
	}
	return s.InstallMap[repo]
}
