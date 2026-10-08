package release

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/adrg/xdg"
	"github.com/charmbracelet/log"
	"github.com/cli/go-gh/v2"
	"github.com/joshsukhdeo/gh-pt/config"
	"github.com/joshsukhdeo/gh-pt/params"
	"github.com/joshsukhdeo/gh-pt/resolver"
	"github.com/joshsukhdeo/gh-pt/safety"
	"github.com/joshsukhdeo/gh-pt/selector"
	"github.com/joshsukhdeo/gh-pt/state"
	"github.com/joshsukhdeo/gh-pt/status"
	"github.com/joshsukhdeo/gh-pt/ui"
	"github.com/joshsukhdeo/gh-pt/verification"
	"github.com/pterm/pterm"
	"golang.org/x/term"
)

// formatGitHubError converts raw GitHub API errors into user-friendly messages.
func formatGitHubError(err error, repo string) error {
	if err == nil {
		return nil
	}

	errStr := err.Error()
	errLower := strings.ToLower(errStr)

	// Network/connection errors
	if strings.Contains(errLower, "no internet") || strings.Contains(errLower, "connection refused") ||
		strings.Contains(errLower, "network is unreachable") || strings.Contains(errLower, "timeout") ||
		strings.Contains(errLower, "dial tcp") || strings.Contains(errLower, "i/o timeout") {
		return fmt.Errorf("cannot connect to GitHub: %w\n  → Check your internet connection and try again", err)
	}

	// 404 - repo not found
	if strings.Contains(errLower, "404") || strings.Contains(errLower, "not found") {
		return fmt.Errorf("repository %q not found on GitHub\n  → Verify the repository name (format: owner/repo) and that it exists", repo)
	}

	// 403 - rate limited or private repo
	if strings.Contains(errLower, "403") || strings.Contains(errLower, "forbidden") {
		if strings.Contains(errLower, "rate limit") || strings.Contains(errLower, "rate limit exceeded") {
			return fmt.Errorf("GitHub API rate limit exceeded\n  → Authenticate with 'gh auth login' to increase limits, or wait and retry")
		}
		return fmt.Errorf("access denied to repository %q\n  → Repository may be private. Authenticate with 'gh auth login' or check permissions", repo)
	}

	// 401 - auth required
	if strings.Contains(errLower, "401") || strings.Contains(errLower, "unauthorized") || strings.Contains(errLower, "bad credentials") {
		return fmt.Errorf("GitHub authentication required\n  → Run 'gh auth login' to authenticate, or check your token has 'repo' scope")
	}

	// 5xx - GitHub server errors
	if strings.Contains(errLower, "500") || strings.Contains(errLower, "502") || strings.Contains(errLower, "503") || strings.Contains(errLower, "504") {
		return fmt.Errorf("GitHub API server error (%s)\n  → GitHub may be experiencing issues. Check status.github.com and retry", errStr)
	}

	// DNS errors
	if strings.Contains(errLower, "no such host") || strings.Contains(errLower, "dns") {
		return fmt.Errorf("cannot resolve GitHub hostname\n  → Check your DNS settings and internet connection")
	}

	// TLS/SSL errors
	if strings.Contains(errLower, "tls") || strings.Contains(errLower, "ssl") || strings.Contains(errLower, "certificate") {
		return fmt.Errorf("TLS/SSL error connecting to GitHub\n  → Check your system certificates or try updating ca-certificates")
	}

	// Generic fallback
	return fmt.Errorf("GitHub API error for %q: %w", repo, err)
}

var (
	execCommand      = exec.Command
	ghExec           = gh.Exec
	getNativeManager = resolver.GetNativeManager
)

type GithubRelease struct {
	CliParams                *params.ExecContext
	Client                   selector.GithubClient
	ResolvedVersion          string
	InstalledPackageNames    []string
	InstalledBinaries        []string
	InstalledAssetFullNames  []string
	InstalledAssetCleanNames []string
	ContainingArchive        string
	PendingDebs              []string
	PendingRpms              []string
	Prompter                 Prompter
	StatusMessage            string
	UI                       interface {
		Start()
		Stop()
		Pause()
		Resume()
		Update(step int, a string, b string, c string, target string, e string)
	}
	Sidecars            string
	SidecarSymlinkTo    []string
	InstalledSidecars   []string
	InstalledFiles      []string
	InstalledSymlinks   []string
	InstalledPkgManager string
	InstalledPackageIDs []string
	DepsPkgManager      string
	DepsPackageIDs      []string
	DepsFiles           []string
	IsTTYFunc           func() bool
	WarnUnmappedAssets  *bool
	VerifiedSigned      bool
	VerificationMethod  string
}

type Prompter interface {
	Confirm(message string) bool
	Input(prompt string, defaultValue string) string
	Multiselect(prompt string, options []string) ([]string, error)
}

type PtermPrompter struct{}

func (p PtermPrompter) Confirm(message string) bool {
	result, _ := pterm.DefaultInteractiveConfirm.WithDefaultValue(true).Show(message)
	return result
}

func (p PtermPrompter) Input(prompt string, defaultValue string) string {
	result, _ := pterm.DefaultInteractiveTextInput.WithDefaultValue(defaultValue).Show(prompt)
	return result
}

func (p PtermPrompter) Multiselect(prompt string, options []string) ([]string, error) {
	return pterm.DefaultInteractiveMultiselect.WithOptions(options).Show(prompt)
}

func MakeGithubRelease(cliParams *params.ExecContext, cli selector.GithubClient) *GithubRelease {

	return &GithubRelease{
		CliParams: cliParams,
		Client:    cli,
		Prompter:  PtermPrompter{},
	}
}

func (r *GithubRelease) interactiveConfirm(prompt string) bool {
	if r.CliParams.DisablePrompts {
		return true
	}
	if r.Prompter == nil {
		r.Prompter = PtermPrompter{}
	}
	if r.UI != nil {
		r.UI.Pause()
		defer r.UI.Resume()
	}
	return r.Prompter.Confirm(prompt)
}

func (r *GithubRelease) interactiveInput(prompt string, defaultValue string) string {
	if r.CliParams.DisablePrompts {
		return defaultValue
	}
	if r.Prompter == nil {
		r.Prompter = PtermPrompter{}
	}
	if r.UI != nil {
		r.UI.Pause()
		defer r.UI.Resume()
	}
	return r.Prompter.Input(prompt, defaultValue)
}

func (r *GithubRelease) isTTY() bool {
	if r.IsTTYFunc != nil {
		return r.IsTTYFunc()
	}
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

func (r *GithubRelease) isInteractive() bool {
	if r.CliParams == nil {
		return false
	}
	if r.CliParams.DisablePrompts {
		return false
	}
	if !r.CliParams.Interactive {
		return false
	}
	return r.isTTY()
}

func (r *GithubRelease) interactiveMultiselect(prompt string, options []string) ([]string, error) {
	if r.CliParams != nil && r.CliParams.DisablePrompts {
		return nil, nil
	}
	if r.Prompter == nil {
		r.Prompter = PtermPrompter{}
	}
	if r.UI != nil {
		r.UI.Pause()
		defer r.UI.Resume()
	}
	return r.Prompter.Multiselect(prompt, options)
}

func (r *GithubRelease) shouldWarnUnmappedAssets() bool {
	if r.CliParams != nil {
		return r.CliParams.WarnUnmappedAssets
	}
	if r.WarnUnmappedAssets != nil {
		return *r.WarnUnmappedAssets
	}
	cfg, err := config.LoadConfig()
	if err != nil || cfg == nil {
		return true
	}
	return cfg.Core.WarnUnmappedAssets
}

func (r *GithubRelease) resolveSidecarTargetPath() string {
	if r.CliParams == nil {
		return ""
	}

	mode := r.CliParams.SidecarMode
	if mode == "" || mode == "auto" {
		mode = "xdg_data_home"
	}

	// Parse the mode
	switch {
	case mode == "same_dest":
		// Sidecars go to the same destination as the main binary
		return r.CliParams.TargetPath
	case mode == "xdg_data_home":
		// Sidecars go to XDG data home
		return filepath.Join(xdg.DataHome, "gh-pt", "sidecars", r.CliParams.Repository)
	case mode == "bin":
		// Sidecars go to the bin directory
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, ".local", "bin")
	case mode == "local-map":
		// Sidecars stay in packageDir and get mapped to /usr/local/
		parts := strings.Split(r.CliParams.Repository, "/")
		var ownerID, repoID string
		if len(parts) >= 2 {
			ownerID = parts[0]
			repoID = parts[1]
		} else {
			ownerID = ""
			repoID = parts[0]
		}
		return r.resolvePackageDir(ownerID, repoID)
	case strings.HasPrefix(mode, "custom-path:"):
		// Extract custom path
		return strings.TrimPrefix(mode, "custom-path:")
	default:
		log.Warn("unknown sidecar mode, defaulting to xdg_data_home", "mode", mode)
		return filepath.Join(xdg.DataHome, "gh-pt", "sidecars", r.CliParams.Repository)
	}
}

func isLicenseFileName(path string) bool {
	return selector.DefaultClassifier().IsLicense(path)
}

func isSuspectedRemoteSidecar(name string) bool {
	return selector.DefaultClassifier().IsSidecar(name)
}

func isSuspectedLocalSidecar(relPath string, fileName string) bool {
	lowerPath := strings.ToLower(relPath)
	lowerName := strings.ToLower(fileName)

	// Filter out standard bloat: README*, LICENSE*, .md, doc/, src/
	if isLicenseFileName(fileName) || isLicenseFileName(relPath) ||
		strings.HasPrefix(lowerName, "readme") {
		return false
	}

	if strings.HasSuffix(lowerName, ".md") ||
		strings.HasSuffix(lowerName, ".txt") ||
		strings.HasSuffix(lowerName, ".rst") ||
		strings.HasSuffix(lowerName, ".rtf") ||
		strings.HasSuffix(lowerName, ".html") {
		return false
	}

	cleanPath := filepath.ToSlash(lowerPath)
	if strings.HasPrefix(cleanPath, "doc/") || strings.Contains(cleanPath, "/doc/") ||
		strings.HasPrefix(cleanPath, "docs/") || strings.Contains(cleanPath, "/docs/") ||
		strings.HasPrefix(cleanPath, "src/") || strings.Contains(cleanPath, "/src/") {
		return false
	}

	if strings.HasPrefix(lowerName, ".") {
		return false
	}

	// Flag shared libraries (.so, .dll, .dylib)
	if strings.HasSuffix(lowerName, ".so") ||
		strings.Contains(lowerName, ".so.") ||
		strings.HasSuffix(lowerName, ".dll") ||
		strings.HasSuffix(lowerName, ".dylib") {
		return true
	}

	// Flag config templates (.json, .yaml, .yml)
	if strings.HasSuffix(lowerName, ".json") ||
		strings.HasSuffix(lowerName, ".yaml") ||
		strings.HasSuffix(lowerName, ".yml") {
		return true
	}

	// Flag domain-specific plugins (.pak, .bin, .red, .dat or keyword "plugin")
	if strings.HasSuffix(lowerName, ".pak") ||
		strings.HasSuffix(lowerName, ".bin") ||
		strings.HasSuffix(lowerName, ".red") ||
		strings.HasSuffix(lowerName, ".dat") ||
		strings.Contains(lowerName, "plugin") {
		return true
	}

	return false
}

