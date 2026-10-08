package release

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/adrg/xdg"
	"github.com/charmbracelet/log"
	"github.com/joshsukhdeo/gh-pt/config"
	"github.com/joshsukhdeo/gh-pt/safety"
	"github.com/joshsukhdeo/gh-pt/selector"
	"github.com/joshsukhdeo/gh-pt/state"
)

var symlinkFunc = os.Symlink

// createSymlinkAtomic creates a symlink atomically to avoid TOCTOU race conditions.
// It creates the symlink with a temporary name and then atomically renames it into place.
// If symlink fails (e.g., on Windows without privilege), it falls back to hardlink or copy.
func createSymlinkAtomic(src, dest string) error {
	// Create parent directory if needed
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	// Generate a temporary name in the same directory
	tmpDest := dest + ".tmp." + filepath.Base(dest)

	// Create the symlink with temporary name
	if err := symlinkFunc(src, tmpDest); err == nil {
		// Atomically rename the temporary symlink to the final destination
		// This replaces any existing file/symlink atomically
		if err := os.Rename(tmpDest, dest); err != nil {
			// Clean up temp file on failure
			_ = os.Remove(tmpDest)
			return err
		}
		return nil
	}

	// Symlink failed - fall back to hardlink or copy (like createSymlinkOrCopy did)
	// Clean up temp file if it exists
	_ = os.Remove(tmpDest)

	// Attempt hardlink fallback where appropriate
	if err := os.Link(src, dest); err == nil {
		return nil
	}

	// Fallback to copying file
	info, err := os.Stat(src)
	mode := os.FileMode(0755)
	if err == nil {
		mode = info.Mode()
	}

	return copyFile(src, dest, mode)
}

func createSymlinkOrCopy(srcPath, destPath string) error {
	if err := createSymlinkAtomic(srcPath, destPath); err == nil {
		return nil
	} else {
		log.Warn("failed to create symlink, attempting fallback", "error", err, "src", srcPath, "dest", destPath)
	}

	_ = os.Remove(destPath)

	// Attempt hardlink fallback where appropriate
	if err := os.Link(srcPath, destPath); err == nil {
		log.Info("created hardlink fallback instead of symlink", "src", srcPath, "dest", destPath)
		return nil
	}

	// Fallback to copying file
	info, err := os.Stat(srcPath)
	mode := os.FileMode(0755)
	if err == nil {
		mode = info.Mode()
	}

	if err := copyFile(srcPath, destPath, mode); err != nil {
		return fmt.Errorf("failed to create symlink, hardlink, or copy file from %s to %s: %w", srcPath, destPath, err)
	}

	log.Info("copied file fallback instead of symlink", "src", srcPath, "dest", destPath)
	return nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_ = os.Remove(target) // Ignore error, will fail if it's a directory but that's fine
			if err := symlinkFunc(link, target); err != nil {
				resolvedPath := link
				if !filepath.IsAbs(resolvedPath) {
					resolvedPath = filepath.Join(filepath.Dir(path), link)
				}
				if fi, statErr := os.Stat(resolvedPath); statErr == nil && !fi.IsDir() {
					return copyFile(resolvedPath, target, fi.Mode())
				}
				return err
			}
			return nil
		}
		_ = os.Remove(target) // Remove existing file before overwrite
		// Ensure parent directory is writable before we attempt to write
		_ = os.Chmod(filepath.Dir(target), 0755)
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if err := in.Close(); err != nil {
			log.Warn("failed to close source file", "error", err, "file", src)
		}
	}()
	_ = os.Remove(dst) // Prevent permission denied if existing file is read-only
	_ = os.Chmod(filepath.Dir(dst), 0755)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer func() {
		if err := out.Close(); err != nil {
			log.Warn("failed to close destination file", "error", err, "file", dst)
		}
	}()
	_, err = io.Copy(out, in)
	return err
}

