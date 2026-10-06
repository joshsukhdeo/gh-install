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
	"github.com/joshsukhdeo/gh-pt/selector"
	"github.com/joshsukhdeo/gh-pt/state"
)

var symlinkFunc = os.Symlink

func createSymlinkOrCopy(srcPath, destPath string) error {
	if err := symlinkFunc(srcPath, destPath); err == nil {
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

	if err := os.MkdirAll(symlinkDir, 0755); err != nil {
		return "", err
	}

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

		if _, err := os.Lstat(destPath); err == nil {
			if r.CliParams.Overwrite || r.CliParams.IsUpgradeCmd {
				if err := os.Remove(destPath); err != nil {
					log.Warn("failed to remove existing file before overwrite", "error", err, "path", destPath)
				}
			} else {
				return "", fmt.Errorf("%s already exists; use force to overwrite", destPath)
			}
		}

		if err := createSymlinkOrCopy(srcPath, destPath); err != nil {
			return "", err
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

	return symlinkDir, nil
}

// symlinkSidecars symlinks sidecar files to the appropriate destination based on --include-sidecars mode
func (r *GithubRelease) symlinkSidecars(symlinkDir string) error {
	mode := r.CliParams.SidecarMode
	if mode == "" || mode == "auto" {
		mode = "xdg_data_home"
	}

	// Handle local-map mode separately
	if mode == "local-map" {
		return r.symlinkLocalMap(symlinkDir)
	}

	// Determine sidecar destination based on mode
	sidecarDest, err := r.resolveSidecarSymlinkDest()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(sidecarDest, 0755); err != nil {
		return err
	}

	// Get the sidecar regex pattern
	sidecarRegex := r.CliParams.Sidecars
	if sidecarRegex == "" {
		// Use default pattern based on destination
		sidecarRegex = r.getDefaultSidecarRegex(sidecarDest)
	}

	regex, err := regexp.Compile(sidecarRegex)
	if err != nil {
		return fmt.Errorf("invalid sidecar regex: %w", err)
	}

	// Walk through symlinkDir and find sidecar files
	err = filepath.WalkDir(symlinkDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(symlinkDir, path)
		if err != nil {
			return nil
		}

		// Check if this file matches the sidecar regex
		if isLicenseFileName(path) || isLicenseFileName(d.Name()) || isLicenseFileName(relPath) {
			return nil
		}
		if regex.MatchString(relPath) || regex.MatchString(d.Name()) {
			// This is a sidecar, symlink it to the destination
			destPath := filepath.Join(sidecarDest, d.Name())

			// Remove existing symlink if it exists
			if _, err := os.Lstat(destPath); err == nil {
				if r.CliParams.Overwrite || r.CliParams.IsUpgradeCmd {
					if err := os.Remove(destPath); err != nil {
						log.Warn("failed to remove existing sidecar symlink", "error", err, "path", destPath)
						return nil
					}
				} else {
					log.Warn("sidecar symlink already exists, skipping", "path", destPath)
					return nil
				}
			}

			if err := createSymlinkOrCopy(path, destPath); err != nil {
				log.Warn("failed to create sidecar symlink or copy", "error", err, "src", path, "dest", destPath)
				return nil
			}

			log.Info("created sidecar symlink", "src", path, "dest", destPath)
			r.InstalledSidecars = append(r.InstalledSidecars, destPath)
		}

		return nil
	})

	return err
}

// symlinkLocalMap maps standard Unix directories to /usr/local/ equivalents
// For example: ~/src/apps/owner/repo/bin/* -> /usr/local/bin/*
func (r *GithubRelease) symlinkLocalMap(symlinkDir string) error {
	// Standard Unix directories to map
	standardDirs := []string{"bin", "include", "lib", "lib64", "libs", "share", "etc", "var"}

	for _, dir := range standardDirs {
		srcDir := filepath.Join(symlinkDir, dir)

		// Check if this directory exists in the symlinkDir
		if _, err := os.Stat(srcDir); os.IsNotExist(err) {
			continue
		}

		// Determine the target directory in /usr/local/
		// Map "libs" to "lib" for consistency
		targetDirName := dir
		if dir == "libs" {
			targetDirName = "lib"
		}
		targetDir := filepath.Join("/usr/local", targetDirName)

		// Create target directory if it doesn't exist
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			log.Warn("failed to create target directory", "path", targetDir, "error", err)
			continue
		}

		// Read all items directly in the source directory
		entries, err := os.ReadDir(srcDir)
		if err != nil {
			log.Warn("failed to read directory", "path", srcDir, "error", err)
			continue
		}

		// Symlink each item directly in the directory
		for _, entry := range entries {
			if isLicenseFileName(entry.Name()) {
				continue
			}
			srcPath := filepath.Join(srcDir, entry.Name())
			destPath := filepath.Join(targetDir, entry.Name())

			// Remove existing symlink/file if it exists
			if _, err := os.Lstat(destPath); err == nil {
				if r.CliParams.Overwrite || r.CliParams.IsUpgradeCmd {
					if err := os.Remove(destPath); err != nil {
						log.Warn("failed to remove existing file/symlink", "path", destPath, "error", err)
						continue
					}
				} else {
					log.Warn("file/symlink already exists, skipping", "path", destPath)
					continue
				}
			}

			// Create symlink
			if err := createSymlinkOrCopy(srcPath, destPath); err != nil {
				log.Warn("failed to create symlink or copy", "src", srcPath, "dest", destPath, "error", err)
				continue
			}

			log.Info("created local-map symlink", "src", srcPath, "dest", destPath)
			r.InstalledSidecars = append(r.InstalledSidecars, destPath)
		}
	}

	return nil
}

// resolveSidecarSymlinkDest determines where sidecars should be symlinked based on --sidecar-mode
func (r *GithubRelease) resolveSidecarSymlinkDest() (string, error) {
	mode := r.CliParams.SidecarMode
	if mode == "" || mode == "auto" {
		mode = "xdg_data_home"
	}

	switch {
	case mode == "same_dest":
		// Symlink sidecars to the same directory as binaries (TargetPath)
		return r.CliParams.TargetPath, nil
	case mode == "xdg_data_home":
		// Symlink sidecars to XDG data home
		return filepath.Join(xdg.DataHome, "gh-pt", "sidecars", r.CliParams.Repository), nil
	case mode == "bin":
		// Symlink sidecars to bin directory
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(homeDir, ".local", "bin"), nil
	case strings.HasPrefix(mode, "custom-path:"):
		// Extract custom path
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