func (r *GithubRelease) handleSuspectedSidecars(suspected []string, extractDir string, fsObj fs.FS, releaseID int) ([]string, error) {
	if len(suspected) == 0 {
		return nil, nil
	}
	if !r.shouldWarnUnmappedAssets() {
		return nil, nil
	}

	if !r.isInteractive() {
		pterm.Warning.Printf("Release contains unmapped sidecar assets (%s). Pass --sidecars to capture them on future installs.\n", strings.Join(suspected, ", "))
		return nil, nil
	}

	selected, err := r.interactiveMultiselect("Suspected sidecar assets detected. Select items to deploy:", suspected)
	if err != nil || len(selected) == 0 {
		return nil, err
	}

	targetDir := r.resolveSidecarTargetPath()
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		log.Warn("could not create sidecar target directory", "error", err)
	}
	// Mark directory as managed by gh-pt for safe removal later
	_ = safety.WriteGhptManagedMarker(targetDir)

	for _, item := range selected {
		if isLicenseFileName(item) {
			continue
		}
		// Check if local file in extractDir
		if extractDir != "" {
			src := filepath.Join(extractDir, item)
			if fi, err := os.Stat(src); err == nil && !fi.IsDir() {
				dst := filepath.Join(targetDir, filepath.Base(item))
				if err := copyFile(src, dst, 0755); err == nil {
					r.InstalledSidecars = append(r.InstalledSidecars, dst)
					continue
				}
			}
		}

		// Check if in fsObj
		if fsObj != nil {
			if sf, err := fsObj.Open(item); err == nil {
				dst := filepath.Join(targetDir, filepath.Base(item))
				_ = os.Chmod(filepath.Dir(dst), 0755)
				_ = os.Remove(dst)
				if df, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755); err == nil {
					_, _ = io.Copy(df, sf)
					_ = df.Close()
					r.InstalledSidecars = append(r.InstalledSidecars, dst)
				}
				_ = sf.Close()
				continue
			}
		}

		// Otherwise, remote asset
		version := r.ResolvedVersion
		if version == "" && r.CliParams != nil {
			version = r.CliParams.ReleaseAsset
		}
		if r.CliParams != nil && version != "" {
			_, _, err := ghExec("release", "download", version, "--repo", r.CliParams.Repository, "--pattern", item, "--dir", targetDir)
			if err == nil {
				r.InstalledSidecars = append(r.InstalledSidecars, filepath.Join(targetDir, item))
			} else {
				log.Warn("failed to download remote sidecar asset", "error", err, "asset", item)
			}
		}
	}

	return selected, nil
}

func (r *GithubRelease) extractExplicitSidecars(extractDir string, fsObj fs.FS) error {
	if len(r.CliParams.Sidecars) == 0 {
		return nil
	}

	targetDir := r.resolveSidecarTargetPath()
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create sidecar target directory: %w", err)
	}
	// Mark directory as managed by gh-pt for safe removal later
	_ = safety.WriteGhptManagedMarker(targetDir)

	sidecarRegex := r.CliParams.Sidecars
	if sidecarRegex == "" {
		sidecarRegex = `\.so.*|\.h.*|\.pak|\.bin|\.red`
	}
	regex, err := regexp.Compile(sidecarRegex)
	if err != nil {
		log.Warn("invalid sidecar regex, using default", "error", err)
		regex = regexp.MustCompile(`\.so.*|\.h.*|\.pak|\.bin|\.red`)
	}

	var matched []string

	if extractDir != "" {
		err := filepath.Walk(extractDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if isLicenseFileName(path) || isLicenseFileName(info.Name()) {
				return nil
			}
			rel, _ := filepath.Rel(extractDir, path)
			if isLicenseFileName(rel) {
				return nil
			}
			if regex.MatchString(rel) {
				matched = append(matched, rel)
			}
			return nil
		})
		if err != nil {
			log.Warn("error walking extraction directory for sidecars", "error", err)
		}
	} else if fsObj != nil {
		err := fs.WalkDir(fsObj, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if isLicenseFileName(path) || isLicenseFileName(d.Name()) {
				return nil
			}
			if regex.MatchString(path) {
				matched = append(matched, path)
			}
			return nil
		})
		if err != nil {
			log.Warn("error walking fs.FS for sidecars", "error", err)
		}
	}

	for _, rel := range matched {
		var src io.ReadCloser
		if extractDir != "" {
			f, err := os.Open(filepath.Join(extractDir, rel))
			if err != nil {
				log.Warn("failed to open sidecar file", "error", err, "sidecar", rel)
				continue
			}
			src = f
		} else if fsObj != nil {
			f, err := fsObj.Open(rel)
			if err != nil {
				log.Warn("failed to open sidecar from fs.FS", "error", err, "sidecar", rel)
				continue
			}
			src = f
		}
		if src == nil {
			continue
		}

		dst := filepath.Join(targetDir, filepath.Base(rel))
		_ = os.Chmod(filepath.Dir(dst), 0755)
		_ = os.Remove(dst)
		df, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			_ = src.Close()
			log.Warn("failed to create sidecar destination", "error", err, "sidecar", rel)
			continue
		}
		_, _ = io.Copy(df, src)
		_ = df.Close()
		_ = src.Close()
		r.InstalledSidecars = append(r.InstalledSidecars, dst)
		log.Info("deployed sidecar asset", "sidecar", rel, "destination", dst)
	}

	if len(matched) == 0 {
		log.Warn("no sidecar assets matched the specified patterns", "patterns", r.CliParams.Sidecars)
	} else {
		log.Info("deployed sidecar assets", "count", len(matched))
	}

	// Create symlinks if requested
	if len(r.CliParams.SidecarSymlinkTo) > 0 {
		r.createSidecarSymlinks()
	}

	return nil
}

func (r *GithubRelease) createSidecarSymlinks() {
	if len(r.InstalledSidecars) == 0 {
		return
	}

	for _, targetDir := range r.CliParams.SidecarSymlinkTo {
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			log.Warn("failed to create symlink target directory", "error", err, "dir", targetDir)
			continue
		}

		for _, sidecarPath := range r.InstalledSidecars {
			sidecarName := filepath.Base(sidecarPath)
			linkPath := filepath.Join(targetDir, sidecarName)

			// Use atomic symlink creation to avoid TOCTOU race condition
			if err := createSymlinkAtomic(sidecarPath, linkPath); err != nil {
				log.Warn("failed to create symlink", "error", err, "sidecar", sidecarPath, "link", linkPath)
			} else {
				log.Info("created symlink", "sidecar", sidecarName, "link", linkPath)
				r.InstalledSymlinks = append(r.InstalledSymlinks, linkPath)
			}
		}
	}
}

func (r *GithubRelease) runAISidecarSetup() error {
	if !r.CliParams.AISetupSidecars || len(r.InstalledSidecars) == 0 {
		return nil
	}

	// Get AI command template from config or use default
	cfg, _ := config.LoadConfig()
	aiCmdTemplate := "agy -p \"%s\""
	if cfg != nil && cfg.AI.AICmd != "" {
		aiCmdTemplate = cfg.AI.AICmd
	}

	// Build prompt with sidecar information
	sidecarList := strings.Join(r.InstalledSidecars, "\n")
	prompt := fmt.Sprintf(`Analyze the following sidecar files that were installed for %s and provide setup instructions:

%s

These files are located in: %s

Please provide:
1. What these files appear to be (plugins, libraries, data files, etc.)
2. Recommended environment variables to set (e.g., PLUGIN_DIR, DATA_PATH)
3. Suggested symlinks or configuration file modifications needed
4. Any additional setup steps required for the application to find these files

Format your response as actionable shell commands where possible.`, r.CliParams.Repository, sidecarList, r.resolveSidecarTargetPath())

	log.Info("Initiating AI sidecar setup analysis...")

	// Execute AI agent
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("powershell", "-Command", aiCmdTemplate, prompt)
	} else {
		cmd = exec.Command("sh", "-c", fmt.Sprintf(aiCmdTemplate, prompt))
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("AI sidecar setup failed: %w", err)
	}

	return nil
}

func (r *GithubRelease) resolveDestinationPath(binaryPath string) string {
	binaryName := filepath.Base(binaryPath)
	destinationPath := filepath.Join(r.CliParams.TargetPath, binaryName)

	if targetBinaryName, exists := r.CliParams.Rename[strings.ToLower(binaryName)]; exists {
		return filepath.Join(r.CliParams.TargetPath, targetBinaryName)
	}

	if !r.CliParams.KeepSuffixes {
		repoParts := strings.Split(r.CliParams.Repository, "/")
		repoName := repoParts[len(repoParts)-1]

		proposedName := GenerateCleanName(binaryName, repoName, r.ResolvedVersion)

		if extMatch := regexp.MustCompile(`(?i)(\.[a-z][a-z0-9]*)$`).FindStringSubmatch(binaryName); len(extMatch) > 0 {
			ext := extMatch[1]
			if !strings.HasSuffix(proposedName, ext) {
				proposedName += ext
			}
		}

		if len(binaryName) > len(proposedName) {
			if r.CliParams.Rename == nil {
				r.CliParams.Rename = make(map[string]string)
			}
			r.CliParams.Rename[strings.ToLower(binaryName)] = proposedName
			destinationPath = filepath.Join(r.CliParams.TargetPath, proposedName)
		}
	}

	if r.CliParams.Interactive && !r.CliParams.DisablePrompts {
		return r.interactiveInput(fmt.Sprintf("Install %s to", binaryPath), destinationPath)
	}

	return destinationPath
}

func (r *GithubRelease) installArchivedBinary(fileSystem fs.FS, binaryPath string) error {
	if r.CliParams.DryRun {
		destinationPath := r.resolveDestinationPath(binaryPath)
		r.InstalledBinaries = append(r.InstalledBinaries, filepath.Base(destinationPath))
		r.InstalledFiles = append(r.InstalledFiles, destinationPath)
		log.Infof("[dry-run] Would extract and install: %s to %s", binaryPath, destinationPath)
		return nil
	}

	tempExtractDir, err := safety.SafeMkdirTemp("", "gh-pt-extract-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = safety.RemoveAll(tempExtractDir)
	}()

	if err := copyFS(fileSystem, tempExtractDir); err != nil {
		return fmt.Errorf("failed to extract archive: %w", err)
	}

	extractedBinaryPath := filepath.Join(tempExtractDir, binaryPath)
	if _, err := os.Stat(extractedBinaryPath); err != nil {
		foundPath := ""
		_ = filepath.WalkDir(tempExtractDir, func(p string, d fs.DirEntry, e error) error {
			if e == nil && !d.IsDir() && (d.Name() == binaryPath || d.Name() == filepath.Base(binaryPath)) {
				foundPath = p
				return fs.SkipAll
			}
			return nil
		})
		if foundPath != "" {
			extractedBinaryPath = foundPath
		}
	}

	if err := r.resolveBinaryDependencies(extractedBinaryPath, tempExtractDir); err != nil {
		log.Warn("dependency resolution encountered an error", "error", err)
	}

	destinationPath := r.resolveDestinationPath(binaryPath)
	r.InstalledBinaries = append(r.InstalledBinaries, filepath.Base(destinationPath))
	r.InstalledFiles = append(r.InstalledFiles, destinationPath)

	log.Infof("will install %s to %s", binaryPath, destinationPath)

	_, err = os.Stat(destinationPath)
	if err == nil {
		if r.CliParams.Interactive {
			if !r.interactiveConfirm(fmt.Sprintf("'%s' already exists. Overwrite?", destinationPath)) {
				return fmt.Errorf("%s already exists and user did not want to overwrite", destinationPath)
			}
		} else {
			if !r.CliParams.Overwrite {
				return fmt.Errorf("%s already exists and -f/--force is not set", destinationPath)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	sourceFile, err := os.Open(extractedBinaryPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = sourceFile.Close()
	}()

	destinationFile, err := os.Create(destinationPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = destinationFile.Close()
	}()

	_, err = io.Copy(destinationFile, sourceFile)
	if err != nil {
		return err
	}

	err = os.Chmod(destinationPath, 0755)
	if err != nil {
		return err
	}

	noComp := false
	if r.CliParams != nil {
		noComp = r.CliParams.NoCompletionSetup
	}
	ScanAndInstallArchiveCompletions(tempExtractDir, filepath.Base(destinationPath), noComp)
	_ = SetupBinaryCompletions(destinationPath, noComp)

	return nil
}

// findBundledSharedObjects recursively sweeps dir for shared object files (.so, .so.*).
// It returns all matching file paths and the unique parent directories containing .so files.
func findBundledSharedObjects(dir string) ([]string, []string, error) {
	var soFiles []string
	var soDirs []string
	seenDirs := make(map[string]bool)

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasSuffix(name, ".so") || strings.Contains(name, ".so.") {
			soFiles = append(soFiles, path)
			dirPath := filepath.Dir(path)
			if !seenDirs[dirPath] {
				seenDirs[dirPath] = true
				soDirs = append(soDirs, dirPath)
			}
		}
		return nil
	})
	return soFiles, soDirs, err
}

// parseMissingLibraries parses the output of `ldd` and captures all library names marked "=> not found".
func parseMissingLibraries(lddOutput string) []string {
	var missing []string
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(strings.NewReader(lddOutput))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.Contains(line, "=> not found") {
			parts := strings.Split(line, "=>")
			libName := strings.TrimSpace(parts[0])
			if libName != "" && !seen[libName] {
				seen[libName] = true
				missing = append(missing, libName)
			}
		}
	}
	return missing
}