func copyFS(fileSystem fs.FS, dst string) error {
	return fs.WalkDir(fileSystem, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dst, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		in, err := fileSystem.Open(path)
		if err != nil {
			return err
		}
		defer func() {
			if err := in.Close(); err != nil {
				log.Warn("failed to close source file", "error", err, "file", path)
			}
		}()
		_ = os.Remove(target) // Prevent permission denied if existing file is read-only
		_ = os.Chmod(filepath.Dir(target), 0755)
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer func() {
			if err := out.Close(); err != nil {
				log.Warn("failed to close destination file", "error", err, "file", target)
			}
		}()
		_, err = io.Copy(out, in)
		return err
	})
}

// resolvePackageDir determines the canonical extraction path for an application package.
// Per ADR-004, the standard location is $XDG_DATA_HOME/gh-pt/packages/{ownerID}/{repoID}.
// Falls back to explicitly configured paths (cfg.Paths.ClonePath or cfg.Paths.PackagePath) or
// checks state (state.LoadState()) to see if an app with this repository was actually previously installed there.
func (r *GithubRelease) resolvePackageDir(ownerID, repoID string) string {
	// 1. Explicitly configured paths in config
	cfg, err := config.LoadConfig()
	if err == nil && cfg != nil {
		if cfg.Paths.PackagePath != "" {
			return filepath.Join(cfg.Paths.PackagePath, ownerID, repoID)
		}
		if cfg.Paths.ClonePath != "" {
			return filepath.Join(cfg.Paths.ClonePath, ownerID, repoID)
		}
	}

	homeDir, _ := os.UserHomeDir()
	legacyAppDir := filepath.Join(homeDir, "src", "apps", ownerID, repoID)

	// 2. Check state (state.LoadState()) to see if an app with this repository was actually previously installed there
	if st, err := state.LoadState(); err == nil && st != nil && st.Apps != nil {
		repoKey := repoID
		if ownerID != "" {
			repoKey = ownerID + "/" + repoID
		}
		var app *state.InstalledApp
		if a, ok := st.Apps[repoKey]; ok {
			app = a
		} else if r != nil && r.CliParams != nil && r.CliParams.Repository != "" {
			if a, ok := st.Apps[r.CliParams.Repository]; ok {
				app = a
			}
		} else if a, ok := st.Apps[repoID]; ok {
			app = a
		}

		if app != nil {
			if app.SymlinkDir != "" && filepath.Clean(app.SymlinkDir) == filepath.Clean(legacyAppDir) {
				return legacyAppDir
			}
			for _, sc := range app.InstalledSidecars {
				if strings.HasPrefix(filepath.Clean(sc), filepath.Clean(legacyAppDir)) {
					return legacyAppDir
				}
			}
			if app.SymlinkDir != "" {
				return app.SymlinkDir
			}
		}
	}

	// 3. Canonical ADR-004 path
	return filepath.Join(xdg.DataHome, "gh-pt", "packages", ownerID, repoID)
}

