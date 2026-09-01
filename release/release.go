package release

import (
	"bufio"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/cli/go-gh/v2"
	"github.com/joshsukhdeo/gh-install/params"
	"github.com/joshsukhdeo/gh-install/selector"
	"github.com/joshsukhdeo/gh-install/state"
	"github.com/pterm/pterm"
	"github.com/rs/zerolog/log"
)

var (
	execCommand = exec.Command
	ghExec      = gh.Exec
)

type GithubRelease struct {
	CliParams             *params.CLI
	Client                selector.GithubClient
	ResolvedVersion       string
	InstalledPackageNames []string
	PendingDebs           []string
	PendingRpms           []string
}

func MakeGithubRelease(cliParams *params.CLI, cli selector.GithubClient) *GithubRelease {

	return &GithubRelease{
		CliParams: cliParams,
		Client:    cli,
	}
}

func (r *GithubRelease) interactiveConfirm(prompt string) bool {
	if r.CliParams.DisablePrompts {
		return true
	}
	result, _ := pterm.DefaultInteractiveConfirm.
		WithDefaultValue(true).
		Show(prompt)
	return result
}

func (r *GithubRelease) interactiveInput(prompt string, defaultValue string) string {
	if r.CliParams.DisablePrompts {
		return defaultValue
	}
	result, _ := pterm.DefaultInteractiveTextInput.
		WithDefaultValue(defaultValue).
		Show(prompt)
	return result
}

func (r *GithubRelease) resolveDestinationPath(binaryPath string) string {
	binaryName := filepath.Base(binaryPath)
	destinationPath := filepath.Join(r.CliParams.TargetPath, binaryName)

	if targetBinaryName, exists := r.CliParams.Rename[strings.ToLower(binaryName)]; exists {
		return filepath.Join(r.CliParams.TargetPath, targetBinaryName)
	}

	if r.CliParams.PromptRename && !r.CliParams.DisablePrompts {
		repoParts := strings.Split(r.CliParams.Repository, "/")
		repoName := repoParts[len(repoParts)-1]

		proposedName := GenerateCleanName(binaryName, repoName, r.ResolvedVersion)

		if extMatch := regexp.MustCompile(`(?i)(\.[a-z][a-z0-9]*)$`).FindStringSubmatch(binaryName); len(extMatch) > 0 {
			ext := extMatch[1]
			if !strings.HasSuffix(proposedName, ext) {
				proposedName += ext
			}
		}

		if len(binaryName) > len(proposedName)+3 { // If it has significant affixes
			if r.interactiveConfirm(fmt.Sprintf("Binary name '%s' is long. Do you want to strip OS/hardware affixes?", binaryName)) {
				newName := r.interactiveInput(fmt.Sprintf("Rename '%s' to", binaryName), proposedName)
				if newName != "" {
					return filepath.Join(r.CliParams.TargetPath, newName)
				}
			}
		}
	}

	if r.CliParams.Interactive && !r.CliParams.DisablePrompts {
		return r.interactiveInput(fmt.Sprintf("Install %s to", binaryPath), destinationPath)
	}

	return destinationPath
}