// mapSharedObjectToPackage maps a missing shared library requirement to candidate package name(s)
// for the given package manager ("apt", "dnf", "pacman", etc.).
func mapSharedObjectToPackage(mgrName string, soName string) string {
	switch {
	case strings.HasPrefix(soName, "libz.so"):
		if mgrName == "apt" {
			return "zlib1g"
		}
		return "zlib"
	case strings.HasPrefix(soName, "libssl.so.3"), strings.HasPrefix(soName, "libcrypto.so.3"):
		if mgrName == "apt" {
			return "libssl3"
		}
		return "openssl-libs"
	case strings.HasPrefix(soName, "libssl.so.1.1"), strings.HasPrefix(soName, "libcrypto.so.1.1"):
		if mgrName == "apt" {
			return "libssl1.1"
		}
		return "openssl1.1"
	case strings.HasPrefix(soName, "libfuse.so.2"):
		return "libfuse2"
	case strings.HasPrefix(soName, "libfuse3.so"):
		if mgrName == "apt" {
			return "libfuse3-3"
		}
		return "fuse3-libs"
	case strings.HasPrefix(soName, "libcurl.so.4"):
		if mgrName == "apt" {
			return "libcurl4"
		}
		return "libcurl"
	case strings.HasPrefix(soName, "libstdc++.so.6"):
		if mgrName == "apt" {
			return "libstdc++6"
		}
		return "libstdc++"
	}

	if mgrName == "dnf" {
		return soName
	}

	re := regexp.MustCompile(`^(lib[a-zA-Z0-9_\-+]+)\.so(?:\.([0-9]+))?`)
	matches := re.FindStringSubmatch(soName)
	if len(matches) > 1 {
		prefix := matches[1]
		ver := ""
		if len(matches) > 2 {
			ver = matches[2]
		}
		if ver != "" {
			return prefix + ver
		}
		return prefix
	}

	return soName
}

// scanMissingDependencies executes `ldd <binary>` with LD_LIBRARY_PATH temporarily containing extractDir
// and any bundled .so directories, capturing missing shared object dependencies ("=> not found").
func scanMissingDependencies(binaryPath string, extractDir string) ([]string, error) {
	if runtime.GOOS != "linux" {
		return nil, nil
	}

	if _, err := exec.LookPath("ldd"); err != nil {
		return nil, nil
	}

	_, soDirs, err := findBundledSharedObjects(extractDir)
	if err != nil {
		log.Warn("failed to sweep for bundled .so files", "error", err, "extractDir", extractDir)
	}

	ldPaths := []string{extractDir}
	ldPaths = append(ldPaths, soDirs...)
	if currentLd := os.Getenv("LD_LIBRARY_PATH"); currentLd != "" {
		ldPaths = append(ldPaths, currentLd)
	}
	injectedLdPath := strings.Join(ldPaths, string(os.PathListSeparator))

	origLd, hasOrigLd := os.LookupEnv("LD_LIBRARY_PATH")
	_ = os.Setenv("LD_LIBRARY_PATH", injectedLdPath)
	defer func() {
		if hasOrigLd {
			_ = os.Setenv("LD_LIBRARY_PATH", origLd)
		} else {
			_ = os.Unsetenv("LD_LIBRARY_PATH")
		}
	}()

	cmd := execCommand("ldd", binaryPath)
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env, "LD_LIBRARY_PATH="+injectedLdPath)

	out, err := cmd.CombinedOutput()
	if err != nil {
		outStr := string(out)
		if strings.Contains(outStr, "not a dynamic executable") || strings.Contains(outStr, "statically linked") {
			return nil, nil
		}
		log.Debug("ldd scan returned non-zero", "error", err, "binary", binaryPath, "output", outStr)
	}

	return parseMissingLibraries(string(out)), nil
}

// resolveBinaryDependencies scans the binary for missing shared library dependencies using ldd
// and installs them via the detected package manager when ResolveDeps is enabled.
func (r *GithubRelease) resolveBinaryDependencies(binaryPath string, extractDir string) error {
	if r.CliParams.NoDeps {
		return nil
	}

	missingLibs, err := scanMissingDependencies(binaryPath, extractDir)
	if err != nil {
		log.Warn("failed to scan binary dependencies via ldd", "error", err)
		return nil
	}

	if len(missingLibs) == 0 {
		return nil
	}

	log.Info("detected missing shared object dependencies", "missing_libraries", missingLibs)

	if !r.CliParams.ResolveDeps {
		log.Warn("missing shared library dependencies detected; run with --resolve-deps to install them automatically", "missing_libraries", missingLibs)
		return nil
	}

	mgr, err := getNativeManager()
	if err != nil {
		log.Warn("could not detect native package manager to resolve dependencies", "error", err)
		return err
	}

	var packagesToInstall []string
	seen := make(map[string]bool)
	for _, lib := range missingLibs {
		pkg := mapSharedObjectToPackage(mgr.Name(), lib)
		if pkg != "" && !seen[pkg] {
			seen[pkg] = true
			packagesToInstall = append(packagesToInstall, pkg)
		}
	}

	if len(packagesToInstall) == 0 {
		return nil
	}

	log.Info("installing missing dependencies via package manager", "manager", mgr.Name(), "packages", packagesToInstall)

	if r.CliParams.PromptDeps && !r.CliParams.DisablePrompts {
		if !r.interactiveConfirm(fmt.Sprintf("Install missing dependencies: %s?", strings.Join(packagesToInstall, ", "))) {
			log.Info("skipping dependency installation per user choice")
			return nil
		}
	}

	if err := mgr.Install(packagesToInstall); err != nil {
		log.Error("failed to install dependencies", "error", err, "packages", packagesToInstall)
		return err
	}

	r.InstalledPackageNames = append(r.InstalledPackageNames, packagesToInstall...)
	r.DepsPkgManager = mgr.Name()
	r.DepsPackageIDs = append(r.DepsPackageIDs, packagesToInstall...)
	return nil
}