func (r *GithubRelease) executeSymlinkInstall(binaries []*selector.SelectorItem, assetPath string) (string, error) {
	if len(binaries) == 0 {
		return "", fmt.Errorf("no binaries to symlink")
	}

	// Parse owner and repo from repository string
	parts := strings.Split(r.CliParams.Repository, "/")
	var ownerID, repoID string
	if len(parts) >= 2 {
		ownerID = parts[0]
		repoID = parts[1]
	} else if len(parts) == 1 {
		ownerID = ""
		repoID = parts[0]
	}

	symlinkDir := r.resolvePackageDir(ownerID, repoID)

	if r.CliParams.DryRun {
		for _, binary := range binaries {
			destPath := r.resolveDestinationPath(binary.Name)
			srcPath := filepath.Join(symlinkDir, binary.Name)
			r.InstalledBinaries = append(r.InstalledBinaries, filepath.Base(destPath))
			r.InstalledFiles = append(r.InstalledFiles, srcPath)
			r.InstalledSymlinks = append(r.InstalledSymlinks, destPath)
			log.Infof("[dry-run] Would symlink %s -> %s", destPath, srcPath)
		}
		return symlinkDir, nil
	}

	if err := os.MkdirAll(symlinkDir, 0755); err != nil {
		return "", err
	}
	// Mark directory as managed by gh-pt for safe removal later
	_ = safety.WriteGhptManagedMarker(symlinkDir)

	// Copy all files to symlinkDir
	if binaries[0].ExtractDir != "" {
		if err := copyDir(binaries[0].ExtractDir, symlinkDir); err != nil {
			return "", err
		}
	} else if binaries[0].Compressed && binaries[0].Fs != nil {
		if err := copyFS(binaries[0].Fs, symlinkDir); err != nil {
			return "", err
		}
	} else {
		// Single file asset
		targetFile := filepath.Join(symlinkDir, filepath.Base(assetPath))
		if err := copyFile(assetPath, targetFile, 0755); err != nil {
			return "", err
		}
	}

	// Symlink binaries to bin directory
	for _, binary := range binaries {
		var srcPath string
		if binary.ExtractDir != "" {
			// Find relative path from ExtractDir
			rel, err := filepath.Rel(binary.ExtractDir, binary.DownloadPath)
			if err != nil {
				rel = binary.Name
			}
			srcPath = filepath.Join(symlinkDir, rel)
		} else if binary.Compressed {
			srcPath = filepath.Join(symlinkDir, binary.FsPath)
		} else {
			srcPath = filepath.Join(symlinkDir, filepath.Base(assetPath))
		}

		destPath := r.resolveDestinationPath(binary.Name)

		// Use atomic symlink creation to avoid TOCTOU race condition
		if err := createSymlinkAtomic(srcPath, destPath); err != nil {
			// If atomic creation fails, check if it's because the file already exists
			if _, statErr := os.Lstat(destPath); statErr == nil {
				if r.CliParams.Overwrite || r.CliParams.IsUpgradeCmd {
					if removeErr := os.Remove(destPath); removeErr != nil {
						log.Warn("failed to remove existing file before overwrite", "error", removeErr, "path", destPath)
					}
					// Retry atomic creation
					if retryErr := createSymlinkAtomic(srcPath, destPath); retryErr != nil {
						return "", retryErr
					}
				} else {
					return "", fmt.Errorf("%s already exists; use force to overwrite", destPath)
				}
			} else {
				return "", err
			}
		}
		r.InstalledFiles = append(r.InstalledFiles, srcPath)
		r.InstalledSymlinks = append(r.InstalledSymlinks, destPath)

		if r.UI != nil {
			r.UI.Update(6, r.ResolvedVersion, filepath.Base(assetPath), binary.Name, destPath, "")
		} else {
			log.Info("processing selected release asset binary for symlink",
				"repository", r.CliParams.Repository,
				"release name", r.ResolvedVersion,
				"release asset name", filepath.Base(assetPath),
				"release asset binary", binary.Name)

			log.Infof("will install symlink %s -> %s", destPath, srcPath)
		}

		r.InstalledBinaries = append(r.InstalledBinaries, filepath.Base(destPath))

		noComp := false
		if r.CliParams != nil {
			noComp = r.CliParams.NoCompletionSetup
		}
		ScanAndInstallArchiveCompletions(symlinkDir, filepath.Base(destPath), noComp)
		_ = SetupBinaryCompletions(destPath, noComp)
	}

	// Handle sidecar symlinking if --include-sidecars is set
	if r.CliParams.IncludeSidecars {
		if err := r.symlinkSidecars(symlinkDir); err != nil {
			log.Warn("failed to symlink sidecars", "error", err)
		}
	}

	// Deploy driver and plugin manifests if --driver or --plugin is set
	if (r.CliParams.Driver != "" && r.CliParams.Driver != "none") || (r.CliParams.Plugin != "" && r.CliParams.Plugin != "none") {
		manifests, err := r.DeployDriverAndPluginManifests(symlinkDir)
		if err != nil {
			log.Warn("driver/plugin manifest registration encountered warning", "error", err)
		} else if len(manifests) > 0 {
			r.InstalledSidecars = append(r.InstalledSidecars, manifests...)
			log.Infof("Registered %d driver/plugin manifests", len(manifests))
		}
	}

	return symlinkDir, nil
}