func (r *GithubRelease) installArchivedBinary(fileSystem fs.FS, binaryPath string) error {
	if r.CliParams.DryRun {
		log.Info().Msgf("[dry-run] Would extract and install: %s", binaryPath)
		return nil
	}
	sourceFile, err := fileSystem.Open(binaryPath)
	if err != nil {
		return err
	}

	defer func() {
		err = errors.Join(err, sourceFile.Close())
	}()

	destinationPath := r.resolveDestinationPath(binaryPath)

	log.Info().
		Msgf("will install %s to %s", binaryPath, destinationPath)

	_, err = os.Stat(destinationPath)
	if err == nil {
		if r.CliParams.Interactive {
			if !r.interactiveConfirm(fmt.Sprintf("'%s' already exists. Overwrite?", destinationPath)) {
				return fmt.Errorf("%s already exists and user did not want to overwrite", destinationPath)
			}
		} else {
			if !r.CliParams.Overwrite {
				return fmt.Errorf("%s already exists and --target-binaries-overwrite is not set", destinationPath)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	destinationFile, err := os.Create(destinationPath)
	if err != nil {
		return err
	}

	defer func() {
		err = errors.Join(err, destinationFile.Close())
	}()

	_, err = io.Copy(destinationFile, sourceFile)
	if err != nil {
		return err
	}

	err = os.Chmod(destinationPath, 0755)
	if err != nil {
		return err
	}

	return nil
}

func (r *GithubRelease) installBinary(binaryPath string) error {
	if r.CliParams.DryRun {
		log.Info().Msgf("[dry-run] Would install binary: %s", binaryPath)
		return nil
	}
	sourceStat, err := os.Stat(binaryPath)
	if err != nil {
		return err
	}

	if !sourceStat.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", binaryPath)
	}

	source, err := os.Open(binaryPath)
	if err != nil {
		return err
	}

	defer func() {
		err = errors.Join(err, source.Close())
	}()

	destinationPath := r.resolveDestinationPath(binaryPath)

	log.Info().
		Msgf("will install %s to %s", binaryPath, destinationPath)

	_, err = os.Stat(destinationPath)
	if err == nil {
		if r.CliParams.Interactive {
			if !r.interactiveConfirm(fmt.Sprintf("'%s' already exists. Overwrite?", destinationPath)) {
				return fmt.Errorf("%s already exists and user did not want to overwrite", destinationPath)
			}
		} else {
			if !r.CliParams.Overwrite {
				return fmt.Errorf("%s already exists and --target-binaries-overwrite is not set", destinationPath)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	destination, err := os.Create(destinationPath)
	if err != nil {
		return err
	}

	defer func() {
		err = errors.Join(err, destination.Close())
	}()

	_, err = io.Copy(destination, source)
	if err != nil {
		return err
	}

	err = os.Chmod(destinationPath, 0755)
	if err != nil {
		return err
	}

	return nil
}

func (r *GithubRelease) ensureSudo() error {
	check := execCommand("sudo", "-n", "true")
	if err := check.Run(); err != nil {
		if !r.CliParams.Interactive {
			return fmt.Errorf("sudo session expired or unavailable; cannot prompt for password in headless mode (-D). Run 'sudo -v' beforehand or use an interactive session")
		}
		log.Warn().Msg("sudo session not cached; you may be prompted for your password")
	}
	return nil
}

// extractPackageName queries the package name from a local package file.
func extractPackageName(binaryPath string, pkgType string) string {
	var cmd *exec.Cmd
	switch pkgType {
	case "deb":
		cmd = exec.Command("dpkg-deb", "--field", binaryPath, "Package")
	case "rpm":
		cmd = exec.Command("rpm", "-qp", binaryPath, "--queryformat", "%{NAME}")
	case "pacman":
		cmd = exec.Command("pacman", "-Qp", "--noconfirm", binaryPath)
	default:
		return ""
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(out))
	// pacman outputs "name version", take just the name
	if pkgType == "pacman" {
		parts := strings.Fields(name)
		if len(parts) > 0 {
			return parts[0]
		}
	}
	return name
}

func (r *GithubRelease) installDeb(binaryPath string) error {
	if r.CliParams.DryRun {
		log.Info().Msgf("[dry-run] Would queue deb for batch install: %s", filepath.Base(binaryPath))
		return nil
	}
	log.Info().Msgf("Queuing %s for batch installation...", filepath.Base(binaryPath))
	r.PendingDebs = append(r.PendingDebs, binaryPath)
	return nil
}

func (r *GithubRelease) installRpm(binaryPath string) error {
	if r.CliParams.DryRun {
		log.Info().Msgf("[dry-run] Would queue rpm for batch install: %s", filepath.Base(binaryPath))
		return nil
	}
	log.Info().Msgf("Queuing %s for batch installation...", filepath.Base(binaryPath))
	r.PendingRpms = append(r.PendingRpms, binaryPath)
	return nil
}

func getScore(name string, types []string) int {
	name = strings.ToLower(name)
	score := -1
	for i, t := range types {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "none" {
			if !strings.Contains(filepath.Base(name), ".") {
				score = (len(types) - i) * 10
				break
			}
		} else if t != "" {
			matched, _ := regexp.MatchString(`(?i)\.`+t+`$`, name)
			if matched {
				score = (len(types) - i) * 10
				break
			}
		}
	}

	if score == -1 {
		for i, t := range types {
			if strings.ToLower(strings.TrimSpace(t)) == "none" {
				score = (len(types) - i) * 10
				break
			}
		}
	}

	if score != -1 && runtime.GOOS == "linux" {
		isMusl, _ := regexp.MatchString(`[-_]musl[-_.]`, name)
		if isMusl {
			score -= 5
		} else {
			isGlibc, _ := regexp.MatchString(`[-_](?:glibc|gnu)[-_.]`, name)
			if isGlibc {
				score += 2
			}
		}
	}

	return score
}

// findChecksumFile looks for a checksum file in the release assets.
// Returns the asset name if found, empty string otherwise.
func (r *GithubRelease) findChecksumFile(assets []*selector.SelectorItem) string {
	// Common checksum file patterns
	patterns := []string{
		`(?i)checksums?\.txt$`,
		`(?i)sha256sums?\.txt$`,
		`(?i)sha512sums?\.txt$`,
		`(?i)checksums?$`,
	}

	for _, asset := range assets {
		for _, pattern := range patterns {
			if matched, _ := regexp.MatchString(pattern, asset.Name); matched {
				return asset.Name
			}
		}
	}
	return ""
}

// verifyChecksum verifies the checksum of a downloaded file against a checksum file.
func (r *GithubRelease) verifyChecksum(filePath, checksumFilePath string) error {
	// Read the checksum file
	file, err := os.Open(checksumFilePath)
	if err != nil {
		return fmt.Errorf("failed to open checksum file: %w", err)
	}
	defer func() { _ = file.Close() }()

	// Parse checksum file - format is typically: <hash>  <filename> or <hash> <filename>
	scanner := bufio.NewScanner(file)
	expectedHash := ""
	targetFilename := filepath.Base(filePath)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Split by whitespace - checksum files use "hash  filename" or "hash filename"
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			hashValue := parts[0]
			filename := parts[len(parts)-1]

			// Remove leading * or ./ from filename (binary mode indicator)
			filename = strings.TrimPrefix(filename, "*")
			filename = strings.TrimPrefix(filename, "./")

			if strings.EqualFold(filename, targetFilename) {
				expectedHash = hashValue
				break
			}
		}
	}

	if expectedHash == "" {
		log.Warn().
			Str("filename", targetFilename).
			Msg("no checksum entry found for file, skipping verification")
		return nil
	}

	// Determine hash algorithm based on hash length
	var hasher hash.Hash
	switch len(expectedHash) {
	case 64: // SHA-256
		hasher = sha256.New()
	case 128: // SHA-512
		hasher = sha512.New()
	default:
		log.Warn().
			Int("hash_length", len(expectedHash)).
			Msg("unknown hash algorithm, skipping verification")
		return nil
	}

	// Calculate the hash of the downloaded file
	downloadedFile, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open downloaded file: %w", err)
	}
	defer func() { _ = downloadedFile.Close() }()

	if _, err := io.Copy(hasher, downloadedFile); err != nil {
		return fmt.Errorf("failed to calculate hash: %w", err)
	}

	actualHash := hex.EncodeToString(hasher.Sum(nil))

	if !strings.EqualFold(actualHash, expectedHash) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	log.Info().
		Str("filename", targetFilename).
		Str("hash", actualHash).
		Msg("checksum verified successfully")

	return nil
}

func (r *GithubRelease) installWindows(binaryPath string) error {
	if r.CliParams.DryRun {
		log.Info().Msgf("[dry-run] Would install windows pkg: %s", binaryPath)
		return nil
	}
	var args []string
	if strings.HasSuffix(strings.ToLower(binaryPath), ".msi") {
		args = []string{"/i", binaryPath, "/qn"}
		if runtime.GOOS != "windows" && r.CliParams.AllowWine {
			args = append([]string{"msiexec"}, args...)
		} else {
			args = append([]string{"msiexec"}, args...)
		}
	} else {
		args = []string{"/S"}
		if runtime.GOOS != "windows" && r.CliParams.AllowWine {
			args = append([]string{binaryPath}, args...)
			args = append([]string{"wine"}, args...)
		} else {
			args = append([]string{binaryPath}, args...)
		}
	}

	cmd := execCommand(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		log.Error().Err(err).Msgf("Failed to install Windows package")
		return err
	}
	log.Info().Msg("Successfully installed Windows package!")
	return nil
}

func (r *GithubRelease) installMac(binaryPath string) error {
	if r.CliParams.DryRun {
		log.Info().Msgf("[dry-run] Would install mac pkg: %s", binaryPath)
		return nil
	}
	if strings.HasSuffix(strings.ToLower(binaryPath), ".dmg") {
		log.Info().Msg("Mounting DMG...")
		cmd := execCommand("hdiutil", "attach", binaryPath, "-nobrowse", "-mountpoint", "/Volumes/gh-install-dmg")
		if err := cmd.Run(); err != nil {
			return err
		}
		defer func() { _ = execCommand("hdiutil", "detach", "/Volumes/gh-install-dmg").Run() }()

		cpCmd := execCommand("sh", "-c", fmt.Sprintf("cp -R /Volumes/gh-install-dmg/*.app %s/", r.CliParams.TargetPath))
		if err := cpCmd.Run(); err != nil {
			return err
		}
		log.Info().Msg("Successfully copied app from DMG!")
		return nil
	} else if strings.HasSuffix(strings.ToLower(binaryPath), ".pkg") {
		if err := r.ensureSudo(); err != nil {
			return err
		}
		cmd := execCommand("sudo", "installer", "-pkg", binaryPath, "-target", "/")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}
		log.Info().Msg("Successfully installed macOS pkg!")
		return nil
	}
	return fmt.Errorf("unsupported mac installer format: %s", binaryPath)
}

func (r *GithubRelease) Install() error {
	releaseSelector, err := selector.ReleaseSelector(r.Client, r.CliParams.Repository, r.CliParams.ReleaseVersion, r.CliParams.Interactive)
	if err != nil {
		log.Error().
			Str("repository", r.CliParams.Repository).
			Str("release version", r.CliParams.ReleaseVersion).
			Err(err).
			Msg("could not create release selector")
		return err
	}
	releases, err := releaseSelector.Run()
	if err != nil {
		log.Error().
			Str("repository", r.CliParams.Repository).
			Str("release version", r.CliParams.ReleaseVersion).
			Err(err).
			Msg("could not select a release")
		return err
	}
	r.ResolvedVersion = releases[0].Name

	assetSelector, err := selector.AssetSelector(r.Client, r.CliParams.Repository, releases[0].Id,
		r.CliParams.ReleaseAsset, r.CliParams.ReleaseAssetRegexps, r.CliParams.Interactive, r.CliParams.AllowForeignArch)
	if err != nil {
		log.Error().
			Str("repository", r.CliParams.Repository).
			Int("release id", releases[0].Id).
			Str("release name", releases[0].Name).
			Str("asset name matcher", r.CliParams.ReleaseAsset).
			Err(err).
			Msg("could not create release asset selector")
		return err
	}
	assets, err := assetSelector.Run()
	if err != nil {
		log.Error().
			Str("repository", r.CliParams.Repository).
			Int("release id", releases[0].Id).
			Str("release asset name matcher", r.CliParams.ReleaseAsset).
			Err(err).
			Msg("could not select release asset")
		return err
	}

	sort.Slice(assets, func(i, j int) bool {
		scoreI := getScore(assets[i].Name, r.CliParams.Type)
		scoreJ := getScore(assets[j].Name, r.CliParams.Type)
		return scoreI > scoreJ
	})

	// Filter r.CliParams.Type down to only the formats that actually matched our chosen assets.
	// This ensures that when the state is saved, future updates will strictly seek this exact format.
	var matchedTypes []string
	for _, asset := range assets {
		for _, t := range r.CliParams.Type {
			originalT := t
			tLower := strings.ToLower(strings.TrimSpace(t))
			if tLower == "none" {
				if !strings.Contains(filepath.Base(asset.Name), ".") {
					matchedTypes = append(matchedTypes, originalT)
					break
				}
			} else if tLower != "" {
				if matched, _ := regexp.MatchString(`(?i)\.`+tLower+`$`, asset.Name); matched {
					matchedTypes = append(matchedTypes, originalT)
					break
				}
			}
		}
	}

	if len(matchedTypes) > 0 {
		seen := make(map[string]bool)
		var finalTypes []string
		for _, t := range matchedTypes {
			if !seen[t] {
				seen[t] = true
				finalTypes = append(finalTypes, t)
			}
		}
		r.CliParams.Type = finalTypes
	}

	// Generate strict regexes for the chosen assets to lock them down for future updates
	var strictRegexes []string
	for _, asset := range assets {
		strictRegex := generateStrictAssetRegex(asset.Name, releases[0].Name)
		strictRegexes = append(strictRegexes, strictRegex)
	}
	r.CliParams.ReleaseAssetRegexp = strings.Join(strictRegexes, " | ")

	downloadDir, err := os.MkdirTemp("", "*")
	if err != nil {
		log.Error().
			Err(err).
			Msg("could not create temporary download directory")
		return err
	}
	defer func() {
		err = errors.Join(err, os.RemoveAll(downloadDir))
	}()

	// Find and download checksum file if available
	var checksumFilePath string
	if r.CliParams.VerifyChecksum {
		// Get all release assets to find checksum file
		allAssetsResponse := []struct{ Name string }{}
		if err := r.Client.Get(fmt.Sprintf("repos/%s/releases/%d/assets", r.CliParams.Repository, releases[0].Id), &allAssetsResponse); err == nil {
			var allAssets []*selector.SelectorItem
			for _, a := range allAssetsResponse {
				allAssets = append(allAssets, &selector.SelectorItem{Name: a.Name})
			}

			checksumFileName := r.findChecksumFile(allAssets)
			if checksumFileName != "" {
				log.Info().
					Str("checksum_file", checksumFileName).
					Msg("found checksum file, downloading")

				_, _, execErr := ghExec("release", "download", releases[0].Name,
					"--repo", r.CliParams.Repository, "--pattern", checksumFileName, "--dir", downloadDir)
				if execErr != nil {
					log.Warn().
						Err(execErr).
						Msg("failed to download checksum file, skipping verification")
				} else {
					checksumFilePath = filepath.Join(downloadDir, checksumFileName)
				}
			}
		}
	}

	for _, asset := range assets {
		var downloadSpinner *pterm.SpinnerPrinter
		if r.CliParams.Interactive {
			downloadSpinner, _ = pterm.DefaultSpinner.Start(fmt.Sprintf("Downloading asset '%s'...", asset.Name))
		}

		stdOut, stdErr, execErr := ghExec("release", "download", releases[0].Name,
			"--repo", r.CliParams.Repository, "--pattern", asset.Name, "--dir", downloadDir)
		if execErr != nil {
			execErr = fmt.Errorf("failed to run gh command: %s", stdErr.String())
			log.Error().
				Str("repository", r.CliParams.Repository).
				Int("release id", releases[0].Id).
				Str("release name", releases[0].Name).
				Str("release asset name", asset.Name).
				Str("download directory", downloadDir).
				Err(execErr).
				Msg("could not download release asset")
			if r.CliParams.Interactive {
				downloadSpinner.Fail(fmt.Sprintf("Failed - '%s' failed", stdErr.String()))
			}
			return execErr
		}

		if r.CliParams.Interactive {
			downloadSpinner.Success("Downloaded!")
		}

		log.Info().
			Str("repository", r.CliParams.Repository).
			Int("release id", releases[0].Id).
			Str("release name", releases[0].Name).
			Str("release asset name", asset.Name).
			Str("download directory", downloadDir).
			Str("output", stdOut.String()).
			Msg("downloaded release asset")

		// Verify checksum if available
		if checksumFilePath != "" && r.CliParams.VerifyChecksum {
			downloadedAssetPath := filepath.Join(downloadDir, asset.Name)
			if err := r.verifyChecksum(downloadedAssetPath, checksumFilePath); err != nil {
				log.Error().
					Err(err).
					Str("asset", asset.Name).
					Msg("checksum verification failed")
				return fmt.Errorf("checksum verification failed for %s: %w", asset.Name, err)
			}
		}

		binarySelector, execErr := selector.BinarySelector(filepath.Join(downloadDir,
			asset.Name), r.CliParams.AssetBinaries, r.CliParams.AssetBinariesRegexp, r.CliParams.Interactive, r.CliParams.NativeExtract)
		if execErr != nil {
			log.Error().
				Str("repository", r.CliParams.Repository).
				Int("release id", releases[0].Id).
				Str("release name", releases[0].Name).
				Str("release asset name", asset.Name).
				Str("downloaded asset", filepath.Join(downloadDir, asset.Name)).
				Strs("asset binary name matchers", r.CliParams.AssetBinaries).
				Str("asset binary regexp matcher", r.CliParams.AssetBinariesRegexp).
				Err(execErr).
				Msg("could not create release asset binary selector")
			return execErr
		}
		binaries, execErr := binarySelector.Run()
		if execErr != nil {
			log.Error().
				Str("repository", r.CliParams.Repository).
				Int("release id", releases[0].Id).
				Str("release name", releases[0].Name).
				Str("release asset name", asset.Name).
				Str("downloaded asset", filepath.Join(downloadDir, asset.Name)).
				Strs("asset binary name matchers", r.CliParams.AssetBinaries).
				Str("asset binary regexp matcher", r.CliParams.AssetBinariesRegexp).
				Err(execErr).
				Msg("could not select release asset binary")
			return execErr
		}

		binariesOutput := make(map[string]string)
		for _, binary := range binaries {
			log.Info().
				Str("repository", r.CliParams.Repository).
				Int("release id", releases[0].Id).
				Str("release name", releases[0].Name).
				Str("release asset name", asset.Name).
				Str("downloaded asset", filepath.Join(downloadDir, asset.Name)).
				Str("release asset binary", binary.Name).
				Msg("processing selected release asset binary")

			actualDownloadPath := binary.DownloadPath

			if binary.Compressed && binary.BinaryType != selector.BinaryExecutable {
				log.Debug().
					Str("release asset binary", binary.Name).
					Msg("extracting archived installer to temporary directory")

				actualDownloadPath = filepath.Join(downloadDir, binary.Name)
				sourceFile, openErr := binary.Fs.Open(binary.FsPath)
				if openErr != nil {
					return openErr
				}
				destFile, createErr := os.Create(actualDownloadPath)
				if createErr != nil {
					_ = sourceFile.Close()
					return createErr
				}
				_, copyErr := io.Copy(destFile, sourceFile)
				_ = destFile.Close()
				_ = sourceFile.Close()
				if copyErr != nil {
					return copyErr
				}
			}

			switch binary.BinaryType {
			case selector.BinaryDebInstaller:
				log.Debug().Msg("binary is a deb installer")
				binariesOutput[binary.Name] = "deb"
				execErr = r.installDeb(actualDownloadPath)
			case selector.BinaryRpmInstaller:
				log.Debug().Msg("binary is a rpm installer")
				binariesOutput[binary.Name] = "rpm"
				execErr = r.installRpm(actualDownloadPath)
			case selector.BinaryPacmanInstaller:
				log.Debug().Msg("binary is a pacman installer")
				binariesOutput[binary.Name] = "pacman"
				execErr = r.installPacman(actualDownloadPath)
			case selector.BinaryPkgInstaller:
				log.Debug().Msg("binary is a freebsd pkg/txz installer")
				binariesOutput[binary.Name] = "pkg"
				execErr = r.installPkg(actualDownloadPath)
			case selector.BinaryMacInstaller:
				log.Debug().Msg("binary is a mac installer")
				binariesOutput[binary.Name] = "mac"
				execErr = r.installMac(actualDownloadPath)
			case selector.BinaryWindowsInstaller:
				log.Debug().Msg("binary is a windows installer")
				binariesOutput[binary.Name] = "windows"
				execErr = r.installWindows(actualDownloadPath)
			default:
				log.Debug().Msg("binary is a plain executable")
				binariesOutput[binary.Name] = "binary"
				if binary.Compressed {
					execErr = r.installArchivedBinary(binary.Fs, binary.FsPath)
				} else {
					execErr = r.installBinary(actualDownloadPath)
				}
			}

			if execErr != nil {
				log.Error().
					Str("repository", r.CliParams.Repository).
					Int("release id", releases[0].Id).
					Str("release name", releases[0].Name).
					Str("release asset name", asset.Name).
					Str("downloaded asset", filepath.Join(downloadDir, asset.Name)).
					Str("release asset binary", binary.Name).
					Err(execErr).
					Msg("could not install release asset binary")
				return execErr
			}
		}

		if !r.CliParams.All {
			break
		}
	}

	if len(r.PendingDebs) > 0 {
		if err := r.ensureSudo(); err != nil {
			return err
		}
		for _, p := range r.PendingDebs {
			if name := extractPackageName(p, "deb"); name != "" {
				r.InstalledPackageNames = append(r.InstalledPackageNames, name)
			}
		}
		var baseDebs []string
		for _, p := range r.PendingDebs {
			baseDebs = append(baseDebs, "./"+filepath.Base(p))
		}
		var args []string
		if r.CliParams.NoDeps {
			args = append([]string{"dpkg", "-i"}, baseDebs...)
		} else if r.CliParams.AddDeps {
			args = append([]string{"apt-get", "install", "-y"}, baseDebs...)
		} else {
			args = append([]string{"apt-get", "install"}, baseDebs...)
		}

		if r.CliParams.Interactive && !r.CliParams.DisablePrompts {
			if !r.interactiveConfirm(fmt.Sprintf("Run 'sudo %s'?", strings.Join(args, " "))) {
				return fmt.Errorf("user aborted batched .deb installation")
			}
		}

		cmd := execCommand("sudo", args...)
		cmd.Dir = filepath.Dir(r.PendingDebs[0])
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		log.Info().Msgf("Executing batched package install: sudo %s", strings.Join(args, " "))
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("batched .deb installation failed: %w", err)
		}
	}

	if len(r.PendingRpms) > 0 {
		if err := r.ensureSudo(); err != nil {
			return err
		}
		for _, p := range r.PendingRpms {
			if name := extractPackageName(p, "rpm"); name != "" {
				r.InstalledPackageNames = append(r.InstalledPackageNames, name)
			}
		}
		var baseRpms []string
		for _, p := range r.PendingRpms {
			baseRpms = append(baseRpms, "./"+filepath.Base(p))
		}
		var args []string
		if r.CliParams.NoDeps {
			args = append([]string{"rpm", "-i"}, baseRpms...)
		} else if r.CliParams.AddDeps {
			args = append([]string{"dnf", "localinstall", "-y"}, baseRpms...)
		} else {
			args = append([]string{"dnf", "localinstall"}, baseRpms...)
		}

		if r.CliParams.Interactive && !r.CliParams.DisablePrompts {
			if !r.interactiveConfirm(fmt.Sprintf("Run 'sudo %s'?", strings.Join(args, " "))) {
				return fmt.Errorf("user aborted batched .rpm installation")
			}
		}

		cmd := execCommand("sudo", args...)
		cmd.Dir = filepath.Dir(r.PendingRpms[0])
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		log.Info().Msgf("Executing batched package install: sudo %s", strings.Join(args, " "))
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("batched .rpm installation failed: %w", err)
		}
	}

	if !r.CliParams.NoSaveState {
		st, err := state.LoadState()
		if err == nil {
			_ = st.AddApp(&state.InstalledApp{
				Repository:          r.CliParams.Repository,
				TargetPath:          r.CliParams.TargetPath,
				Global:              r.CliParams.Global,
				ReleaseAsset:        r.CliParams.ReleaseAsset,
				ReleaseRegexp:       r.CliParams.ReleaseAssetRegexp,
				Version:             releases[0].Name,
				Rename:              r.CliParams.Rename,
				Type:                r.CliParams.Type,
				All:                 r.CliParams.All,
				AssetBinaries:       r.CliParams.AssetBinaries,
				AssetBinariesRegexp: r.CliParams.AssetBinariesRegexp,
				PackageNames:        r.InstalledPackageNames,
				Pinned:              r.CliParams.Pin,
				NativeExtract:       r.CliParams.NativeExtract,
			})
		} else {
			log.Warn().Err(err).Msg("could not save installed app state")
		}
	}

	return nil
}

