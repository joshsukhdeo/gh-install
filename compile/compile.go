package compile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/joshsukhdeo/gh-pt/ai"
	"github.com/joshsukhdeo/gh-pt/heuristics"
	"github.com/joshsukhdeo/gh-pt/resolver"
	"github.com/joshsukhdeo/gh-pt/safety"
	"github.com/joshsukhdeo/gh-pt/state"
)

// Config holds the configuration for a compile operation.
type Config struct {
	Repository       string
	RepoDir          string
	BuildDir         string
	ScriptPath       string
	ManifestPath     string
	TargetPath       string
	SymlinkDir       string
	InstallDirTarget string
	Track            string // "stable", "prerelease", "latest-commit"
	Version          string
	CompileScript    string
}

// CompileResult contains the results of a compile operation.
type CompileResult struct {
	Version          string
	InstalledFiles   []string
	Sidecars         []string
	Dependencies     []ai.Dependency
	CommitHash       string
}

// Execute runs the compile flow for a repository.
func Execute(cfg *Config, deps []ai.Dependency) (*CompileResult, error) {
	// Ensure build directory exists
	if err := os.MkdirAll(cfg.BuildDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create build directory: %w", err)
	}

	// Create .ghpt directory
	ghptDir := filepath.Join(cfg.RepoDir, ".ghpt")
	if err := os.MkdirAll(ghptDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create .ghpt directory: %w", err)
	}

	// Create symlinks in .ghpt directory
	if err := setupGhptSymlinks(cfg); err != nil {
		return nil, fmt.Errorf("failed to setup .ghpt symlinks: %w", err)
	}

	// Resolve dependencies
	if len(deps) > 0 {
		if err := resolveDependencies(deps, cfg.RepoDir); err != nil {
			return nil, fmt.Errorf("failed to resolve dependencies: %w", err)
		}
	}

	// Execute compile script in container
	if err := ExecuteInContainer(cfg); err != nil {
		return nil, fmt.Errorf("compile execution failed: %w", err)
	}

	// Get commit hash for version tracking
	commitHash := getCommitHash(cfg.RepoDir)

	// Collect installed files
	installedFiles := collectInstalledFiles(cfg.InstallDirTarget)

	// Check for .ghpt/dist handoff
	distDir := filepath.Join(ghptDir, "dist")
	var sidecars []string
	if info, err := os.Stat(distDir); err == nil && info.IsDir() {
		sidecars = handleDistHandoff(distDir, cfg.TargetPath)
	}

	return &CompileResult{
		Version:        determineVersion(cfg.Track, commitHash),
		InstalledFiles: installedFiles,
		Sidecars:       sidecars,
		Dependencies:   deps,
		CommitHash:     commitHash,
	}, nil
}

// setupGhptSymlinks creates symlinks in .ghpt directory for build artifacts.
func setupGhptSymlinks(cfg *Config) error {
	ghptDir := filepath.Join(cfg.RepoDir, ".ghpt")

	// Create install-dir symlink
	installDirLink := filepath.Join(ghptDir, "install-dir")
	if err := os.Remove(installDirLink); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Symlink(cfg.InstallDirTarget, installDirLink); err != nil {
		return fmt.Errorf("failed to create install-dir symlink: %w", err)
	}

	// Create manifest.json symlink if it doesn't exist in repo
	manifestLink := filepath.Join(ghptDir, "manifest.json")
	if err := os.Remove(manifestLink); err != nil && !os.IsNotExist(err) {
		return err
	}
	if cfg.ManifestPath != "" {
		if err := os.Symlink(cfg.ManifestPath, manifestLink); err != nil {
			return fmt.Errorf("failed to create manifest.json symlink: %w", err)
		}
	}

	// Create compile.sh symlink if it doesn't exist in repo
	compileLink := filepath.Join(ghptDir, "compile.sh")
	if err := os.Remove(compileLink); err != nil && !os.IsNotExist(err) {
		return err
	}
	if cfg.ScriptPath != "" {
		if err := os.Symlink(cfg.ScriptPath, compileLink); err != nil {
			return fmt.Errorf("failed to create compile.sh symlink: %w", err)
		}
	}

	return nil
}