// hasLocalMapLayout returns true if the extracted package contains standard Unix hierarchy directories.
func (r *GithubRelease) hasLocalMapLayout(symlinkDir string) bool {
	// Standard non-bin Unix hierarchy directories
	standardDirs := []string{"share", "include", "lib", "lib64", "libs", "etc", "var", "man"}
	for _, dir := range standardDirs {
		info, err := os.Stat(filepath.Join(symlinkDir, dir))
		if err == nil && info.IsDir() {
			return true
		}
	}

	// Check if bin directory contains auxiliary files/scripts other than the primary installed binaries
	binDir := filepath.Join(symlinkDir, "bin")
	if info, err := os.Stat(binDir); err == nil && info.IsDir() {
		entries, err := os.ReadDir(binDir)
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() && !isLicenseFileName(entry.Name()) {
					isPrimary := false
					for _, installed := range r.InstalledBinaries {
						if entry.Name() == installed {
							isPrimary = true
							break
						}
					}
					if !isPrimary {
						return true
					}
				}
			}
		}
	}
	return false
}

// symlinkSidecars symlinks sidecar files to the appropriate destination based on --include-sidecars mode
func (r *GithubRelease) symlinkSidecars(symlinkDir string) error {
	mode := ""
	if r.CliParams != nil {
		mode = r.CliParams.SidecarMode
	}
	if mode == "" || mode == "auto" {
		if r.hasLocalMapLayout(symlinkDir) {
			mode = "local-map"
		} else {
			mode = "xdg_data_home"
		}
	}

	// Handle local-map mode
	if mode == "local-map" {
		return r.symlinkLocalMap(symlinkDir)
	}

	if mode == "bin" || mode == "same_dest" {
		log.Warn("sidecar-mode 'bin'/'same_dest' dumps non-binaries into binary directory; consider 'auto' or 'local-map'")
	}

	sidecarDest, err := r.resolveSidecarSymlinkDest(mode)
	if err != nil {
		return err
	}

	return r.symlinkSidecarsToDest(symlinkDir, sidecarDest)
}

// symlinkSidecarsToDest symlinks files matching sidecar regex from symlinkDir into sidecarDest
func (r *GithubRelease) symlinkSidecarsToDest(symlinkDir, sidecarDest string) error {
	if err := os.MkdirAll(sidecarDest, 0755); err != nil {
		return err
	}
	// Mark directory as managed by gh-pt for safe removal later
	_ = safety.WriteGhptManagedMarker(sidecarDest)

	sidecarRegex := ""
	if r.CliParams != nil {
		sidecarRegex = r.CliParams.Sidecars
	}
	if sidecarRegex == "" {
		sidecarRegex = r.getDefaultSidecarRegex(sidecarDest)
	}

	regex, err := regexp.Compile(sidecarRegex)
	if err != nil {
		return fmt.Errorf("invalid sidecar regex: %w", err)
	}

	return filepath.WalkDir(symlinkDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(symlinkDir, path)
		if err != nil {
			return nil
		}

		if isLicenseFileName(path) || isLicenseFileName(d.Name()) || isLicenseFileName(relPath) {
			return nil
		}
		if regex.MatchString(relPath) || regex.MatchString(d.Name()) {
			destPath := filepath.Join(sidecarDest, d.Name())

			// Use atomic symlink creation to avoid TOCTOU race condition
			if err := createSymlinkAtomic(path, destPath); err != nil {
				// If atomic creation fails, check if it's because the file already exists
				if _, statErr := os.Lstat(destPath); statErr == nil {
					if r.CliParams != nil && (r.CliParams.Overwrite || r.CliParams.IsUpgradeCmd) {
						if removeErr := os.Remove(destPath); removeErr != nil {
							log.Warn("failed to remove existing sidecar symlink", "error", removeErr, "path", destPath)
							return nil
						}
						// Retry atomic creation
						if retryErr := createSymlinkAtomic(path, destPath); retryErr != nil {
							log.Warn("failed to create sidecar symlink or copy", "error", retryErr, "src", path, "dest", destPath)
							return nil
						}
					} else {
						log.Warn("sidecar symlink already exists, skipping", "path", destPath)
						return nil
					}
				} else {
					log.Warn("failed to create sidecar symlink or copy", "error", err, "src", path, "dest", destPath)
					return nil
				}
			}

			log.Info("created sidecar symlink", "src", path, "dest", destPath)
			r.InstalledSidecars = append(r.InstalledSidecars, destPath)
		}

		return nil
	})
}