func (r *GithubRelease) installPkg(binaryPath string) error {
	basePath := "./" + filepath.Base(binaryPath)
	if r.CliParams.DryRun {
		log.Info().Msgf("[dry-run] Would install freebsd pkg: %s", filepath.Base(binaryPath))
		return nil
	}
	if err := r.ensureSudo(); err != nil {
		return err
	}
	var args []string
	if r.CliParams.NoDeps {
		args = []string{"pkg", "add", basePath}
	} else if r.CliParams.AddDeps {
		args = []string{"pkg", "install", "-y", basePath}
	} else {
		args = []string{"pkg", "install", basePath}
	}

	if r.CliParams.Interactive {
		if !r.interactiveConfirm(fmt.Sprintf("Run 'sudo %s'?", strings.Join(args, " "))) {
			return fmt.Errorf("'%s' is a FreeBSD PKG installer and user did not want to run it", filepath.Base(binaryPath))
		}
	}

	cmd := execCommand("sudo", args...)
	cmd.Dir = filepath.Dir(binaryPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		log.Error().
			Str("installer binary", binaryPath).
			Err(err).
			Msgf("'sudo %s' failed", strings.Join(args, " "))
		if r.CliParams.Interactive {
			pterm.Error.Println("Failed to install FreeBSD package")
		}
		return err
	}
	log.Info().
		Str("installer binary", binaryPath).
		Msgf("ran 'sudo %s'", strings.Join(args, " "))
	if r.CliParams.Interactive {
		pterm.Success.Println("Successfully installed FreeBSD package!")
	}
	return nil
}