func (r *GithubRelease) installBinary(binaryPath string) error {
	if r.CliParams.DryRun {
		destinationPath := r.resolveDestinationPath(binaryPath)
		r.InstalledBinaries = append(r.InstalledBinaries, filepath.Base(destinationPath))
		r.InstalledFiles = append(r.InstalledFiles, destinationPath)
		log.Infof("[dry-run] Would install binary: %s to %s", binaryPath, destinationPath)
		return nil
	}
	sourceStat, err := os.Stat(binaryPath)
	if err != nil {
		return err
	}

	if !sourceStat.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", binaryPath)
	}

	if err := r.resolveBinaryDependencies(binaryPath, filepath.Dir(binaryPath)); err != nil {
		log.Warn("dependency resolution encountered an error", "error", err)
	}

	source, err := os.Open(binaryPath)
	if err != nil {
		return err
	}

	defer func() {
		err = errors.Join(err, source.Close())
	}()

	destinationPath := r.resolveDestinationPath(binaryPath)
	r.InstalledBinaries = append(r.InstalledBinaries, filepath.Base(destinationPath))
	r.InstalledFiles = append(r.InstalledFiles, destinationPath)

	log.Infof("will install %s to %s", binaryPath, destinationPath)

	_, err = os.Stat(destinationPath)
	if err == nil {
		if r.CliParams.Interactive {
			if !r.interactiveConfirm(fmt.Sprintf("'%s' already exists. Overwrite?", destinationPath)) {
				return fmt.Errorf("%s already exists and user did not want to overwrite", destinationPath)
			}
		} else {
			if !r.CliParams.Overwrite {
				return fmt.Errorf("%s already exists and -f/--force is not set", destinationPath)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	destination, err := os.Create(destinationPath)
	if err != nil {
		if os.IsPermission(err) {
			log.Info("permission denied, attempting to install with sudo")
			if err := r.ensureSudo(); err != nil {
				return err
			}
			if r.CliParams.Interactive {
				if !r.interactiveConfirm(fmt.Sprintf("Run 'sudo install -m 755 %s %s'?", binaryPath, destinationPath)) {
					return fmt.Errorf("permission denied and user aborted sudo installation")
				}
			}
			if r.UI != nil {
				r.UI.Pause()
				defer r.UI.Resume()
			}
			cmd := execCommand("sudo", "install", "-m", "755", binaryPath, destinationPath)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("sudo install failed: %w", err)
			}
			noComp := false
			if r.CliParams != nil {
				noComp = r.CliParams.NoCompletionSetup
			}
			_ = SetupBinaryCompletions(destinationPath, noComp)
			return nil
		}
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

	noComp := false
	if r.CliParams != nil {
		noComp = r.CliParams.NoCompletionSetup
	}
	_ = SetupBinaryCompletions(destinationPath, noComp)

	return nil
}

func (r *GithubRelease) ensureSudo() error {
	check := execCommand("sudo", "-n", "true")
	if err := check.Run(); err != nil {
		if !r.CliParams.Interactive {
			return fmt.Errorf("sudo session expired or unavailable; cannot prompt for password in headless mode (-D). Run 'sudo -v' beforehand or use an interactive session")
		}
		log.Warn("sudo session not cached; you may be prompted for your password")
	}
	return nil
}

// extractPackageName queries the package name from a local package file.
func extractPackageName(binaryPath string, pkgType string) string {
	base := filepath.Base(binaryPath)
	fallbackName := func() string {
		ext := filepath.Ext(base)
		cleaned := strings.TrimSuffix(base, ext)
		if idx := strings.Index(cleaned, "_"); idx > 0 {
			return cleaned[:idx]
		}
		if idx := strings.Index(cleaned, "-"); idx > 0 {
			return cleaned[:idx]
		}
		return cleaned
	}

	if safety.IsDryRun() {
		return fallbackName()
	}

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

// DebBinaryEntry represents a binary file found inside a .deb package.
type DebBinaryEntry struct {
	Path string // Path inside the package (e.g., "/usr/bin/mytool")
	Name string // Basename of the binary (e.g., "mytool")
}

// ListDebContents uses dpkg-deb -c to list all files in a .deb package.
// Returns a slice of file paths found inside the package.
func ListDebContents(debPath string) ([]string, error) {
	if safety.IsDryRun() {
		return nil, nil
	}
	cmd := exec.Command("dpkg-deb", "-c", debPath)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("dpkg-deb -c failed: %w", err)
	}
	var files []string
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// dpkg-deb -c output format: "drwxr-xr-x root/root 0 2024-01-01 00:00 ./usr/bin/"
		// or "-rwxr-xr-x root/root 12345 2024-01-01 00:00 ./usr/bin/mytool"
		parts := strings.Fields(line)
		if len(parts) >= 6 {
			// The last field is the path
			path := parts[len(parts)-1]
			path = strings.TrimPrefix(path, "./")
			files = append(files, path)
		}
	}
	return files, nil
}

// ListDebBinaries returns all executable binary paths found in a .deb package.
// It filters for files that look like binaries (in bin/, sbin/, libexec/ directories).
func ListDebBinaries(debPath string) ([]DebBinaryEntry, error) {
	files, err := ListDebContents(debPath)
	if err != nil {
		return nil, err
	}
	var binaries []DebBinaryEntry
	binaryDirs := []string{"/bin/", "/sbin/", "/libexec/", "/usr/bin/", "/usr/sbin/", "/usr/libexec/"}
	for _, f := range files {
		for _, d := range binaryDirs {
			if strings.HasPrefix(f, d) {
				name := filepath.Base(f)
				binaries = append(binaries, DebBinaryEntry{Path: f, Name: name})
				break
			}
		}
	}
	return binaries, nil
}

// CheckDebBinaryCollision checks if a .deb package contains binaries that would
// collide with standalone binaries from the same release.
// Returns a map of colliding binary names and their paths in the deb.
func CheckDebBinaryCollision(debPath string, standaloneBinaries []string) (map[string]string, error) {
	debBinaries, err := ListDebBinaries(debPath)
	if err != nil {
		return nil, err
	}
	collisions := make(map[string]string)
	standaloneMap := make(map[string]bool)
	for _, b := range standaloneBinaries {
		standaloneMap[strings.ToLower(filepath.Base(b))] = true
	}
	for _, db := range debBinaries {
		if standaloneMap[strings.ToLower(db.Name)] {
			collisions[db.Name] = db.Path
		}
	}
	return collisions, nil
}

// getStandaloneBinariesForDeb returns the list of standalone binary names that would be
// installed alongside the given deb package. It filters the pending binaries for the same release.
func (r *GithubRelease) getStandaloneBinariesForDeb(debPath string) []string {
	var binaries []string
	for _, binary := range r.InstalledBinaries {
		if binary != "" && !strings.HasSuffix(binary, ".deb") {
			binaries = append(binaries, filepath.Base(binary))
		}
	}
	// Also check pending debs/rpms for binaries
	for _, p := range r.PendingDebs {
		if p != debPath {
			binaries = append(binaries, filepath.Base(p))
		}
	}
	for _, p := range r.PendingRpms {
		binaries = append(binaries, filepath.Base(p))
	}
	return binaries
}

func (r *GithubRelease) installDeb(binaryPath string) error {
	if r.CliParams.DryRun {
		pkgName := extractPackageName(binaryPath, "deb")
		if pkgName != "" {
			r.InstalledPackageNames = append(r.InstalledPackageNames, pkgName)
		}
		log.Infof("[dry-run] Would queue deb for batch install: %s", filepath.Base(binaryPath))
		return nil
	}
	log.Infof("Queuing %s for batch installation...", filepath.Base(binaryPath))
	r.PendingDebs = append(r.PendingDebs, binaryPath)
	return nil
}

func (r *GithubRelease) installRpm(binaryPath string) error {
	if r.CliParams.DryRun {
		pkgName := extractPackageName(binaryPath, "rpm")
		if pkgName != "" {
			r.InstalledPackageNames = append(r.InstalledPackageNames, pkgName)
		}
		log.Infof("[dry-run] Would queue rpm for batch install: %s", filepath.Base(binaryPath))
		return nil
	}
	log.Infof("Queuing %s for batch installation...", filepath.Base(binaryPath))
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

func cleanAssetType(assetName string) string {
	lower := strings.ToLower(assetName)
	for _, compound := range []string{".tar.gz", ".tar.xz", ".tar.bz2", ".tar.zst", ".tar.lzma", ".pkg.tar.zst", ".pkg.tar.xz"} {
		if strings.HasSuffix(lower, compound) {
			return strings.TrimPrefix(compound, ".")
		}
	}
	ext := strings.TrimPrefix(filepath.Ext(lower), ".")
	if ext != "" {
		return ext
	}
	return "binary"
}

var semverExtractRegex = regexp.MustCompile(`(?i)\bv?([0-9]+\.[0-9]+(?:\.[0-9]+)?(?:-[a-zA-Z0-9.+_-]+)?)\b`)

func extractVersionFromString(s string) string {
	lines := strings.Split(s, "\n")
	for _, line := range lines {
		matches := semverExtractRegex.FindStringSubmatch(line)
		if len(matches) > 1 {
			ver := matches[1]
			if strings.HasPrefix(strings.ToLower(matches[0]), "v") && !strings.HasPrefix(ver, "v") {
				ver = "v" + ver
			}
			return ver
		}
	}
	return ""
}

func probeBinaryVersion(binaryPath string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()

	for _, flag := range []string{"--version", "-version", "-v", "version"} {
		cmd := exec.CommandContext(ctx, binaryPath, flag)
		out, err := cmd.CombinedOutput()
		if err == nil && len(out) > 0 {
			ver := extractVersionFromString(string(out))
			if ver != "" {
				return ver
			}
		}
	}
	return ""
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
		log.Warn("no checksum entry found for file", "filename", targetFilename)
		// If verify-checksum is enabled, this is a failure - we can't verify the file
		if r != nil && r.CliParams != nil && r.CliParams.VerifyChecksum {
			return fmt.Errorf("checksum verification enabled but no checksum entry found for %s in %s", targetFilename, checksumFilePath)
		}
		return nil
	}

	if len(expectedHash) != 64 && len(expectedHash) != 128 {
		log.Warn("unknown hash algorithm, skipping verification", "hash_length", len(expectedHash))
		return nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to open downloaded file: %w", err)
	}

	insecure := false
	forceBypass := false
	var repoName, oldCommit, newCommit string
	if r != nil && r.CliParams != nil {
		insecure = r.CliParams.InsecureAllowUnsigned
		forceBypass = r.CliParams.Overwrite && r.CliParams.InsecureAllowUnsigned && r.CliParams.SpecificallyTargeted
		repoName = r.CliParams.Repository
		if st, _ := state.LoadState(); st != nil && st.Apps != nil {
			if app, ok := st.Apps[repoName]; ok {
				if app.CommitHash != "" {
					oldCommit = app.CommitHash
				} else {
					oldCommit = app.Version
				}
			}
		}
		newCommit = r.ResolvedVersion
	}
	v := verification.NewVerifier(insecure)
	if err := v.Verify(verification.ArtifactContext{
		Data:                data,
		Checksum:            expectedHash,
		Repo:                repoName,
		OldCommit:           oldCommit,
		NewCommit:           newCommit,
		GlobalAllowUnsigned: insecure,
		ItemAllowsUnsigned:  insecure,
		ForceBypass:         forceBypass,
	}); err != nil {
		return err
	}

	if r != nil {
		if forceBypass {
			r.VerifiedSigned = false
			r.VerificationMethod = "commit_lineage_bypass"
		} else {
			r.VerifiedSigned = true
			r.VerificationMethod = "checksum"
		}
	}
	log.Info("checksum verified successfully", "filename", targetFilename, "hash", expectedHash)
	return nil
}

func (r *GithubRelease) installWindows(binaryPath string) error {
	if r.CliParams.DryRun {
		log.Infof("[dry-run] Would install windows pkg: %s", binaryPath)
		return nil
	}
	var args []string
	if strings.HasSuffix(strings.ToLower(binaryPath), ".msi") {
		args = []string{"/i", binaryPath, "/qn"}
		if runtime.GOOS != "windows" && (r.CliParams.Wine == "allow" || r.CliParams.Wine == "priority" || r.CliParams.Wine == "force") {
			args = append([]string{"msiexec"}, args...)
		} else {
			args = append([]string{"msiexec"}, args...)
		}
	} else {
		args = []string{"/S"}
		if runtime.GOOS != "windows" && (r.CliParams.Wine == "allow" || r.CliParams.Wine == "priority" || r.CliParams.Wine == "force") {
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
		log.Error("Failed to install Windows package", "error", err)
		return err
	}
	log.Info("Successfully installed Windows package!")
	return nil
}

func (r *GithubRelease) installMac(binaryPath string) error {
	if r.CliParams.DryRun {
		log.Infof("[dry-run] Would install mac pkg: %s", binaryPath)
		return nil
	}
	if strings.HasSuffix(strings.ToLower(binaryPath), ".dmg") {
		log.Info("Mounting DMG...")
		cmd := execCommand("hdiutil", "attach", binaryPath, "-nobrowse", "-mountpoint", "/Volumes/gh-pt-dmg")
		if err := cmd.Run(); err != nil {
			return err
		}
		defer func() { _ = execCommand("hdiutil", "detach", "/Volumes/gh-pt-dmg").Run() }()

		cpCmd := execCommand("sh", "-c", fmt.Sprintf("cp -R /Volumes/gh-pt-dmg/*.app %s/", r.CliParams.TargetPath))
		if err := cpCmd.Run(); err != nil {
			return err
		}
		log.Info("Successfully copied app from DMG!")
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
		log.Info("Successfully installed macOS pkg!")
		return nil
	}
	return fmt.Errorf("unsupported mac installer format: %s", binaryPath)
}

func (r *GithubRelease) GetLatestRelease() (*selector.SelectorItem, error) {
	var prerelease, stable bool
	if r.CliParams != nil {
		prerelease = r.CliParams.Prerelease
		stable = r.CliParams.Stable
	}
	releaseSelector, err := selector.ReleaseSelector(r.Client, r.CliParams.Repository, r.CliParams.ReleaseVersion, r.CliParams.Interactive, prerelease, stable)
	if err != nil {
		return nil, formatGitHubError(err, r.CliParams.Repository)
	}
	releases, err := releaseSelector.Run()
	if err != nil {
		return nil, formatGitHubError(err, r.CliParams.Repository)
	}
	return releases[0], nil
}

func (r *GithubRelease) Install() error {
	if r.CliParams != nil {
		if r.CliParams.NoEmojis {
			r.CliParams.DisableIcons = true
		}
		if r.CliParams.NoColor || os.Getenv("NO_COLOR") != "" {
			r.CliParams.NoColor = true
			_ = os.Setenv("NO_COLOR", "1")
			pterm.DisableColor()
		}
	}

	var pUI interface {
		Start()
		Stop()
		Pause()
		Resume()
		Update(step int, a string, b string, c string, target string, e string)
		UpdateSymlink(symlink string)
		AddAsset(name string, fullName string, symlink string, installCmd string)
		SetCurrentAsset(index int)
		SetAssetInstallCmd(installCmd string)
		WaitForAnimation()
	}
	if (r.CliParams != nil && r.CliParams.Verbose) || (r.CliParams != nil && r.CliParams.ProgressBar == "none") {
		pUI = &ui.NullProgressBar{}
		r.UI = pUI
		ui.GlobalPacman = nil
	} else if r.CliParams != nil && r.CliParams.ProgressBar == "conveyor" {
		cUI := &ui.ConveyorUI{Repo: r.CliParams.Repository}
		if r.CliParams.DisableIcons || r.CliParams.NoEmojis {
			cUI.DisableIcons = true
		}
		r.UI = cUI
		pUI = cUI
	} else {
		repo := ""
		if r.CliParams != nil {
			repo = r.CliParams.Repository
		}
		pacUI := ui.NewPacmanUI(repo)
		if r.CliParams != nil && (r.CliParams.DisableIcons || r.CliParams.NoEmojis) {
			pacUI.DisableIcons = true
		}
		r.UI = pacUI
		ui.GlobalPacman = pacUI
		pUI = pacUI
	}
	pUI.Update(0, "", "", "", r.CliParams.TargetPath, "")
	pUIStarted := false
	startUI := func() {
		if pUI != nil && !pUIStarted {
			pUI.Start()
			pUIStarted = true
		}
	}
	if r.CliParams == nil || !r.CliParams.Interactive {
		startUI()
	}
	defer func() {
		if pUI != nil {
			pUI.Stop()
		}
	}()

	// Auto-enable IncludeSidecars and Symlink when any sidecar, driver, or plugin param is specified
	if !r.CliParams.IncludeSidecars {
		if r.CliParams.Sidecars != "" || len(r.CliParams.SidecarSymlinkTo) > 0 || r.CliParams.AISetupSidecars || r.CliParams.SidecarMode != "" || (r.CliParams.Driver != "" && r.CliParams.Driver != "none") || (r.CliParams.Plugin != "" && r.CliParams.Plugin != "none") {
			r.CliParams.IncludeSidecars = true
			log.Debug("auto-enabled --include-sidecars due to sidecar/driver/plugin params")
		}
	}
	if (r.CliParams.Driver != "" && r.CliParams.Driver != "none") || (r.CliParams.Plugin != "" && r.CliParams.Plugin != "none") {
		r.CliParams.Symlink = true
	}
	if r.CliParams.IncludeSidecars && r.CliParams.SidecarMode == "" {
		r.CliParams.SidecarMode = "auto"
	}

	var prerelease, stable bool
	if r.CliParams != nil {
		prerelease = r.CliParams.Prerelease
		stable = r.CliParams.Stable
	}
	releaseSelector, err := selector.ReleaseSelector(r.Client, r.CliParams.Repository, r.CliParams.ReleaseVersion, r.CliParams.Interactive, prerelease, stable)
	if err != nil {
		return formatGitHubError(err, r.CliParams.Repository)
	}
	releases, err := releaseSelector.Run()
	if err != nil {
		return formatGitHubError(err, r.CliParams.Repository)
	}

	// Try each release up to FallbackReleases times if no assets found
	var assets []*selector.SelectorItem
	var selectedRelease *selector.SelectorItem
	maxAttempts := 1
	if r.CliParams.FallbackReleases > 0 {
		maxAttempts = r.CliParams.FallbackReleases + 1
	}
	if maxAttempts > len(releases) {
		maxAttempts = len(releases)
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		release := releases[attempt]
		r.ResolvedVersion = release.Name
		if pUI != nil {
			pUI.Update(1, r.ResolvedVersion, "", "", "", "")
		}

		avxLvl := ""
		if r.CliParams != nil {
			avxLvl = r.CliParams.AvxLevel
		}
		assetSelector, err := selector.AssetSelector(r.Client, r.CliParams.Repository, selector.AssetMatchCriteria{
			ReleaseId:        release.Id,
			Name:             r.CliParams.ReleaseAsset,
			Regexps:          r.CliParams.ReleaseAssetRegexps,
			Interactive:      r.CliParams.Interactive,
			AllowForeignArch: r.CliParams.AllowForeignArch,
			Single:           !r.CliParams.Interactive && !r.CliParams.All,
			AvxLevel:         avxLvl,
		})
		if err != nil {
			log.Error("could not create release asset selector", "repository", r.CliParams.Repository, "release id", release.Id, "release name", release.Name, "asset name matcher", r.CliParams.ReleaseAsset, "error", err)
			return err
		}
		assets, err = assetSelector.Run()
		if err == nil {
			selectedRelease = release
			if attempt > 0 {
				log.Info("found assets in older release", "repository", r.CliParams.Repository, "release", release.Name, "attempt", attempt+1)
			}
			break
		}

		if attempt < maxAttempts-1 {
			nextRelease := releases[attempt+1]
			log.Warn("Unable to find release assets for platform to install in version => attempting to find platform assets in the prior version",
				"repository", r.CliParams.Repository,
				"platform", runtime.GOOS+"/"+runtime.GOARCH,
				"failed_version", release.Name,
				"next_version", nextRelease.Name,
				"error", err)
		}
	}

	if assets == nil || selectedRelease == nil {
		if r.CliParams.SearchForInstallInstructionsIfNoReleaseAssets {
			r.fallbackToReadmeInstructions()
		}
		log.Error("could not select release asset after trying multiple releases", "repository", r.CliParams.Repository, "release asset name matcher", r.CliParams.ReleaseAsset, "releases_tried", maxAttempts)
		return fmt.Errorf("no matching assets found in %d releases for %s", maxAttempts, r.CliParams.Repository)
	}

	// Update releases to use the selected release
	releases = []*selector.SelectorItem{selectedRelease}

	// --- STATUS ABORTION CHECK ---
	st, _ := state.LoadState()
	var inState bool
	var prevVersion string
	var alreadyInstalled bool
	if st != nil && st.Apps != nil {
		if app, ok := st.Apps[r.CliParams.Repository]; ok {
			inState = true
			prevVersion = app.Version
			if app.TargetPath != "" {
				alreadyInstalled = true
				if len(app.InstalledBinaries) > 0 {
					for _, name := range app.InstalledBinaries {
						if _, err := os.Stat(filepath.Join(app.TargetPath, name)); err != nil {
							alreadyInstalled = false
							break
						}
					}
					if prevVersion == "" && alreadyInstalled {
						prevVersion = probeBinaryVersion(filepath.Join(app.TargetPath, app.InstalledBinaries[0]))
					}
				} else {
					if fi, err := os.Stat(app.TargetPath); err != nil || !fi.IsDir() {
						alreadyInstalled = false
					}
				}
			}
		}
	}

	if !inState && r.CliParams != nil && r.CliParams.TargetPath != "" {
		expectedCleanName := filepath.Base(r.CliParams.Repository)
		if len(assets) > 0 && !strings.Contains(assets[0].Name, "tar") && !strings.HasSuffix(assets[0].Name, ".zip") && !strings.HasSuffix(assets[0].Name, ".7z") {
			expectedCleanName = GenerateCleanName(assets[0].Name, r.CliParams.Repository, releases[0].Name)
		}
		if len(r.CliParams.Rename) > 0 {
			if val, ok := r.CliParams.Rename[expectedCleanName]; ok {
				expectedCleanName = val
			}
		}
		expectedDestPath := filepath.Join(r.CliParams.TargetPath, expectedCleanName)
		if fi, err := os.Stat(expectedDestPath); err == nil && !fi.IsDir() {
			alreadyInstalled = true
			if prevVersion == "" {
				prevVersion = probeBinaryVersion(expectedDestPath)
			}
		}
	}

	sort.Slice(assets, func(i, j int) bool {
		scoreI := getScore(assets[i].Name, r.CliParams.Type)
		scoreJ := getScore(assets[j].Name, r.CliParams.Type)
		return scoreI > scoreJ
	})

	// Determine the exact clean types of the chosen assets to save in state and use in status output.
	// This ensures that when the state is saved, future updates will strictly seek this exact format
	// and state never stores raw regex search patterns.
	var exactTypes []string
	seenTypes := make(map[string]bool)
	for _, asset := range assets {
		ct := cleanAssetType(asset.Name)
		if !seenTypes[ct] {
			seenTypes[ct] = true
			exactTypes = append(exactTypes, ct)
		}
	}
	if len(exactTypes) > 0 {
		r.CliParams.Type = exactTypes
	}

	installState := status.InstallState{
		InState:           inState,
		AlreadyInstalled:  alreadyInstalled,
		PrevVersion:       prevVersion,
		NewVersion:        releases[0].Name,
		AppName:           r.CliParams.Repository,
		Type:              cleanAssetType(assets[0].Name),
		Repo:              r.CliParams.Repository,
		AssetName:         assets[0].Name,
		Force:             r.CliParams.Overwrite,
		AllowDowngrade:    r.CliParams.AllowDowngrade,
		LeRetrogrouch:     r.CliParams.LeRetrogrouch,
		RetrogradeStopgap: r.CliParams.RetrogradeStopgap,
		Barbarous:         r.CliParams.Barbarous,
		SelfInflictedDebt: r.CliParams.SelfInflictedDebt,
		IsUpgradeCmd:      r.CliParams.IsUpgradeCmd,
	}

	statusMsg, abortErr := status.GenerateStatusMessage(installState)
	if abortErr != nil {
		return abortErr
	}
	r.StatusMessage = statusMsg // We need to add StatusMessage to GithubRelease struct

	// Automatically allow overwriting for version upgrades
	if installState.InState && status.CompareVersions(installState.PrevVersion, installState.NewVersion) > 0 {
		r.CliParams.Overwrite = true
	}

	if pUI != nil {
		ghostType := "🍒"
		if alreadyInstalled {
			if r.CliParams.Overwrite {
				if (r.CliParams != nil && r.CliParams.NoColor) || os.Getenv("NO_COLOR") != "" {
					ghostType = "👻"
				} else {
					ghostType = "\033[5m👻\033[0m" // flashing ghost
				}
			} else {
				ghostType = "ᗣ" // solid ghost
			}
		}
		pUI.Update(2, "", assets[0].Name, "", "", ghostType)
	}

	// Generate strict regexes for the chosen assets to lock them down for future updates
	var strictRegexes []string
	for _, asset := range assets {
		strictRegex := generateStrictAssetRegex(asset.Name, releases[0].Name)
		strictRegexes = append(strictRegexes, strictRegex)
	}
	r.CliParams.ReleaseAssetRegexp = strings.Join(strictRegexes, " | ")

	// Early check if the expected destination already exists to fail fast
	if len(assets) > 0 {
		isSystem := false
		for _, asset := range assets {
			bt := selector.BinaryTypeFromPath(asset.Name)
			if bt == selector.BinaryDebInstaller || bt == selector.BinaryRpmInstaller || bt == selector.BinaryPacmanInstaller || bt == selector.BinaryPkgInstaller || bt == selector.BinaryMacInstaller || bt == selector.BinaryWindowsInstaller {
				isSystem = true
				break
			}
		}

		if !isSystem {
			expectedCleanName := filepath.Base(r.CliParams.Repository)
			if !strings.Contains(assets[0].Name, "tar") && !strings.HasSuffix(assets[0].Name, ".zip") && !strings.HasSuffix(assets[0].Name, ".7z") {
				expectedCleanName = GenerateCleanName(assets[0].Name, r.CliParams.Repository, releases[0].Name)
			}
			if len(r.CliParams.Rename) > 0 {
				if val, ok := r.CliParams.Rename[expectedCleanName]; ok {
					expectedCleanName = val
				}
			}
			expectedDestPath := filepath.Join(r.CliParams.TargetPath, expectedCleanName)

			if fi, err := os.Stat(expectedDestPath); err == nil && !fi.IsDir() {
				if r.CliParams.Interactive {
					if !r.interactiveConfirm(fmt.Sprintf("'%s' already exists. Overwrite?", expectedDestPath)) {
						return fmt.Errorf("%s already exists and user did not want to overwrite", expectedDestPath)
					}
					r.CliParams.Overwrite = true
				} else if !r.CliParams.Overwrite {
					return fmt.Errorf("%s already exists and -f/--force is not set", expectedDestPath)
				}
			}
		}
	}

	downloadDir, err := safety.SafeMkdirTemp("", "gh-pt-download-*")
	if err != nil {
		log.Error("could not create temporary download directory", "error", err)
		return err
	}
	safety.SetScopedTempDir(downloadDir)
	defer func() {
		safety.SetScopedTempDir("")
		err = errors.Join(err, safety.RemoveAll(downloadDir))
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
				log.Info("found checksum file, downloading", "checksum_file", checksumFileName)

				_, _, execErr := ghExec("release", "download", releases[0].Name,
					"--repo", r.CliParams.Repository, "--pattern", checksumFileName, "--dir", downloadDir)
				if execErr != nil {
					log.Warn("failed to download checksum file, skipping verification", "error", execErr)
				} else {
					checksumFilePath = filepath.Join(downloadDir, checksumFileName)
				}
			}
		}
	}

	for _, asset := range assets {
		var binaries []*selector.SelectorItem
		var execErr error

		if r.CliParams.DryRun {
			log.Infof("[dry-run] Inspecting asset %s without writing to disk", asset.Name)
			lower := strings.ToLower(asset.Name)
			if strings.HasSuffix(lower, ".zip") {
				zipItems, err := r.PeekRemoteZip(asset, releases[0].Name)
				if err != nil {
					log.Warn("remote zip peeking encountered error, falling back", "error", err)
				}
				binaries = zipItems
			} else if strings.Contains(lower, "tar") || strings.HasSuffix(lower, ".tgz") {
				tarItems, err := r.PeekRemoteTar(asset, releases[0].Name, nil)
				if err != nil {
					log.Warn("remote tar peeking encountered error, falling back", "error", err)
				}
				binaries = tarItems
			} else {
				binaries = []*selector.SelectorItem{
					{
						Name:         asset.Name,
						DownloadPath: filepath.Join(downloadDir, asset.Name),
						Compressed:   false,
						BinaryType:   selector.BinaryTypeFromPath(asset.Name),
					},
				}
			}

			// If archive inspection found candidate items inside, filter them via binary selector
			if len(binaries) > 0 && (strings.HasSuffix(lower, ".zip") || strings.Contains(lower, "tar") || strings.HasSuffix(lower, ".tgz")) {
				sel := &selector.Selector{
					Kind:           selector.Binary,
					Items:          binaries,
					NamesMatcher:   r.CliParams.AssetBinaries,
					RegexpMatchers: []string{r.CliParams.AssetBinariesRegexp},
					Single:         r.CliParams.OnlyFirstMatch,
					Repository:     r.CliParams.Repository,
					OnlyFirstMatch: r.CliParams.OnlyFirstMatch,
					MaxExeInstalls: r.CliParams.MaxExeInstalls,
				}
				filteredBinaries, selErr := sel.Run()
				if selErr == nil && len(filteredBinaries) > 0 {
					binaries = filteredBinaries
				}
				r.ContainingArchive = asset.Name
			} else if len(binaries) == 0 {
				cleanName := GenerateCleanName(asset.Name, r.CliParams.Repository, releases[0].Name)
				binaries = []*selector.SelectorItem{
					{
						Name:         cleanName,
						DownloadPath: filepath.Join(downloadDir, cleanName),
						Compressed:   false,
						BinaryType:   selector.BinaryTypeFromPath(asset.Name),
					},
				}
			}
		} else {
			startUI()
			if r.CliParams.Interactive {
				if pUI != nil {
					pUI.Update(3, "", "", "", "", "")
				}
			}

			stdOut, stdErr, ghErr := ghExec("release", "download", releases[0].Name,
				"--repo", r.CliParams.Repository, "--pattern", asset.Name, "--dir", downloadDir)
			if ghErr != nil {
				execErr = fmt.Errorf("failed to run gh command: %s", stdErr.String())
				log.Error("could not download release asset", "repository", r.CliParams.Repository, "release id", releases[0].Id, "release name", releases[0].Name, "release asset name", asset.Name, "download directory", downloadDir, "error", execErr)
				if r.CliParams.Interactive {
					if pUI != nil {
						pUI.Stop()
						fmt.Printf("\nFailed - '%s' failed\n", stdErr.String())
					}
				}
				return execErr
			}

			if r.CliParams.Interactive {
				if pUI != nil {
					pUI.Update(4, "", "", "", "", "")
				}
			}

			log.Info("downloaded release asset", "repository", r.CliParams.Repository, "release id", releases[0].Id, "release name", releases[0].Name, "release asset name", asset.Name, "download directory", downloadDir, "output", stdOut.String())

			downloadedAssetPath := filepath.Join(downloadDir, asset.Name)

			// Content-type magic bytes verification (Assumption 4): verify file magic matches filename
			headerBuf := make([]byte, 512)
			if f, fErr := os.Open(downloadedAssetPath); fErr == nil {
				n, _ := f.Read(headerBuf)
				_ = f.Close()
				if n >= 4 {
					consistent, reason := selector.VerifyAssetContentConsistency(asset.Name, headerBuf[:n])
					if !consistent {
						log.Error("asset content-type magic mismatch (potential spoofing)", "asset", asset.Name, "reason", reason)
						return fmt.Errorf("security error: asset %s content does not match extension: %s", asset.Name, reason)
					}
				}
			}

			if checksumFilePath != "" && r.CliParams.VerifyChecksum {
				if err := r.verifyChecksum(downloadedAssetPath, checksumFilePath); err != nil {
					log.Error("checksum verification failed", "error", err, "asset", asset.Name)
					return fmt.Errorf("checksum verification failed for %s: %w", asset.Name, err)
				}
			} else if r.CliParams.IsUpgradeCmd {
				data, readErr := os.ReadFile(downloadedAssetPath)
				if readErr != nil {
					return fmt.Errorf("failed to read downloaded asset %s: %w", asset.Name, readErr)
				}
				forceBypass := r.CliParams.Overwrite && r.CliParams.InsecureAllowUnsigned && r.CliParams.SpecificallyTargeted
				var repoName, oldCommit, newCommit string
				repoName = r.CliParams.Repository
				if st, _ := state.LoadState(); st != nil && st.Apps != nil {
					if app, ok := st.Apps[repoName]; ok {
						if app.CommitHash != "" {
							oldCommit = app.CommitHash
						} else {
							oldCommit = app.Version
						}
					}
				}
				newCommit = r.ResolvedVersion
				v := verification.NewVerifier(r.CliParams.InsecureAllowUnsigned)
				if err := v.Verify(verification.ArtifactContext{
					Data:                data,
					Repo:                repoName,
					OldCommit:           oldCommit,
					NewCommit:           newCommit,
					GlobalAllowUnsigned: r.CliParams.InsecureAllowUnsigned,
					ItemAllowsUnsigned:  r.CliParams.InsecureAllowUnsigned,
					ForceBypass:         forceBypass,
				}); err != nil {
					log.Error("verification failed for unsigned artifact", "error", err, "asset", asset.Name)
					return fmt.Errorf("artifact verification failed for %s: %w", asset.Name, err)
				}
				r.VerifiedSigned = false
				r.VerificationMethod = "commit_lineage"
			}

			if r.CliParams.VTApiKey != "" {
				vtHash, hashErr := CalculateSHA256(downloadedAssetPath)
				if hashErr != nil {
					return fmt.Errorf("failed to calculate SHA-256 for VirusTotal: %w", hashErr)
				}
				err := VerifyHashWithVirusTotal(vtHash, downloadedAssetPath, r.CliParams.VTApiKey, r.CliParams.Interactive && !r.CliParams.DisablePrompts, r.CliParams.SkipVtSandbox)
				if err != nil {
					return err
				}
			}

			binarySelector, selErr := selector.BinarySelector(selector.BinaryMatchCriteria{
				DownloadPath:   filepath.Join(downloadDir, asset.Name),
				Names:          r.CliParams.AssetBinaries,
				Matcher:        r.CliParams.AssetBinariesRegexp,
				Interactive:    r.CliParams.Interactive,
				Extractor:      r.CliParams.Extractor,
				Repository:     r.CliParams.Repository,
				OnlyFirstMatch: r.CliParams.OnlyFirstMatch,
				MaxExeInstalls: r.CliParams.MaxExeInstalls,
			})
			if selErr != nil {
				log.Error("could not create release asset binary selector", "repository", r.CliParams.Repository, "release id", releases[0].Id, "release name", releases[0].Name, "release asset name", asset.Name, "downloaded asset", filepath.Join(downloadDir, asset.Name), "asset binary name matchers", r.CliParams.AssetBinaries, "asset binary regexp matcher", params.TruncateRegex(r.CliParams.AssetBinariesRegexp, 200), "error", selErr)
				return selErr
			}
			binaries, execErr = binarySelector.Run()
			if execErr != nil {
				log.Error("could not select release asset binary", "repository", r.CliParams.Repository, "release id", releases[0].Id, "release name", releases[0].Name, "release asset name", asset.Name, "downloaded asset", filepath.Join(downloadDir, asset.Name), "asset binary name matchers", r.CliParams.AssetBinaries, "asset binary regexp matcher", params.TruncateRegex(r.CliParams.AssetBinariesRegexp, 200), "error", execErr)
				return execErr
			}
		}

		if r.CliParams.OnlyFirstMatch && len(binaries) > 1 {
			binaries = binaries[:1]
		}
		if r.CliParams.MaxExeInstalls > 0 && len(binaries) > r.CliParams.MaxExeInstalls {
			binaries = binaries[:r.CliParams.MaxExeInstalls]
		}

		if r.CliParams.Interactive && pUI != nil {
			var bNames []string
			for _, b := range binaries {
				bNames = append(bNames, b.Name)
			}
			pUI.Update(4, "", "", strings.Join(bNames, ", "), "", "")
		}

		// Section F: Orphaned Asset Heuristics & Interactive Prompting
		var suspectedSidecars []string
		suspectedSet := make(map[string]bool)

		// 1. Scan unselected GitHub release assets
		var allReleaseAssets []struct{ Name string }
		if err := r.Client.Get(fmt.Sprintf("repos/%s/releases/%d/assets", r.CliParams.Repository, releases[0].Id), &allReleaseAssets); err == nil {
			selectedAssetMap := make(map[string]bool)
			for _, a := range assets {
				selectedAssetMap[a.Name] = true
			}
			for _, a := range allReleaseAssets {
				if !selectedAssetMap[a.Name] && isSuspectedRemoteSidecar(a.Name) {
					if !suspectedSet[a.Name] {
						suspectedSet[a.Name] = true
						suspectedSidecars = append(suspectedSidecars, a.Name)
					}
				}
			}
		}

		// 2. Scan discarded files in local extraction directory
		var extractDir string
		var fsObj fs.FS
		if len(binaries) > 0 {
			extractDir = binaries[0].ExtractDir
			fsObj = binaries[0].Fs

			selectedBinaries := make(map[string]bool)
			for _, b := range binaries {
				selectedBinaries[filepath.Clean(b.DownloadPath)] = true
				selectedBinaries[b.Name] = true
			}

			if extractDir != "" {
				_ = filepath.Walk(extractDir, func(path string, info os.FileInfo, err error) error {
					if err != nil || info.IsDir() {
						return nil
					}
					if selectedBinaries[filepath.Clean(path)] || selectedBinaries[info.Name()] {
						return nil
					}
					rel, err := filepath.Rel(extractDir, path)
					if err != nil {
						rel = info.Name()
					}
					if isSuspectedLocalSidecar(rel, info.Name()) {
						if !suspectedSet[rel] {
							suspectedSet[rel] = true
							suspectedSidecars = append(suspectedSidecars, rel)
						}
					}
					return nil
				})
			} else if fsObj != nil {
				_ = fs.WalkDir(fsObj, ".", func(fsPath string, d fs.DirEntry, err error) error {
					if err != nil || d.IsDir() {
						return nil
					}
					if selectedBinaries[fsPath] || selectedBinaries[d.Name()] {
						return nil
					}
					if isSuspectedLocalSidecar(fsPath, d.Name()) {
						if !suspectedSet[fsPath] {
							suspectedSet[fsPath] = true
							suspectedSidecars = append(suspectedSidecars, fsPath)
						}
					}
					return nil
				})
			}
		}

		if len(suspectedSidecars) > 0 {
			// If IncludeSidecars is enabled, auto-include all suspected sidecars
			if r.CliParams.IncludeSidecars {
				targetDir := r.resolveSidecarTargetPath()
				if err := os.MkdirAll(targetDir, 0755); err != nil {
					log.Warn("could not create sidecar target directory", "error", err)
				}

				for _, item := range suspectedSidecars {
					if isLicenseFileName(item) {
						continue
					}
					// Deploy the sidecar
					if extractDir != "" {
						src := filepath.Join(extractDir, item)
						if fi, err := os.Stat(src); err == nil && !fi.IsDir() {
							dst := filepath.Join(targetDir, filepath.Base(item))
							if err := copyFile(src, dst, 0755); err == nil {
								r.InstalledSidecars = append(r.InstalledSidecars, dst)
								continue
							}
						}
					}
					if fsObj != nil {
						if sf, err := fsObj.Open(item); err == nil {
							dst := filepath.Join(targetDir, filepath.Base(item))
							_ = os.Chmod(filepath.Dir(dst), 0755)
							_ = os.Remove(dst)
							if df, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755); err == nil {
								_, _ = io.Copy(df, sf)
								_ = df.Close()
								r.InstalledSidecars = append(r.InstalledSidecars, dst)
							}
							_ = sf.Close()
							continue
						}
					}
				}
				log.Info("auto-included suspected sidecar assets", "count", len(suspectedSidecars))
			} else {
				// Otherwise, use the existing heuristic handling (warn or prompt)
				_, _ = r.handleSuspectedSidecars(suspectedSidecars, extractDir, fsObj, releases[0].Id)
			}
		}

		// Explicit --sidecars pattern matching
		if r.CliParams.Sidecars != "" {
			_ = r.extractExplicitSidecars(extractDir, fsObj)

			// Run AI setup if requested
			if r.CliParams.AISetupSidecars {
				if err := r.runAISidecarSetup(); err != nil {
					log.Warn("AI sidecar setup failed", "error", err)
				}
			}
		}

		binariesOutput := make(map[string]string)

		// Separate system installers from regular binaries
		var regularBinaries []*selector.SelectorItem
		var systemInstallers []*selector.SelectorItem
		for _, binary := range binaries {
			switch binary.BinaryType {
			case selector.BinaryDebInstaller, selector.BinaryRpmInstaller, selector.BinaryPacmanInstaller, selector.BinaryPkgInstaller, selector.BinaryMacInstaller:
				systemInstallers = append(systemInstallers, binary)
			default:
				regularBinaries = append(regularBinaries, binary)
			}
		}

		if r.CliParams.Symlink {
			if len(regularBinaries) > 0 {
				if regularBinaries[0].Compressed || cleanAssetType(asset.Name) != "binary" {
					r.ContainingArchive = asset.Name
				}
				for _, binary := range regularBinaries {
					r.InstalledAssetFullNames = append(r.InstalledAssetFullNames, asset.Name)
					cleanName := GenerateCleanName(binary.Name, r.CliParams.Repository, releases[0].Name)
					r.InstalledAssetCleanNames = append(r.InstalledAssetCleanNames, cleanName)
				}
				symlinkDir, err := r.executeSymlinkInstall(regularBinaries, filepath.Join(downloadDir, asset.Name))
				if err != nil {
					return err
				}
				if pUI != nil {
					pUI.UpdateSymlink(symlinkDir)
					pUI.Update(6, "", "", "", "", "")
				}
			}
			if len(systemInstallers) == 0 {
				continue
			}
			binaries = systemInstallers
		}

		// Add all binaries to UI for animation
		if pUI != nil {
			for _, binary := range binaries {
				cleanName := GenerateCleanName(binary.Name, r.CliParams.Repository, releases[0].Name)
				fullName := filepath.Join(r.CliParams.TargetPath, cleanName)
				pUI.AddAsset(cleanName, fullName, "", "")
			}
		}

		for binaryIdx, binary := range binaries {
			log.Info("processing selected release asset binary", "repository", r.CliParams.Repository, "release id", releases[0].Id, "release name", releases[0].Name, "release asset name", asset.Name, "downloaded asset", filepath.Join(downloadDir, asset.Name), "release asset binary", binary.Name)

			actualDownloadPath := binary.DownloadPath

			if binary.Compressed && binary.BinaryType != selector.BinaryExecutable {
				log.Debug("extracting archived installer to temporary directory", "release asset binary", binary.Name)

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

			// Update UI to current asset
			if pUI != nil {
				pUI.SetCurrentAsset(binaryIdx)
			}

			var installCmd string
			switch binary.BinaryType {
			case selector.BinaryDebInstaller:
				log.Debug("binary is a deb installer")
				binariesOutput[binary.Name] = "deb"
				installCmd = fmt.Sprintf("sudo apt install %s", actualDownloadPath)
				execErr = r.installDeb(actualDownloadPath)
			case selector.BinaryRpmInstaller:
				log.Debug("binary is a rpm installer")
				binariesOutput[binary.Name] = "rpm"
				installCmd = fmt.Sprintf("sudo dnf install %s", actualDownloadPath)
				execErr = r.installRpm(actualDownloadPath)
			case selector.BinaryPacmanInstaller:
				log.Debug("binary is a pacman installer")
				binariesOutput[binary.Name] = "pacman"
				installCmd = fmt.Sprintf("sudo pacman -U %s", actualDownloadPath)
				execErr = r.installPacman(actualDownloadPath)
			case selector.BinaryPkgInstaller:
				log.Debug("binary is a freebsd pkg/txz installer")
				binariesOutput[binary.Name] = "pkg"
				installCmd = fmt.Sprintf("sudo pkg install %s", actualDownloadPath)
				execErr = r.installPkg(actualDownloadPath)
			case selector.BinaryMacInstaller:
				log.Debug("binary is a mac installer")
				binariesOutput[binary.Name] = "mac"
				installCmd = fmt.Sprintf("sudo installer -pkg %s -target /", actualDownloadPath)
				execErr = r.installMac(actualDownloadPath)
			case selector.BinaryWindowsInstaller:
				log.Debug("binary is a windows installer")
				binariesOutput[binary.Name] = "windows"
				installCmd = fmt.Sprintf("msiexec /i %s", actualDownloadPath)
				execErr = r.installWindows(actualDownloadPath)
			default:
				log.Debug("binary is a plain executable")
				binariesOutput[binary.Name] = "binary"
				if binary.Compressed {
					execErr = r.installArchivedBinary(binary.Fs, binary.FsPath)
				} else {
					execErr = r.installBinary(actualDownloadPath)
				}
			}

			// Update UI with install command
			if pUI != nil && installCmd != "" {
				pUI.SetAssetInstallCmd(installCmd)
			}

			if execErr != nil {
				log.Error("could not install release asset binary", "repository", r.CliParams.Repository, "release id", releases[0].Id, "release name", releases[0].Name, "release asset name", asset.Name, "downloaded asset", filepath.Join(downloadDir, asset.Name), "release asset binary", binary.Name, "error", execErr)
				return execErr
			}

			// Track asset information for state
			r.InstalledAssetFullNames = append(r.InstalledAssetFullNames, asset.Name)
			cleanName := GenerateCleanName(binary.Name, r.CliParams.Repository, releases[0].Name)
			r.InstalledAssetCleanNames = append(r.InstalledAssetCleanNames, cleanName)
			if binary.Compressed || cleanAssetType(asset.Name) != "binary" {
				r.ContainingArchive = asset.Name
			}
		}
	}

	// Finish and stop progress UI before invoking package managers or interactive installers,
	// so the terminal is completely released and clean for package manager prompts and output.
	if len(r.PendingDebs) > 0 || len(r.PendingRpms) > 0 {
		if pUI != nil {
			pUI.Update(5, "", "", "", "", "")
			pUI.WaitForAnimation()
			pUI.Stop()
			pUI = nil
		}
	}

	if len(r.PendingDebs) > 0 {
		// Check for binary collisions between .deb packages and standalone binaries
		// Collect standalone binaries that would be installed alongside debs
		for _, p := range r.PendingDebs {
			// Check each deb for binary collisions with other pending binaries
			collisions, err := CheckDebBinaryCollision(p, r.getStandaloneBinariesForDeb(p))
			if err != nil {
				log.Warnf("Failed to check deb contents for %s: %v", filepath.Base(p), err)
			} else if len(collisions) > 0 {
				log.Warnf("Binary collision detected in %s: %v", filepath.Base(p), collisions)
				log.Warn("The following binaries exist in both the .deb package and as standalone releases. They will be installed twice (PATH shadowing may occur):")
				for name, path := range collisions {
					log.Warnf("  - %s (from deb: %s)", name, path)
				}
			}
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
		} else if r.CliParams.ResolveDeps || r.CliParams.DisablePrompts {
			args = append([]string{"apt-get", "install", "-y"}, baseDebs...)
		} else {
			args = append([]string{"apt-get", "install"}, baseDebs...)
		}

		if r.CliParams.DryRun {
			log.Infof("[dry-run] Would execute batched package install: sudo %s", strings.Join(args, " "))
			r.InstalledPkgManager = args[0]
			r.InstalledPackageIDs = append(r.InstalledPackageIDs, r.InstalledPackageNames...)
		} else {
			if err := r.ensureSudo(); err != nil {
				return err
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
			log.Infof("Executing batched package install: sudo %s", strings.Join(args, " "))
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("batched .deb installation failed: %w", err)
			}
			r.InstalledPkgManager = args[0]
			r.InstalledPackageIDs = append(r.InstalledPackageIDs, r.InstalledPackageNames...)
		}
	}

	if len(r.PendingRpms) > 0 {
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
		} else if r.CliParams.ResolveDeps || r.CliParams.DisablePrompts {
			args = append([]string{"dnf", "localinstall", "-y"}, baseRpms...)
		} else {
			args = append([]string{"dnf", "localinstall"}, baseRpms...)
		}

		if r.CliParams.DryRun {
			log.Infof("[dry-run] Would execute batched package install: sudo %s", strings.Join(args, " "))
			r.InstalledPkgManager = args[0]
			r.InstalledPackageIDs = append(r.InstalledPackageIDs, r.InstalledPackageNames...)
		} else {
			if err := r.ensureSudo(); err != nil {
				return err
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
			log.Infof("Executing batched package install: sudo %s", strings.Join(args, " "))
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("batched .rpm installation failed: %w", err)
			}
			r.InstalledPkgManager = args[0]
			r.InstalledPackageIDs = append(r.InstalledPackageIDs, r.InstalledPackageNames...)
		}
	}

	if pUI != nil {
		pUI.Update(5, "", "", "", "", "")
		time.Sleep(500 * time.Millisecond) // Let the animation finish eating dots
	}

	var filteredSidecars []string
	for _, sc := range r.InstalledSidecars {
		if !isLicenseFileName(sc) {
			filteredSidecars = append(filteredSidecars, sc)
		}
	}
	r.InstalledSidecars = filteredSidecars

	if !r.CliParams.NoSaveState && !r.CliParams.DryRun {
		st, err := state.LoadState()
		if err == nil {
			_ = st.AddApp(&state.InstalledApp{
				Repository:               r.CliParams.Repository,
				TargetPath:               r.CliParams.TargetPath,
				Global:                   r.CliParams.Global,
				ReleaseAsset:             r.CliParams.ReleaseAsset,
				ReleaseRegexp:            r.CliParams.ReleaseAssetRegexp,
				Version:                  releases[0].Name,
				Rename:                   r.CliParams.Rename,
				Type:                     r.CliParams.Type,
				All:                      r.CliParams.All,
				AssetBinaries:            r.CliParams.AssetBinaries,
				AssetBinariesRegexp:      r.CliParams.AssetBinariesRegexp,
				InstalledBinaries:        r.InstalledBinaries,
				InstalledAssetNames:      r.InstalledAssetCleanNames,
				InstalledAssetsFullNames: r.InstalledAssetFullNames,
				ContainingArchive:        r.ContainingArchive,
				PackageNames:             r.InstalledPackageNames,
				Pinned:                   r.CliParams.PinInstall,
				Extractor:                r.CliParams.Extractor,
				IsPrerelease:             releases[0].Prerelease,
				Sidecars:                 r.CliParams.Sidecars,
				SidecarSymlinkTo:         r.SidecarSymlinkTo,
				IncludeSidecars:          r.CliParams.IncludeSidecars,
				SidecarMode:              r.CliParams.SidecarMode,
				Driver:                   r.CliParams.Driver,
				Plugin:                   r.CliParams.Plugin,
				InstalledSidecars:        r.InstalledSidecars,
				FallbackReleases:         r.CliParams.FallbackReleases,
				InsecureAllowUnsigned:    r.CliParams.InsecureAllowUnsigned,
				WasSigned:                r.VerifiedSigned,
				VerificationMethod:       r.VerificationMethod,
			})

			// Save to install-map
			var depsStep *state.InstallStep
			if r.DepsPkgManager != "" || len(r.DepsPackageIDs) > 0 || len(r.DepsFiles) > 0 {
				var pkgInfo *state.InstallPkg
				if r.DepsPkgManager != "" || len(r.DepsPackageIDs) > 0 {
					pkgInfo = &state.InstallPkg{
						Manager:   r.DepsPkgManager,
						PackageID: strings.Join(r.DepsPackageIDs, ", "),
					}
				}
				depsStep = &state.InstallStep{
					Pkg:   pkgInfo,
					Files: r.DepsFiles,
				}
			}

			allInstalledFiles := make([]string, 0)
			seenFiles := make(map[string]bool)
			for _, f := range r.InstalledFiles {
				if f != "" && !seenFiles[f] {
					seenFiles[f] = true
					allInstalledFiles = append(allInstalledFiles, f)
				}
			}
			for _, sc := range r.InstalledSidecars {
				if sc != "" && !seenFiles[sc] {
					seenFiles[sc] = true
					allInstalledFiles = append(allInstalledFiles, sc)
				}
			}
			for _, sym := range r.InstalledSymlinks {
				if sym != "" && !seenFiles[sym] {
					seenFiles[sym] = true
					allInstalledFiles = append(allInstalledFiles, sym)
				}
			}
			for _, bin := range r.InstalledBinaries {
				fullBin := filepath.Join(r.CliParams.TargetPath, bin)
				if !seenFiles[fullBin] {
					seenFiles[fullBin] = true
					allInstalledFiles = append(allInstalledFiles, fullBin)
				}
			}

			var installedPkgInfo *state.InstallPkg
			if r.InstalledPkgManager != "" || len(r.InstalledPackageIDs) > 0 {
				installedPkgInfo = &state.InstallPkg{
					Manager:   r.InstalledPkgManager,
					PackageID: strings.Join(r.InstalledPackageIDs, ", "),
				}
			}

			installedStep := &state.InstallStep{
				Pkg:   installedPkgInfo,
				Files: allInstalledFiles,
			}

			installMapEntry := &state.InstallMapEntry{
				Deps:      depsStep,
				Installed: installedStep,
			}
			_ = st.SetInstallMap(r.CliParams.Repository, installMapEntry)
		} else {
			log.Warn("could not save installed app state", "error", err)
		}
	}

	// Update final install state with actually installed assets, sidecars, and archive details
	finalState := installState
	if len(r.InstalledBinaries) > 0 && len(r.InstalledPackageNames) > 0 {
		finalState.ExtractedAssets = append([]string{}, r.InstalledPackageNames...)
		finalState.ExtractedAssets = append(finalState.ExtractedAssets, r.InstalledBinaries...)
		finalState.Type = "deb+binary"
	} else if len(r.InstalledBinaries) > 0 {
		finalState.ExtractedAssets = r.InstalledBinaries
	} else if len(r.InstalledPackageNames) > 0 {
		finalState.ExtractedAssets = r.InstalledPackageNames
	} else if len(r.InstalledAssetCleanNames) > 0 {
		finalState.ExtractedAssets = r.InstalledAssetCleanNames
	}

	if len(r.InstalledAssetFullNames) > 1 {
		finalState.AssetName = strings.Join(r.InstalledAssetFullNames, ", ")
	}

	if r.ContainingArchive != "" {
		finalState.ArchiveName = r.ContainingArchive
		finalState.ArchiveType = cleanAssetType(r.ContainingArchive)
		if len(r.InstalledPackageNames) > 0 {
			finalState.Type = cleanAssetType(r.InstalledPackageNames[0])
		} else {
			finalState.Type = "binary"
		}
	}

	if len(r.InstalledSidecars) > 0 {
		var sidecarNames []string
		for _, sc := range r.InstalledSidecars {
			sidecarNames = append(sidecarNames, filepath.Base(sc))
		}
		finalState.Sidecars = sidecarNames
	}

	if finalState.PrevVersion == "" && len(r.InstalledBinaries) > 0 && r.CliParams != nil && r.CliParams.TargetPath != "" {
		dest := filepath.Join(r.CliParams.TargetPath, r.InstalledBinaries[0])
		if _, err := os.Stat(dest); err == nil {
			finalState.PrevVersion = probeBinaryVersion(dest)
		}
	}

	if finalMsg, err := status.GenerateStatusMessage(finalState); err == nil {
		r.StatusMessage = finalMsg
	}

	// Wait for pacman animation to complete before showing final message
	if pUI != nil {
		pUI.WaitForAnimation()
	}

	if r.StatusMessage != "" {
		msg := r.StatusMessage
		if r.CliParams != nil && (r.CliParams.NoEmojis || r.CliParams.DisableIcons) {
			msg = ui.StripEmojis(msg)
		}
		if (r.CliParams != nil && r.CliParams.NoColor) || os.Getenv("NO_COLOR") != "" {
			fmt.Printf("\n%s\n", msg)
		} else {
			fmt.Printf("\n\033[1;32m%s\033[0m\n", msg)
		}
	} else {
		repoName := r.CliParams.Repository
		if parts := strings.Split(repoName, "/"); len(parts) == 2 {
			repoName = parts[1]
		}
		if (r.CliParams != nil && r.CliParams.NoColor) || os.Getenv("NO_COLOR") != "" {
			fmt.Printf("\nInstalled %s successfully!\n", repoName)
		} else {
			fmt.Printf("\n\033[1;32mInstalled %s successfully!\033[0m\n", repoName)
		}
	}

	if r.CliParams != nil && r.CliParams.Verbose {
		fmt.Printf("\n--- 📦 INSTALLED ASSETS & FILES ---\n")
		if len(r.InstalledAssetFullNames) > 0 {
			fmt.Printf("Assets used:\n")
			for _, a := range r.InstalledAssetFullNames {
				fmt.Printf("  • %s\n", a)
			}
		}
		if len(r.InstalledBinaries) > 0 {
			fmt.Printf("Installed binaries:\n")
			for _, b := range r.InstalledBinaries {
				target := filepath.Join(r.CliParams.TargetPath, b)
				fmt.Printf("  • %s (%s)\n", b, target)
			}
		}
		if len(r.InstalledPackageNames) > 0 {
			fmt.Printf("Installed system packages:\n")
			for _, p := range r.InstalledPackageNames {
				fmt.Printf("  • %s\n", p)
			}
		}
	}

	if r.CliParams == nil || (!r.CliParams.Update && !r.CliParams.UpdateAll) {
		repo := ""
		if r.CliParams != nil {
			repo = r.CliParams.Repository
		}
		state.LogHistory("install", repo, releases[0].Name)
	}

	return nil
}

