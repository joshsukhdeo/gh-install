package release

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/joshsukhdeo/gh-pt/selector"
)

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
			return os.Symlink(link, target)
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

func (r *GithubRelease) executeSymlinkInstall(binaries []*selector.SelectorItem, assetPath string) (string, error) {
	if len(binaries) == 0 {
		return "", fmt.Errorf("no binaries to symlink")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
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

	// Always use ~/src/apps/{ownerID}/{repoID}
	symlinkDir := filepath.Join(homeDir, "src", "apps", ownerID, repoID)

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

		if err := os.Symlink(srcPath, destPath); err != nil {
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
	if r.CliParams.IncludeSidecars != "" {
		if err := r.symlinkSidecars(symlinkDir); err != nil {
			log.Warn("failed to symlink sidecars", "error", err)
		}
	}

	return symlinkDir, nil
}

// symlinkSidecars symlinks sidecar files to the appropriate destination based on --include-sidecars mode
func (r *GithubRelease) symlinkSidecars(symlinkDir string) error {
	mode := r.CliParams.IncludeSidecars

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

			if err := os.Symlink(path, destPath); err != nil {
				log.Warn("failed to create sidecar symlink", "error", err, "src", path, "dest", destPath)
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
			if err := os.Symlink(srcPath, destPath); err != nil {
				log.Warn("failed to create symlink", "src", srcPath, "dest", destPath, "error", err)
				continue
			}

			log.Info("created local-map symlink", "src", srcPath, "dest", destPath)
			r.InstalledSidecars = append(r.InstalledSidecars, destPath)
		}
	}

	return nil
}

// resolveSidecarSymlinkDest determines where sidecars should be symlinked based on --include-sidecars mode
func (r *GithubRelease) resolveSidecarSymlinkDest() (string, error) {
	mode := r.CliParams.IncludeSidecars

	switch {
	case mode == "same_dest":
		// Symlink sidecars to the same directory as binaries (TargetPath)
		return r.CliParams.TargetPath, nil
	case mode == "xdg_data_home":
		// Symlink sidecars to XDG data home
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(homeDir, ".local", "share", r.CliParams.Repository), nil
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
		return "", fmt.Errorf("unknown include-sidecars mode: %s", mode)
	}
}

// getDefaultSidecarRegex returns a default regex pattern based on the destination
func (r *GithubRelease) getDefaultSidecarRegex(destPath string) string {
	// Determine appropriate regex based on destination
	switch {
	case strings.Contains(destPath, ".local/bin") || strings.Contains(destPath, "/usr/bin"):
		// For bin directories, match executables and libraries
		return `\.so.*|\.dll|\.dylib|\.exe$`
	case strings.Contains(destPath, ".local/share") || strings.Contains(destPath, "xdg"):
		// For XDG data home, match libraries, headers, configs, docs
		return `\.so.*|\.h$|\.hpp$|\.c$|\.cpp$|\.txt$|README.*|\.md$|\.json$|\.yaml$|\.yml$|\.toml$|\.conf$`
	default:
		// Default pattern for custom paths
		return `\.so.*|\.h$|\.dll|\.dylib|\.txt$|README.*`
	}
}
