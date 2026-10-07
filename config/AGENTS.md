# config/ — Configuration and Paths

## Files
- `config.go` — Config structs, defaults, migration

## Config Structs
```go
type PathsConfig struct {
    InstallPath    string
    TargetBaseDir  string  // V3: inferred from InstallPath
    RepoDir        string  // V3: defaults to $XDG_DATA_HOME/gh-pt/repos
}

type CoreConfig struct {
    Global bool  // V3: global install mode
}
```

## Defaults Inference
```go
func applyConfigDefaults(cfg *Config) {
    if cfg.Paths.TargetBaseDir == "" && cfg.Paths.InstallPath != "" {
        cfg.Paths.TargetBaseDir = filepath.Dir(cfg.Paths.InstallPath)
    }
    if cfg.Paths.RepoDir == "" {
        cfg.Paths.RepoDir = filepath.Join(xdg.DataHome(), "gh-pt", "repos")
    }
}
```

## Migration
- Reads legacy fields, applies defaults, writes back on change
- TargetBaseDir persists for source repo compilation