func (r *GithubRelease) installPkg(binaryPath string) error {
	basePath := "./" + filepath.Base(binaryPath)
	if r.CliParams.DryRun {
		log.Infof("[dry-run] Would install freebsd pkg: %s", filepath.Base(binaryPath))
		return nil
	}
	if err := r.ensureSudo(); err != nil {
		return err
	}
	var args []string
	if r.CliParams.NoDeps {
		args = []string{"pkg", "add", basePath}
	} else if r.CliParams.ResolveDeps || r.CliParams.DisablePrompts {
		args = []string{"pkg", "install", "-y", basePath}
	} else {
		args = []string{"pkg", "install", basePath}
	}

	if r.UI != nil {
		r.UI.Pause()
		defer r.UI.Resume()
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
		log.Error(fmt.Sprintf("'sudo %s' failed", strings.Join(args, " ")), "installer binary", binaryPath, "error", err)
		if r.CliParams.Interactive {
			pterm.Error.Println("Failed to install FreeBSD package")
		}
		return err
	}
	log.Info(fmt.Sprintf("ran 'sudo %s'", strings.Join(args, " ")), "installer binary", binaryPath)
	if r.CliParams.Interactive {
		pterm.Success.Println("Successfully installed FreeBSD package!")
	}
	return nil
}