// symlinkLocalMap maps standard Unix directories to prefix equivalents (~/.local or /usr/local)
func (r *GithubRelease) symlinkLocalMap(symlinkDir string) error {
	basePrefix := ""
	if r.CliParams != nil && r.CliParams.TargetPath != "" {
		basePrefix = filepath.Dir(r.CliParams.TargetPath)
	}
	homeDir, _ := os.UserHomeDir()
	if (r.CliParams != nil && r.CliParams.Global) || os.Geteuid() == 0 {
		basePrefix = "/usr/local"
	} else if basePrefix == "." || basePrefix == "" || basePrefix == "/" || (r.CliParams != nil && basePrefix == filepath.Clean(r.CliParams.TargetPath)) {
		basePrefix = filepath.Join(homeDir, ".local")
	}

	standardDirs := []string{"bin", "include", "lib", "lib64", "libs", "share", "etc", "var", "man"}
	foundAny := false

	for _, dir := range standardDirs {
		srcDir := filepath.Join(symlinkDir, dir)
		info, err := os.Stat(srcDir)
		if err != nil || !info.IsDir() {
			continue
		}

		var targetDirName string
		switch dir {
		case "libs":
			targetDirName = "lib"
		case "man":
			targetDirName = filepath.Join("share", "man")
		default:
			targetDirName = dir
		}
		targetBase := filepath.Join(basePrefix, targetDirName)

		if dir == "bin" && r.CliParams != nil && r.CliParams.TargetPath != "" {
			targetBase = r.CliParams.TargetPath
		}

		_ = filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil || d.IsDir() {
				return nil
			}
			if isLicenseFileName(d.Name()) {
				return nil
			}

			relPath, err := filepath.Rel(srcDir, path)
			if err != nil {
				return nil
			}
			destPath := filepath.Join(targetBase, relPath)

			// Skip if already installed as a primary binary
			for _, installed := range r.InstalledSymlinks {
				if filepath.Clean(installed) == filepath.Clean(destPath) {
					return nil
				}
			}

			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				log.Warn("failed to create destination directory", "dir", filepath.Dir(destPath), "error", err)
				return nil
			}

			// Use atomic symlink creation to avoid TOCTOU race condition
			if err := createSymlinkAtomic(path, destPath); err != nil {
				// If atomic creation fails, check if it's because the file already exists
				if _, statErr := os.Lstat(destPath); statErr == nil {
					if r.CliParams != nil && (r.CliParams.Overwrite || r.CliParams.IsUpgradeCmd) {
						if removeErr := os.Remove(destPath); removeErr != nil {
							log.Warn("failed to remove existing sidecar target", "dest", destPath, "error", removeErr)
							return nil
						}
						// Retry atomic creation
						if retryErr := createSymlinkAtomic(path, destPath); retryErr != nil {
							log.Warn("failed to create local-map symlink", "src", path, "dest", destPath, "error", retryErr)
							return nil
						}
					} else {
						log.Warn("sidecar target already exists, skipping", "dest", destPath)
						return nil
					}
				} else {
					log.Warn("failed to create local-map symlink", "src", path, "dest", destPath, "error", err)
					return nil
				}
			}

			foundAny = true
			log.Info("created local-map symlink", "src", path, "dest", destPath)
			r.InstalledSidecars = append(r.InstalledSidecars, destPath)
			return nil
		})
	}

	if !foundAny {
		log.Info("no standard Unix hierarchy directories found for local-map, falling back to xdg_data_home", "repo", r.CliParams.Repository)
		sidecarDest, err := r.resolveSidecarSymlinkDest("xdg_data_home")
		if err != nil {
			return err
		}
		return r.symlinkSidecarsToDest(symlinkDir, sidecarDest)
	}

	return nil
}

