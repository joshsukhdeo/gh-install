# state/ — State Management (V3 Schema)

## Files
- `state.go` — State struct, migrations, LoadState/Save

## State V3 Struct
```go
type State struct {
    Version        int
    Apps           map[string]AppState
    TargetBaseDir  string          // V3: inferred from first app's TargetPath
    Global         bool            // V3: global install flag
    SourceRepos    map[string]SourceRepo  // V3: source repo tracking
}

type SourceRepo struct {
    Repository     string
    RepoPath       string
    IsFork         bool
    CurrentVersion string
    Track          bool
    LastUpdated    time.Time
    CompileScript  string
    ManifestPath   string
}
```

## Migrations
- V1→V2: Adds Apps map
- V2→V3: Adds TargetBaseDir, Global, SourceRepos; auto-saves after migration
- `LoadState()` handles all migrations and persists

## Key Invariants
- Always use `state.LoadState()` / `st.Save()`
- Never bypass `state.json.lock`
- Test isolation: `XDG_DATA_HOME` via `t.TempDir()` + `xdg.Reload()`