func (r *GithubRelease) installPacman(binaryPath string) error {
	if r.CliParams.DryRun {
		log.Infof("[dry-run] Would install pacman pkg: %s", filepath.Base(binaryPath))
		return nil
	}
	if err := r.ensureSudo(); err != nil {
		return err
	}
	if name := extractPackageName(binaryPath, "pacman"); name != "" {
		r.InstalledPackageNames = append(r.InstalledPackageNames, name)
		r.InstalledPkgManager = "pacman"
		r.InstalledPackageIDs = append(r.InstalledPackageIDs, name)
	}
	log.Debug("installing pacman package", "binaryPath", binaryPath)

	var cmd *exec.Cmd
	basePath := "./" + filepath.Base(binaryPath)
	if r.CliParams.ResolveDeps || r.CliParams.DisablePrompts {
		cmd = execCommand("sudo", "pacman", "-U", "--noconfirm", basePath)
	} else if r.CliParams.NoDeps {
		cmd = execCommand("sudo", "pacman", "-U", "--nodeps", "--noconfirm", basePath)
	} else {
		cmd = execCommand("sudo", "pacman", "-U", basePath)
	}
	cmd.Dir = filepath.Dir(binaryPath)

	if r.UI != nil {
		r.UI.Pause()
		defer r.UI.Resume()
	}

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

func (r *GithubRelease) fallbackToReadmeInstructions() {
	var readmeData struct {
		Content string `json:"content"`
	}
	err := r.Client.Get(fmt.Sprintf("repos/%s/readme", r.CliParams.Repository), &readmeData)
	if err != nil {
		return
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(readmeData.Content, "\n", ""))
	if err != nil {
		return
	}

	content := string(decoded)
	lines := strings.Split(content, "\n")

	headerRe := regexp.MustCompile(`(?i)^(\#{1,6})\s*(?:quick\s*start|install(?:ation)?|setup)`)
	anyHeaderRe := regexp.MustCompile(`^(\#{1,6})\s`)
	cmdRe := regexp.MustCompile(`^\s*(pipx|uv tool|pnpm|npm|snap|cargo|go install|curl)\b`)

	inInstallSection := false
	headerLevel := 0
	var capturedLines []string

	for _, line := range lines {
		if !inInstallSection {
			matches := headerRe.FindStringSubmatch(line)
			if len(matches) > 0 {
				inInstallSection = true
				headerLevel = len(matches[1])
			}
			continue
		}

		matches := anyHeaderRe.FindStringSubmatch(line)
		if len(matches) > 0 {
			level := len(matches[1])
			if level <= headerLevel {
				break
			}
		}
		capturedLines = append(capturedLines, line)
	}

	if len(capturedLines) == 0 {
		return
	}

	inCodeBlock := false
	for _, line := range capturedLines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			if cmdRe.MatchString(line) {
				if r.UI != nil {
					r.UI.Stop()
				}
				pterm.Info.Println("No compatible release assets found. Found alternative installation method in README:")
				fmt.Println(strings.TrimSpace(line))
				os.Exit(0)
			}
		}
	}
}