func (r *GithubRelease) installPacman(binaryPath string) error {
	if r.CliParams.DryRun {
		log.Info().Msgf("[dry-run] Would install pacman pkg: %s", filepath.Base(binaryPath))
		return nil
	}
	if err := r.ensureSudo(); err != nil {
		return err
	}
	if name := extractPackageName(binaryPath, "pacman"); name != "" {
		r.InstalledPackageNames = append(r.InstalledPackageNames, name)
	}
	log.Debug().
		Str("binaryPath", binaryPath).
		Msg("installing pacman package")

	var cmd *exec.Cmd
	basePath := "./" + filepath.Base(binaryPath)
	if r.CliParams.AddDeps {
		cmd = execCommand("sudo", "pacman", "-U", "--noconfirm", basePath)
	} else if r.CliParams.NoDeps {
		cmd = execCommand("sudo", "pacman", "-U", "--nodeps", "--noconfirm", basePath)
	} else {
		cmd = execCommand("sudo", "pacman", "-U", basePath)
	}
	cmd.Dir = filepath.Dir(binaryPath)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func generateStrictAssetRegex(assetName string, resolvedVersion string) string {
	if resolvedVersion == "" {
		return fmt.Sprintf("^%s$", regexp.QuoteMeta(assetName))
	}

	noVStr := regexp.QuoteMeta(strings.TrimPrefix(strings.ToLower(resolvedVersion), "v"))

	// We optionally consume up to 2 digits of a package revision (e.g. -1 to -99).
	versionRegex := regexp.MustCompile(fmt.Sprintf(`(?i)v?%s(?:-\d{1,2})?`, noVStr))
	matches := versionRegex.FindAllStringIndex(assetName, -1)

	if len(matches) == 0 {
		return fmt.Sprintf("^%s$", regexp.QuoteMeta(assetName))
	}

	// Manual lookahead to prevent partial consumption of longer digit sequences (e.g. -386 architecture).
	// If the character immediately following our match is a digit, we backtrack and strictly match the version only.
	for i, match := range matches {
		matchStr := assetName[match[0]:match[1]]
		// If we actually matched a suffix (length > basic version)
		if len(matchStr) > len(noVStr) && match[1] < len(assetName) {
			nextChar := assetName[match[1]]
			if nextChar >= '0' && nextChar <= '9' {
				// Backtrack match to exclude the -\d+ suffix
				baseRegex := regexp.MustCompile(fmt.Sprintf(`(?i)v?%s`, noVStr))
				baseMatch := baseRegex.FindStringIndex(assetName[match[0]:])
				if baseMatch != nil {
					matches[i][1] = match[0] + baseMatch[1]
				}
			}
		}
	}

	var parts []string
	lastIdx := 0
	for _, match := range matches {
		parts = append(parts, regexp.QuoteMeta(assetName[lastIdx:match[0]]))
		lastIdx = match[1]
	}
	parts = append(parts, regexp.QuoteMeta(assetName[lastIdx:]))

	return fmt.Sprintf("^%s$", strings.Join(parts, ".*"))
}