// resolveDependencies installs build dependencies using appropriate package managers.
func resolveDependencies(deps []ai.Dependency, repoDir string) error {
	// Detect ecosystem
	ecosystem, err := heuristics.DetectEcosystem(repoDir)
	if err != nil {
		log.Warn("failed to detect repository ecosystem", "error", err)
		ecosystem = heuristics.PriorityDefault
	}
	log.Info("detected repository ecosystem", "ecosystem", ecosystem)

	// Get priority chain (hardcoded for now, should come from config)
	priorityChain := heuristics.ResolvePriorityChain(nil, ecosystem)
	log.Info("resolved dependency priority chain", "chain", priorityChain)

	// Install each dependency
	for _, dep := range deps {
		resolverName := dep.GetResolver()
		var mgr resolver.PackageManager

		if resolverName != "" {
			mgr, err = resolver.GetManager(resolverName)
		}

		if mgr == nil || err != nil {
			// Fall back to priority chain
			for _, name := range priorityChain {
				m, merr := resolver.GetManager(name)
				if merr == nil && m.IsInstalled() {
					mgr = m
					break
				}
			}
		}

		if mgr == nil {
			mgr, _ = resolver.GetNativeManager()
		}

		if mgr == nil {
			return fmt.Errorf("could not find suitable package manager to install dependency '%s'", dep.Name)
		}

		log.Info("installing dependency", "dependency", dep.Name, "resolver", mgr.Name())
		if err := mgr.Install([]string{dep.Name}); err != nil {
			return fmt.Errorf("failed to install dependency '%s' with resolver '%s': %w", dep.Name, mgr.Name(), err)
		}
	}

	return nil
}

// getCommitHash returns the current commit hash of the repository.
func getCommitHash(repoDir string) string {
	// This would normally call git rev-parse HEAD
	// For now, return placeholder
	return "unknown"
}

// collectInstalledFiles returns a list of all installed files in the target directory.
func collectInstalledFiles(installDir string) []string {
	var files []string
	filepath.Walk(installDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	return files
}

// handleDistHandoff processes binaries from .ghpt/dist/ directory.
func handleDistHandoff(distDir, targetPath string) []string {
	// Similar to moveDistWithSidecarDetection in root.go
	// For now, just move files
	var sidecars []string
	filepath.Walk(distDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(distDir, path)
		if err != nil {
			return nil
		}
		destPath := filepath.Join(targetPath, rel)
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return nil
		}
		if err := copyFile(path, destPath); err != nil {
			return nil
		}
		os.Chmod(destPath, 0755)
		return nil
	})
	return sidecars
}

// copyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

// determineVersion returns the appropriate version string based on track.
func determineVersion(track, commitHash string) string {
	// This will be enhanced to use git tags for stable/prerelease
	switch track {
	case "latest-commit":
		return commitHash
	default:
		// Would check for git tags
		return commitHash
	}
}

// SaveCompileState persists compile metadata to state.
func SaveCompileState(cfg *Config, result *CompileResult) error {
	st, err := state.LoadState()
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	// Create or update install map entry
	entry := st.GetInstallMap(cfg.Repository)
	if entry == nil {
		entry = &state.InstallMapEntry{}
	}

	// Record dependencies
	if len(result.Dependencies) > 0 {
		entry.Deps = &state.InstallStep{
			Pkg: &state.InstallPkg{
				Manager:   "multiple",
				PackageID: formatDependencies(result.Dependencies),
			},
		}
	}

	// Record installed files
	entry.Installed = &state.InstallStep{
		Files: result.InstalledFiles,
	}

	if err := st.SetInstallMap(cfg.Repository, entry); err != nil {
		return fmt.Errorf("failed to set install map: %w", err)
	}

	// Update app entry
	app, exists := st.Apps[cfg.Repository]
	if !exists {
		app = &state.InstalledApp{
			Repository: cfg.Repository,
			TargetPath: cfg.TargetPath,
		}
	}

	app.Version = result.Version
	app.CompileScript = cfg.ScriptPath
	app.SymlinkDir = cfg.SymlinkDir
	app.Sidecars = strings.Join(result.Sidecars, ",")

	st.Apps[cfg.Repository] = app

	return st.Save()
}

// formatDependencies creates a comma-separated list of dependencies.
func formatDependencies(deps []ai.Dependency) string {
	var names []string
	for _, dep := range deps {
		names = append(names, dep.Name)
	}
	return strings.Join(names, ", ")
}

// Clean removes the build directory and compile artifacts.
func Clean(repoDir string) error {
	if err := safety.RemoveAll(repoDir); err != nil {
		return fmt.Errorf("failed to remove build directory: %w", err)
	}
	return nil
}

// GetBuildPath returns the build directory for a repository.
func GetBuildPath(repo string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	parts := strings.Split(repo, "/")
	if len(parts) < 2 {
		return ""
	}
	repoName := parts[len(parts)-1]
	return filepath.Join(homeDir, "builds", repoName)
}

// ParseManifest reads and parses a manifest.json file.
func ParseManifest(manifestPath string) (*ai.Manifest, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest ai.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	return &manifest, nil
}

// WriteManifest writes a manifest to file.
func WriteManifest(manifestPath string, manifest *ai.Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
		return fmt.Errorf("failed to create manifest directory: %w", err)
	}

	return os.WriteFile(manifestPath, data, 0644)
}