// resolveSidecarSymlinkDest maps a specific sidecar mode to its destination directory
func (r *GithubRelease) resolveSidecarSymlinkDest(mode string) (string, error) {
	switch {
	case mode == "same_dest":
		if r.CliParams != nil {
			return r.CliParams.TargetPath, nil
		}
		return "", fmt.Errorf("no target path set")
	case mode == "xdg_data_home":
		repo := ""
		if r.CliParams != nil {
			repo = r.CliParams.Repository
		}
		return filepath.Join(xdg.DataHome, "gh-pt", "sidecars", repo), nil
	case mode == "bin":
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(homeDir, ".local", "bin"), nil
	case strings.HasPrefix(mode, "custom-path:"):
		return strings.TrimPrefix(mode, "custom-path:"), nil
	default:
		return "", fmt.Errorf("unknown sidecar mode: %s", mode)
	}
}

// getDefaultSidecarRegex returns a default regex pattern based on the destination or sidecar mode
func (r *GithubRelease) getDefaultSidecarRegex(destPath string) string {
	binRegex := `\.so.*|\.dll|\.dylib|\.exe$`
	xdgRegex := `\.so.*|\.h$|\.hpp$|\.c$|\.cpp$|\.txt$|README.*|\.md$|\.json$|\.yaml$|\.yml$|\.toml$|\.conf$`
	customRegex := `\.so.*|\.h$|\.dll|\.dylib|\.txt$|README.*`

	var mode string
	if r != nil && r.CliParams != nil {
		mode = r.CliParams.SidecarMode
	}

	// 1. Explicit sidecar modes
	switch {
	case mode == "bin":
		return binRegex
	case mode == "xdg_data_home":
		return xdgRegex
	case strings.HasPrefix(mode, "custom-path:"):
		return customRegex
	}

	// 2. Classify based on target directory / destPath
	cleanDest := filepath.Clean(destPath)
	homeDir, _ := os.UserHomeDir()

	// Bin destinations: match executables/libraries
	if filepath.Base(cleanDest) == "bin" ||
		(homeDir != "" && cleanDest == filepath.Join(homeDir, ".local", "bin")) ||
		cleanDest == "/usr/bin" ||
		cleanDest == "/usr/local/bin" {
		return binRegex
	}

	// Data/XDG destinations: match libraries, headers, configs, docs
	if filepath.Base(cleanDest) == "share" ||
		(homeDir != "" && strings.HasPrefix(cleanDest, filepath.Join(homeDir, ".local", "share"))) ||
		(xdg.DataHome != "" && (cleanDest == filepath.Clean(xdg.DataHome) || strings.HasPrefix(cleanDest, filepath.Clean(xdg.DataHome)+string(filepath.Separator)))) {
		return xdgRegex
	}

	for _, part := range strings.Split(cleanDest, string(filepath.Separator)) {
		if part == "share" {
			return xdgRegex
		}
	}

	// 3. Fallback for custom paths
	return customRegex
}
