package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/adrg/xdg"
	"github.com/alecthomas/kong"
	"github.com/charmbracelet/log"
	"github.com/cli/go-gh/v2"
	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/joshsukhdeo/gh-pt/ai"
	"github.com/joshsukhdeo/gh-pt/compile"
	"github.com/joshsukhdeo/gh-pt/config"
	"github.com/joshsukhdeo/gh-pt/heuristics"
	"github.com/joshsukhdeo/gh-pt/params"
	"github.com/joshsukhdeo/gh-pt/release"
	"github.com/joshsukhdeo/gh-pt/resolver"
	"github.com/joshsukhdeo/gh-pt/safety"
	"github.com/joshsukhdeo/gh-pt/selector"
	"github.com/joshsukhdeo/gh-pt/state"
	"github.com/joshsukhdeo/gh-pt/ui"
	"github.com/pterm/pterm"
	"golang.org/x/term"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

type RootCLI struct {
	params.ExecContext
	CliParams         *params.ExecContext `kong:"-"`
	Prompter          release.Prompter    `kong:"-"`
	IsTTYFunc         func() bool         `kong:"-"`
	InstalledSidecars []string            `kong:"-"`
	Hook              params.Hook         `kong:"-"`
}

func (r *RootCLI) ensureCliParams() {
	if r.CliParams == nil {
		r.CliParams = &r.ExecContext
	}
}

const (
	GH_PT_PREFIX_ENV                = "GH_PT_ENV_PREFIX"
	GH_PT_DEFAULT_PREFIX            = "GH_PT"
	GH_PT_CHECKSUM_ASSET_REGEX      = ".*(?:checksum|txt)+.*$"
	GH_INSTALL_PREFIX_ENV           = "GH_INSTALL_ENV_PREFIX"
	GH_INSTALL_DEFAULT_PREFIX       = "GH_INSTALL"
	GH_INSTALL_CHECKSUM_ASSET_REGEX = ".*(?:checksum|txt)+.*$"
	GH_PT_DEFAULT_GITHUB_TIMEOUT    = 30 * time.Second
	GH_PT_DEFAULT_CONNECT_TIMEOUT   = 10 * time.Second
)

// newGitHubRESTClient creates a GitHub REST client with proper timeouts and error handling.
func newGitHubRESTClient() (*api.RESTClient, error) {
	// Check for internet connectivity first with a quick dial
	if !isInternetReachable() {
		return nil, fmt.Errorf("no internet connection: cannot reach GitHub API. Please check your network connection")
	}

	httpClient := &http.Client{
		Timeout: GH_PT_DEFAULT_GITHUB_TIMEOUT,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   GH_PT_DEFAULT_CONNECT_TIMEOUT,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			MaxIdleConns:          10,
			IdleConnTimeout:       90 * time.Second,
		},
	}

	ghClient, err := api.NewRESTClient(api.ClientOptions{
		Timeout:   GH_PT_DEFAULT_GITHUB_TIMEOUT,
		Transport: httpClient.Transport,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub REST client: %w", err)
	}

	return ghClient, nil
}

// isInternetReachable performs a quick connectivity check to GitHub.
func isInternetReachable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dialer := &net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", "api.github.com:443")
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// validateTargetPath checks if the target installation path is writable.
func validateTargetPath(targetPath string) error {
	if targetPath == "" {
		return fmt.Errorf("target path is empty")
	}

	// Try to create the directory if it doesn't exist
	info, err := os.Stat(targetPath)
	if os.IsNotExist(err) {
		// Try to create it
		if err := os.MkdirAll(targetPath, 0755); err != nil {
			return fmt.Errorf("cannot create target directory %q: %w (check permissions)", targetPath, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot access target directory %q: %w", targetPath, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("target path %q exists but is not a directory", targetPath)
	}

	// Test write permission by creating a temp file
	testFile := filepath.Join(targetPath, ".gh-pt-write-test")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		return fmt.Errorf("target directory %q is not writable: %w (run with appropriate permissions or use --target with a writable path)", targetPath, err)
	}
	_ = os.Remove(testFile)
	return nil
}

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

// needsGitHubAPI returns true if the command requires GitHub API access.
func needsGitHubAPI(r *RootCLI) bool {
	r.ensureCliParams()
	// Commands that don't need GitHub API
	if r.Ls != "" || r.Ll != "" {
		return false
	}
	if r.EditSavedState {
		return false
	}
	if r.RmSavedState != "" {
		return false
	}
	if r.Rm != "" {
		return false
	}
	if r.Purge != "" {
		return false
	}
	if r.Pin != "" {
		return false
	}
	// Clone/Fork/Show/CompileFromSource don't need GitHub API for release fetching
	if r.Clone || r.Fork || r.CompileFromSource || r.Show || r.ShowAssets > -1 || r.ShowVersions > -1 || r.ShowDescription > -1 || r.ShowReadme > -1 {
		return false
	}
	// Update/Upgrade/Install need GitHub API
	return true
}

// validateGHCLI checks if gh CLI is installed and authenticated.
// Returns a user-friendly error if not.
func validateGHCLI() error {
	// Check if gh is installed
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		return fmt.Errorf("GitHub CLI (gh) not found in PATH\n  → Install from https://cli.github.io/\n  → Or ensure 'gh' is in your PATH")
	}

	// Check gh version (need 2.0+ for API support)
	verOut, err := exec.Command(ghPath, "--version").Output()
	if err == nil {
		verStr := strings.TrimSpace(string(verOut))
		if strings.Contains(verStr, "gh version") {
			// Extract version number
			parts := strings.Fields(verStr)
			for _, p := range parts {
				if strings.HasPrefix(p, "v") || strings.Contains(p, ".") {
					// Basic version check - gh 2.0+ required
					if strings.HasPrefix(p, "v1.") {
						return fmt.Errorf("GitHub CLI version too old (%s)\n  → gh-pt requires gh 2.0+\n  → Update: https://cli.github.io/", verStr)
					}
					break
				}
			}
		}
	}

	// Check authentication status
	authOut, err := exec.Command(ghPath, "auth", "status").CombinedOutput()
	if err != nil {
		authErr := strings.TrimSpace(string(authOut))
		if strings.Contains(authErr, "not logged in") || strings.Contains(authErr, "no auth") {
			return fmt.Errorf("GitHub CLI not authenticated\n  → Run: gh auth login\n  → Or set GH_TOKEN environment variable with 'repo' scope")
		}
		return fmt.Errorf("failed to check gh auth status: %v\n  → Run 'gh auth status' to diagnose", err)
	}

	authStr := strings.TrimSpace(string(authOut))
	if strings.Contains(authStr, "not logged in") || strings.Contains(authStr, "no auth") {
		return fmt.Errorf("GitHub CLI not authenticated\n  → Run: gh auth login\n  → Or set GH_TOKEN environment variable with 'repo' scope")
	}

	// Check token has repo scope
	if !strings.Contains(authStr, "repo") && !strings.Contains(authStr, "admin:repo_hook") {
		return fmt.Errorf("GitHub CLI token missing 'repo' scope\n  → Run: gh auth refresh -h github.com -s repo\n  → Or re-authenticate with: gh auth login --scopes repo")
	}

	return nil
}

// isContainerEnvironment detects whether gh-pt is running inside a Docker, Podman, or container runtime.
func isContainerEnvironment() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		return true
	}
	if os.Getenv("container") != "" {
		return true
	}
	// Check cgroups for container markers
	for _, cgroupPath := range []string{"/proc/1/cgroup", "/proc/self/cgroup"} {
		if data, err := os.ReadFile(cgroupPath); err == nil {
			s := string(data)
			if strings.Contains(s, "docker") || strings.Contains(s, "podman") ||
				strings.Contains(s, "containerd") || strings.Contains(s, "kubepods") ||
				strings.Contains(s, "lxc") {
				return true
			}
		}
	}
	return false
}

// validateInputs validates and sanitizes user inputs to prevent injection attacks
// and ensure well-formed repository names, paths, and hook commands.
func validateInputs(r *RootCLI) error {
	r.ensureCliParams()
	c := r.CliParams

	// Validate repository name format (owner/repo)
	if c.Repository != "" {
		// Only allow alphanumeric, dash, underscore, dot in owner and repo names
		// No shell metacharacters, no path traversal
		repoRegex := regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*/[a-zA-Z0-9][a-zA-Z0-9._-]*$`)
		if !repoRegex.MatchString(c.Repository) {
			return fmt.Errorf("invalid repository format: %q (must be owner/repo with alphanumeric, dash, underscore, dot only)", c.Repository)
		}
		// Prevent overly long names
		if len(c.Repository) > 200 {
			return fmt.Errorf("repository name too long (max 200 chars)")
		}
	}

	// Validate target path - no path traversal, no shell metacharacters
	if c.TargetPath != "" {
		cleanPath := filepath.Clean(c.TargetPath)
		if cleanPath != c.TargetPath {
			return fmt.Errorf("target path contains path traversal sequences: %q", c.TargetPath)
		}
		// Check for shell metacharacters
		if strings.ContainsAny(c.TargetPath, "`$&|;(){}[]<>") {
			return fmt.Errorf("target path contains shell metacharacters: %q", c.TargetPath)
		}
	}

	// Validate release asset/regexp - no shell metacharacters
	if c.ReleaseAsset != "" && strings.ContainsAny(c.ReleaseAsset, "`$&|;(){}[]<>") {
		return fmt.Errorf("release asset pattern contains shell metacharacters: %q", c.ReleaseAsset)
	}
	if c.ReleaseAssetRegexp != "" && strings.ContainsAny(c.ReleaseAssetRegexp, "`$&|;(){}[]<>") {
		return fmt.Errorf("release asset regexp contains shell metacharacters: %q", c.ReleaseAssetRegexp)
	}
	if c.ReleaseVersion != "" && strings.ContainsAny(c.ReleaseVersion, "`$&|;(){}[]<>") {
		return fmt.Errorf("release version contains shell metacharacters: %q", c.ReleaseVersion)
	}

	// Validate sidecar patterns - no shell metacharacters
	if c.Sidecars != "" && strings.ContainsAny(c.Sidecars, "`$&|;(){}[]<>") {
		return fmt.Errorf("sidecar pattern contains shell metacharacters: %q", c.Sidecars)
	}

	// Validate hook commands - hooks are passed via Hook field
	if r.Hook.ScriptPath != "" {
		cleanPath := filepath.Clean(r.Hook.ScriptPath)
		if cleanPath != r.Hook.ScriptPath {
			return fmt.Errorf("hook script path contains path traversal: %q", r.Hook.ScriptPath)
		}
		if strings.ContainsAny(r.Hook.ScriptPath, "`$&|;(){}[]<>\\") {
			return fmt.Errorf("hook script path contains shell metacharacters: %q", r.Hook.ScriptPath)
		}
	}

	// Validate sidecar symlink targets
	for _, target := range c.SidecarSymlinkTo {
		cleanPath := filepath.Clean(target)
		if cleanPath != target {
			return fmt.Errorf("sidecar symlink target contains path traversal: %q", target)
		}
		if strings.ContainsAny(target, "`$&|;(){}[]<>") {
			return fmt.Errorf("sidecar symlink target contains shell metacharacters: %q", target)
		}
	}

	// Validate rename map keys and values
	for k, v := range c.Rename {
		if strings.ContainsAny(k, "`$&|;(){}[]<>\\") || strings.ContainsAny(v, "`$&|;(){}[]<>\\") {
			return fmt.Errorf("rename map contains shell metacharacters: %q -> %q", k, v)
		}
	}

	// Validate asset binaries regexp
	if c.AssetBinariesRegexp != "" && strings.ContainsAny(c.AssetBinariesRegexp, "`$&|;(){}[]<>") {
		return fmt.Errorf("asset binaries regexp contains shell metacharacters: %q", c.AssetBinariesRegexp)
	}

	// Validate FallbackReleases - cap at 3
	if c.FallbackReleases > 3 {
		return fmt.Errorf("fallback-releases cannot exceed 3 (got %d)", c.FallbackReleases)
	}
	if c.FallbackReleases < 0 {
		return fmt.Errorf("fallback-releases cannot be negative (got %d)", c.FallbackReleases)
	}

	return nil
}

func (r *RootCLI) Validate() error {
	r.ensureCliParams()
	if r.Wine != "off" && r.Wine != "" {
		if !selector.IsWineSupportedOS(runtime.GOOS) {
			pterm.Warning.Printf("Wine is not supported on %s. Continuing with wine disabled.\n", runtime.GOOS)
			r.Wine = "off"
		} else if !selector.IsWineInstalled() {
			pterm.Warning.Println("Wine is not installed. Continuing with wine disabled.")
			r.Wine = "off"
		}
	}

	if !r.Update && !r.UpdateAll && r.Ls == "" && r.Ll == "" && !r.EditSavedState && r.RmSavedState == "" && r.Rm == "" && r.Purge == "" && r.Pin == "" {
		match, _ := regexp.MatchString(`.+/.+`, r.Repository)
		if !match {
			return fmt.Errorf("repository must be in 'user/repository' format (provided: '%s')", r.Repository)
		}
	}

	if r.CompileFromSource && !r.AI {
		return fmt.Errorf("--compile-from-source can only be used with --ai")
	}

	// Validate and sanitize inputs
	if err := validateInputs(r); err != nil {
		return err
	}

	if r.Clone || r.Fork || r.CompileFromSource || r.Show || r.ShowAssets > -1 || r.ShowVersions > -1 || r.ShowDescription > -1 || r.ShowReadme > -1 {
		return nil
	}

	// Detect root user and handle global install path
	// Root install policy:
	// - Container detection: in containers (Docker, CI), running as root is standard; suppress warning
	// - Host root: warning banner shown unless --force-root is provided
	// - --global or --force-root or --allow-root-user-install permits operation
	// - Audit log root operations for security
	if os.Geteuid() == 0 {
		inContainer := isContainerEnvironment()
		if r.Global {
			r.ForceRoot = true
		}

		if !inContainer && !r.ForceRoot {
			if r.NoColor || os.Getenv("NO_COLOR") != "" {
				fmt.Fprintf(os.Stderr, "*** running as root is *HIGHLY* discouraged ***\n")
			} else {
				fmt.Fprintf(os.Stderr, "\033[33m*** running as root is *HIGHLY* discouraged ***\033[0m\n")
			}
		} else if inContainer {
			log.Info("running as root inside container environment (warning suppressed)")
		}

		log.Info("audit: privileged root operation",
			"container", inContainer,
			"global", r.Global,
			"force_root", r.ForceRoot,
			"allow_root_user_install", r.AllowRootUserInstall,
			"pid", os.Getpid(),
			"uid", os.Getuid(),
			"euid", os.Geteuid(),
		)

		if r.Global {
			if r.TargetPath == GetDefaultTargetPath() {
				r.TargetPath = "/usr/local/bin"
			}
			if os.Geteuid() != 0 {
				if err := exec.Command("sudo", "-v").Run(); err != nil {
					log.Warn("sudo -v failed (credentials may not cache)", "error", err)
				}
			}
		} else if !r.AllowRootUserInstall && !r.ForceRoot {
			err := fmt.Errorf("running as root without --global flag. Use --global for system-wide install or --force-root / --allow-root-user-install to install to user-local paths")
			log.Error("init error", "error", err)
			return err
		}
	}

	if r.TargetPath == "" {
		err := fmt.Errorf("could not determine default install path, use '--install-path' flag")
		log.Error("init error", "error", err)
		return err
	}

	targetPathInfo, err := os.Stat(r.TargetPath)
	if err != nil {
		if !os.IsNotExist(err) {
			createPath := r.TargetPathCreate
			if r.Interactive {
				createPath, _ = pterm.DefaultInteractiveConfirm.
					WithDefaultValue(true).
					Show(fmt.Sprintf("'%s' does not exist. Create?", r.TargetPath))
			}

			if createPath {
				err := os.MkdirAll(r.TargetPath, os.ModePerm)
				if err != nil {
					log.Error(fmt.Sprintf("target installation path '%s' error", r.TargetPath), "error", err)
					return err
				}
				// Mark directory as managed by gh-pt for safe removal later
				_ = safety.WriteGhptManagedMarker(r.TargetPath)
				return nil
			} else {
				log.Error(fmt.Sprintf("target installation path '%s' error", r.TargetPath), "error", err)
				return err
			}

		}
		log.Error(fmt.Sprintf("target installation path '%s' error", r.TargetPath), "error", err)
		return err
	}

	if !targetPathInfo.Mode().IsDir() {
		err = errors.New("not a directory")
		log.Error(fmt.Sprintf("target installation path '%s' error", r.TargetPath), "error", err)
	}

	return nil
}

func PostBuild(k *kong.Kong) error {
	k.Model.Positional[0].Tag.Envs = []string{fmt.Sprintf("%s_REPOSITORY", GetEnvPrefix())}
	return nil
}

func (r *RootCLI) RunInstall() error {
	r.ensureCliParams()
	safety.SetDryRun(r.DryRun)
	defer safety.SetDryRun(false)
	if r.NoEmojis {
		r.DisableIcons = true
	}
	if r.NoColor || os.Getenv("NO_COLOR") != "" {
		r.NoColor = true
		ApplyDisplayPreferences(true, false)
	}
	if r.Barbarous {
		r.InsecureAllowUnsigned = true
		r.VerifyChecksum = false
		r.SkipVtSandbox = true
		r.AllowForeignArch = true
		r.AllowDowngrade = true
		if r.Wine == "" || r.Wine == "off" {
			r.Wine = "allow"
		}
	}
	if r.LeRetrogrouch {
		r.AllowDowngrade = true
		r.PinInstall = false
	}
	if r.RetrogradeStopgap {
		r.AllowDowngrade = true
		r.PinInstall = true
	}
	if r.SelfInflictedDebt {
		r.AllowDowngrade = true
	}

	if !r.Verbose {
		log.SetLevel(log.WarnLevel)
	} else {
		log.SetLevel(log.DebugLevel)
	}
	if r.LogQuietInteractive && r.Interactive && !r.Verbose {
		log.SetLevel(log.FatalLevel)
	}

	if r.Verbose {
		r.ProgressBar = "none"
		if r.CliParams != nil {
			r.CliParams.ProgressBar = "none"
			r.CliParams.Verbose = true
		}
		ui.GlobalPacman = nil
	}

	cfg := loadConfig()

	stdoutWrapper := ui.PacmanLogWriter{Writer: os.Stdout}

	var terminalWriter io.Writer = stdoutWrapper
	if r.Verbose {
		terminalWriter = os.Stdout
	}

	baseWriter := terminalWriter
	if cfg != nil && cfg.Core.LogToFile {
		fileLogger := &lumberjack.Logger{
			Filename:   filepath.Join(xdg.DataHome, "gh-pt", "gh-pt.log"),
			MaxSize:    10,
			MaxBackups: 5,
			MaxAge:     30,
			Compress:   true,
		}
		baseWriter = io.MultiWriter(terminalWriter, fileLogger)
	}

	if r.Verbose {
		log.SetOutput(&ui.RegexTruncatingWriter{Writer: baseWriter, MaxLen: 200})
	} else {
		log.SetOutput(baseWriter)
	}

	if cfg != nil {
		if r.VTApiKey == "" {
			r.VTApiKey = cfg.Core.VTApiKey
		}
		if cfg.Core.AllowPrerelease {
			r.Prerelease = true
		}
		if cfg.Core.DisableIcons {
			r.DisableIcons = true
		}
		if cfg.Core.NoColor {
			r.NoColor = true
		}
		if cfg.Core.NoEmojis {
			r.NoEmojis = true
		}
		if cfg.Core.AvxLevel != "" && (r.AvxLevel == "" || r.AvxLevel == "auto") {
			r.AvxLevel = cfg.Core.AvxLevel
			if r.CliParams != nil {
				r.CliParams.AvxLevel = cfg.Core.AvxLevel
			}
		}
	}
	if r.NoEmojis {
		r.DisableIcons = true
	}
	if r.NoColor {
		ApplyDisplayPreferences(true, false)
	}
	if r.Stable {
		r.Prerelease = false
	}

	if r.ResolveDeps && r.NoDeps {
		r.ResolveDeps = false
		r.NoDeps = false
	} else if !r.ResolveDeps && !r.NoDeps {
		envDeps := strings.ToUpper(os.Getenv("GH_PT_ADD_DEPS"))
		if envDeps == "" {
			envDeps = strings.ToUpper(os.Getenv("GH_INSTALL_ADD_DEPS"))
		}
		switch envDeps {
		case "TRUE":
			r.ResolveDeps = true
		case "FALSE":
			r.NoDeps = true
		default:
			if cfg != nil {
				r.ResolveDeps = cfg.Core.ResolveDeps
				r.NoDeps = cfg.Core.NoDeps
				if !r.DisablePrompts {
					r.DisablePrompts = cfg.Core.DisablePrompts
				}
				if !r.NoSaveState {
					r.NoSaveState = cfg.Core.NoSaveState
				}
				if r.Extractor == "" || r.Extractor == "default" {
					r.Extractor = cfg.Core.Extractor
				}
				if !r.KeepSuffixes {
					r.KeepSuffixes = cfg.Core.KeepSuffixes
				}
				if !r.Symlink {
					r.Symlink = cfg.Core.Symlink
				}

			}
		}
	}

	if r.Global && r.TargetPath == GetDefaultTargetPath() {
		switch runtime.GOOS {
		case "windows":
			r.TargetPath = os.Getenv("ProgramFiles")
			if r.TargetPath == "" {
				r.TargetPath = "C:\\Program Files"
			}
		default:
			r.TargetPath = "/usr/local/bin"
		}
	}

	// Validate target path early (before any network calls)
	if r.TargetPath != "" {
		if err := validateTargetPath(r.TargetPath); err != nil {
			return err
		}
	}

	// Validate gh CLI is installed and authenticated (for commands needing GitHub API)
	if needsGitHubAPI(r) {
		if err := validateGHCLI(); err != nil {
			return err
		}
	}

	// Create GitHub client with timeout and proper error handling
	ghClient, err := newGitHubRESTClient()
	if err != nil {
		return formatGitHubError(err, r.Repository)
	}

	if r.Ls != "" || r.Ll != "" {
		return ListState(r)
	}
	if r.EditSavedState {
		return EditState()
	}
	if r.RmSavedState != "" {
		if !r.DisablePrompts && !r.Overwrite {
			confirmed, err := pterm.DefaultInteractiveConfirm.WithDefaultValue(false).Show(fmt.Sprintf("Remove %q from saved state only? This does not uninstall the app.", r.RmSavedState))
			if err != nil {
				return err
			}
			if !confirmed {
				return nil
			}
		}
		return RmStateOnly(r.RmSavedState)
	}
	if r.Rm != "" {
		if !r.DisablePrompts && !r.Overwrite {
			confirmed, err := pterm.DefaultInteractiveConfirm.WithDefaultValue(false).Show(fmt.Sprintf("Uninstall %q and remove it from saved state? This removes the tracked binary(s) and any package managed by the OS package manager.", r.Rm))
			if err != nil {
				return err
			}
			if !confirmed {
				return nil
			}
		}
		return RemoveApp(r.Rm, false, false)
	}
	if r.Purge != "" {
		if !r.DisablePrompts && !r.Overwrite {
			confirmed, err := pterm.DefaultInteractiveConfirm.WithDefaultValue(false).Show(fmt.Sprintf("Purge %q and remove it from saved state? This removes the tracked binary(s) and purges the package if applicable.", r.Purge))
			if err != nil {
				return err
			}
			if !confirmed {
				return nil
			}
		}
		return RemoveApp(r.Purge, true, false)
	}
	if r.Pin != "" {
		return PinAppState(r.Pin)
	}

	if r.Update || r.UpdateAll {
		return DoUpdate(r, ghClient)
	}

	if r.Overwrite && !r.DryRun {
		// If overwrite/force is used, attempt to purge any existing installation first
		_ = RemoveApp(r.Repository, true, false)
	}

	if r.Repository == "" {
		return fmt.Errorf("repository argument is required for installation")
	}

	if !strings.Contains(r.Repository, "/") && !strings.HasPrefix(r.Repository, "http") {
		return fmt.Errorf("unknown command or invalid repository format: '%s' (expected owner/repo)", r.Repository)
	}

	if r.AI && r.AISafetyScan {
		if err := r.handleAISafetyScan(cfg); err != nil {
			return err
		}
	}

	if r.Clone || r.Fork {
		if cfg == nil {
			cfg, _ = config.LoadConfig()
		}
		return r.handleRepoCloneOrFork(cfg)
	}

	if r.CompileFromSource {
		if cfg == nil {
			cfg, _ = config.LoadConfig()
		}
		return r.handleCompileFromSource(cfg)
	}

	if r.AssetBinariesRegexp == "" {
		r.AssetBinariesRegexp = "(?i).*"
	}

	if r.ReleaseAssetRegexp == "" {
		r.ReleaseAssetRegexps = buildRegexFromTypes(r.Type, r.Wine)
		r.ReleaseAssetRegexp = strings.Join(r.ReleaseAssetRegexps, " | ")
	} else {
		r.ReleaseAssetRegexps = []string{r.ReleaseAssetRegexp}
	}

	log.Debug("installing with values",
		"repository", r.Repository,
		"release version", r.ReleaseVersion,
		"release asset name", r.ReleaseAsset,
		"release asset regexp", params.TruncateRegex(r.ReleaseAssetRegexp, 200),
		"release asset binary names", r.AssetBinaries,
		"release asset binary name regexp", params.TruncateRegex(r.AssetBinariesRegexp, 200),
		"target path", r.TargetPath,
		"renaming binaries", r.Rename,
	)

	response := struct{ Name string }{}
	err = ghClient.Get(fmt.Sprintf("repos/%s", r.Repository), &response)
	if err != nil {
		log.Error(fmt.Sprintf("repository %s doesn't exist", r.Repository), "error", err)
	}

	var existingHooks map[string]string
	if st, err := state.LoadState(); err == nil && st.Apps != nil {
		if existing, ok := st.Apps[r.Repository]; ok && existing != nil && len(existing.Hooks) > 0 {
			existingHooks = make(map[string]string)
			for k, v := range existing.Hooks {
				existingHooks[k] = v
			}
		}
	}

	installRelease := release.MakeGithubRelease(
		&r.ExecContext,
		ghClient)
	err = installRelease.Install()
	if err != nil {
		return err
	}

	if len(existingHooks) > 0 {
		if st, err := state.LoadState(); err == nil && st.Apps != nil {
			if app, ok := st.Apps[r.Repository]; ok && app != nil {
				if app.Hooks == nil {
					app.Hooks = make(map[string]string)
				}
				for k, v := range existingHooks {
					app.Hooks[k] = v
				}
				_ = st.Save()
			}
		}
	}

	return r.runPostInstallHook(r.Repository)
}

func resolveRepoPath(repo string, isClone, isFork bool, clonePath, forkPath string) string {
	parts := strings.Split(repo, "/")
	repoName := parts[len(parts)-1]

	expandHome := func(p string) string {
		if strings.HasPrefix(p, "~/") || p == "~" {
			homeDir, err := os.UserHomeDir()
			if err == nil {
				if p == "~" {
					return homeDir
				}
				return filepath.Join(homeDir, p[2:])
			}
		}
		return p
	}

	if isFork {
		base := forkPath
		if base == "" {
			base = GetDefaultForkPath()
		} else {
			base = expandHome(base)
		}
		return filepath.Join(base, repoName)
	}

	if isClone {
		base := clonePath
		if base == "" {
			base = GetDefaultClonePath()
		} else {
			base = expandHome(base)
		}
		return filepath.Join(base, repoName)
	}

	return ""
}

func buildCloneOrForkArgs(repo string, isFork bool, targetDir string, maxDepth int) []string {
	var args []string
	if isFork {
		args = []string{"repo", "fork", repo, "--clone", targetDir}
		if maxDepth > 0 {
			args = append(args, "--", "--depth", fmt.Sprintf("%d", maxDepth))
		}
	} else {
		cloneArgs := []string{"repo", "clone", repo, targetDir}
		if maxDepth > 0 {
			cloneArgs = append(cloneArgs, "--", "--depth", fmt.Sprintf("%d", maxDepth))
		}
		args = cloneArgs
	}
	return args
}

func (r *RootCLI) handleRepoCloneOrFork(cfg *config.Config) error {
	cloneBase := GetDefaultClonePath()
	forkBase := GetDefaultForkPath()
	if cfg != nil {
		cloneBase = cfg.Paths.ClonePath
		forkBase = cfg.Paths.ForkPath
	}

	targetDir := resolveRepoPath(r.Repository, r.Clone, r.Fork, cloneBase, forkBase)
	if r.TargetPath != "" && r.TargetPath != GetDefaultTargetPath() {
		targetDir = r.TargetPath
	}

	log.Info("handling repository clone/fork",
		"repository", r.Repository,
		"target_directory", targetDir,
		"clone", r.Clone,
		"fork", r.Fork,
	)

	if r.DryRun {
		if r.Fork {
			log.Info(fmt.Sprintf("[dry-run] Would fork and clone %s to %s", r.Repository, targetDir))
		} else {
			log.Info(fmt.Sprintf("[dry-run] Would clone %s to %s", r.Repository, targetDir))
		}
		return nil
	}

	if _, err := os.Stat(targetDir); err == nil {
		if !r.Overwrite {
			return fmt.Errorf("target path %s already exists; use -f or --force to overwrite", targetDir)
		}
		if err := forceRemoveAll(targetDir); err != nil {
			return fmt.Errorf("failed to forcefully remove existing target directory %s: %w", targetDir, err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(targetDir), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	args := buildCloneOrForkArgs(r.Repository, r.Fork, targetDir, r.MaxDepth)

	stdOut, stdErr, err := gh.Exec(args...)
	if err != nil {
		return fmt.Errorf("failed to execute gh %s: %s (%w)", strings.Join(args, " "), stdErr.String(), err)
	}
	log.Info("repository cloned successfully", "output", stdOut.String())

	if !r.NoSaveState {
		st, err := state.LoadState()
		if err == nil {
			err = st.AddApp(&state.InstalledApp{
				Repository: r.Repository,
				TargetPath: targetDir,
				Global:     r.Global,
				Clone:      r.Clone,
				Fork:       r.Fork,
				Pinned:     r.PinInstall,
				MaxDepth:   r.MaxDepth,
			})
			if err != nil {
				log.Warn("could not save repository state", "error", err)
			} else {
				log.Info(fmt.Sprintf("Saved %s to state tracking.", r.Repository))
			}
		}
	}

	return nil
}

func getCompileScriptPath(repo string) string {
	parts := strings.Split(repo, "/")
	pkgName := parts[len(parts)-1]

	ext := ".sh"
	if runtime.GOOS == "windows" {
		ext = ".ps1"
	}

	configDir := filepath.Dir(config.GetConfigPath())
	return filepath.Join(configDir, "scripts", fmt.Sprintf("compile-%s%s", pkgName, ext))
}

func getSourcePaths(repo string) (manifestPath, compilePath string) {
	parts := strings.Split(repo, "/")
	var ownerID, repoID string
	if len(parts) >= 2 {
		ownerID = parts[0]
		repoID = parts[1]
	} else if len(parts) == 1 {
		ownerID = "unknown"
		repoID = parts[0]
	}
	ownerClean := strings.ToLower(ownerID)
	repoClean := strings.ToLower(repoID)

	sourceDir := state.GetSourceDir()
	manifestPath = filepath.Join(sourceDir, fmt.Sprintf("manifest-%s-%s.json", ownerClean, repoClean))
	compilePath = filepath.Join(sourceDir, fmt.Sprintf("compile-%s-%s.sh", ownerClean, repoClean))
	return manifestPath, compilePath
}

func snapshotDirFiles(dir string) (map[string]os.FileInfo, error) {
	files := make(map[string]os.FileInfo)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return files, nil
	}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			files[path] = info
		}
		return nil
	})
	return files, err
}

func diffDirFiles(dir string, before map[string]os.FileInfo) ([]string, error) {
	var newFiles []string
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return newFiles, nil
	}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			prev, existed := before[path]
			if !existed || prev.ModTime().Before(info.ModTime()) || prev.Size() != info.Size() {
				newFiles = append(newFiles, path)
			}
		}
		return nil
	})
	return newFiles, err
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	if info, err := os.Stat(src); err == nil {
		_ = os.Chmod(dst, info.Mode())
	}
	return os.Remove(src)
}

func (r *RootCLI) processNewFilesAndSymlinks(newFiles []string) []string {
	var recorded []string
	for _, stagedPath := range newFiles {
		activePath := strings.Replace(stagedPath, ".ghpt/", "", 1)
		if err := os.MkdirAll(filepath.Dir(activePath), 0755); err != nil {
			log.Warn("failed to create directory for symlink", "dir", filepath.Dir(activePath), "error", err)
		}
		_ = os.Remove(activePath)
		if err := os.Symlink(stagedPath, activePath); err != nil {
			log.Warn("failed to create symlink", "staged", stagedPath, "active", activePath, "error", err)
		}
		recorded = append(recorded, stagedPath, activePath)
	}
	return recorded
}

func buildCompilePrompt(repo, buildDir, scriptPath, targetPath, symlinkDir string) string {
	var basePrompt string
	if symlinkDir != "" {
		basePrompt = fmt.Sprintf("Please inspect the repository '%s' (cloned at '%s') and create an automated compilation/build workflow. Follow all build instructions for '%s', compile and install the application/binaries into '%s' (this is the staging directory), then create symlink(s) in '%s' pointing to the executable(s) in '%s'. IMPORTANT: First install/stage everything in '%s', then symlink from there to '%s'. Purge any temporary build artifacts.", repo, buildDir, repo, symlinkDir, targetPath, symlinkDir, symlinkDir, targetPath)
	} else {
		basePrompt = fmt.Sprintf("Please inspect the repository '%s' (cloned at '%s') and create an automated compilation/build workflow. Follow all build instructions for '%s', compile the application/binaries, install or copy them to '%s', and purge any temporary build artifacts.", repo, buildDir, repo, targetPath)
	}

	instruction := `\n\nWORKFLOW — USE ONLY ghpt helper COMMANDS (do NOT output any code or JSON):

1. INSPECT: Run 'ghpt helper --get-system-info' to understand the build environment
2. MANIFEST: Use 'ghpt helper --append-manifest "manager=pkg@version"' to declare build dependencies
   - Check current manifest with 'ghpt helper --get-manifest'
   - Validate with 'ghpt helper --validate-manifest'
   - Remove deps with 'ghpt helper --remove-from-manifest "manager=pkg"'
3. BUILD SCRIPT:
   - Get template: 'ghpt helper --get-body-template'
   - Create body.sh in .ghpt/ using that template
   - INSIDE body.sh, use 'ghpt helper --install "SRC=DEST"' to copy files during build
   - Validate with 'ghpt helper --validate-compile-script' (MUST pass before execution)
4. EXECUTE: Run 'ghpt helper --run-compile-script' (only works if validation passes)

KEY RULES:
- ghpt binary checks PROCESS TREE for 'gh-pt' ancestor — if found, ONLY 'ghpt helper' subcommands permitted
- Dependencies are installed via CONTAINERIZATION before script runs (container image matches user's OS)
- NEVER output JSON or code — use helper commands exclusively
- body.sh must be created in .ghpt/ directory using template from --get-body-template
- INSIDE body.sh, use exclusively 'ghpt helper --install "SRC=DEST"' to copy files (not direct filesystem writes)
- --run-compile-script ONLY executes if --validate-compile-script returns true
- Source builds default to symlink mode; version = commit hash (latest-commit) or git tag (stable/prerelease)`

	return basePrompt + instruction
}

func buildCompileFixPrompt(repo, buildDir, scriptPath, targetPath, symlinkDir, errorOutput string, attempt int) string {
	var basePrompt string
	if symlinkDir != "" {
		basePrompt = fmt.Sprintf("The automated compilation workflow at '%s' for repository '%s' (cloned at '%s') failed to run with the following error output (attempt %d of 2):\n\n%s\n\nPlease fix the workflow so that it successfully compiles and installs the application into '%s' (staging directory), then creates symlink(s) in '%s' pointing to the executable(s) in '%s'. REMEMBER: First stage everything in '%s', then symlink from there to '%s'.", scriptPath, repo, buildDir, attempt, errorOutput, symlinkDir, targetPath, symlinkDir, symlinkDir, targetPath)
	} else {
		basePrompt = fmt.Sprintf("The automated compilation workflow at '%s' for repository '%s' (cloned at '%s') failed to run with the following error output (attempt %d of 2):\n\n%s\n\nPlease fix the workflow so that it successfully compiles and installs the binaries into '%s'.", scriptPath, repo, buildDir, attempt, errorOutput, targetPath)
	}

	instruction := `\n\nWORKFLOW — USE ONLY ghpt helper COMMANDS (do NOT output any code or JSON):

1. REVIEW: Check current manifest with 'ghpt helper --get-manifest'
2. FIX MANIFEST: Use 'ghpt helper --append-manifest' or '--remove-from-manifest' as needed
3. FIX body.sh:
   - Read current body.sh in .ghpt/
   - Get fresh template if needed: 'ghpt helper --get-body-template'
   - INSIDE body.sh, use exclusively 'ghpt helper --install "SRC=DEST"' to copy files
4. VALIDATE: Run 'ghpt helper --validate-compile-script' (MUST pass before execution)
5. EXECUTE: Run 'ghpt helper --run-compile-script' (only works if validation passes)

KEY RULES:
- ghpt binary checks PROCESS TREE for 'gh-pt' ancestor — if found, ONLY 'ghpt helper' subcommands permitted
- Dependencies are installed via CONTAINERIZATION before script runs (container image matches user's OS)
- NEVER output JSON or code — use helper commands exclusively
- body.sh must be created in .ghpt/ directory using template from --get-body-template
- INSIDE body.sh, use exclusively 'ghpt helper --install "SRC=DEST"' to copy files (not direct filesystem writes)
- --run-compile-script ONLY executes if --validate-compile-script returns true
- Source builds default to symlink mode; version = commit hash (latest-commit) or git tag (stable/prerelease)`

	return basePrompt + instruction
}

func runAIAgent(aiCmdTemplate, prompt, dir string) error {
	_, err := runAIAgentWithOutput(aiCmdTemplate, prompt, dir)
	return err
}

func runAIAgentWithOutput(aiCmdTemplate, prompt, dir string) (string, error) {
	var cmd *exec.Cmd

	if strings.Contains(aiCmdTemplate, "%s") {
		// Strip any surrounding quotes from the %s placeholder in the user's template
		aiCmdTemplate = strings.ReplaceAll(aiCmdTemplate, `"%s"`, `%s`)
		aiCmdTemplate = strings.ReplaceAll(aiCmdTemplate, `'%s'`, `%s`)

		var formattedCmd string
		if runtime.GOOS == "windows" {
			formattedCmd = strings.ReplaceAll(aiCmdTemplate, "%s", `$env:GH_PT_PROMPT`)
			cmd = exec.Command("powershell", "-NoProfile", "-Command", formattedCmd)
		} else {
			formattedCmd = strings.ReplaceAll(aiCmdTemplate, "%s", `"$GH_PT_PROMPT"`)
			cmd = exec.Command("sh", "-c", formattedCmd)
		}
		cmd.Env = append(os.Environ(), "GH_PT_PROMPT="+prompt)
	} else {
		cmd = exec.Command(aiCmdTemplate, prompt)
	}

	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	var outBuf bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &outBuf)
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	return outBuf.String(), err
}

// runAIAgentInContainer runs the AI agent inside a container with network access
// to generate body.sh and manifest.json. This is Stage 1 of the two-stage AI workflow.
func (r *RootCLI) runAIAgentInContainer(aiCmdTemplate, prompt, repoDir string) (string, error) {
	// Detect container runtime
	runtimeName, err := compile.DetectContainerRuntime()
	if err != nil {
		return "", fmt.Errorf("no container runtime found for AI container: %w", err)
	}

	// Use a lightweight image with the AI tool pre-installed, or install it
	// For now, use the same base image but with network enabled
	aiImage := compile.DetectContainerImage()

	// Build container command with network access for AI
	containerArgs := []string{
		"run", "--rm",
		// Network enabled for AI to access APIs
		"--network=host",
		// Mount repo directory
		"-v", fmt.Sprintf("%s:/build:rw", repoDir),
		"-w", "/build",
		// Set environment variables
		"-e", "GH_PT_PROMPT=" + prompt,
		"-e", "HOME=/tmp",
	}

	// Add the AI command
	if strings.Contains(aiCmdTemplate, "%s") {
		aiCmdTemplate = strings.ReplaceAll(aiCmdTemplate, `"%s"`, `%s`)
		aiCmdTemplate = strings.ReplaceAll(aiCmdTemplate, `'%s'`, `%s`)

		var formattedCmd string
		if runtime.GOOS == "windows" {
			formattedCmd = strings.ReplaceAll(aiCmdTemplate, "%s", `$env:GH_PT_PROMPT`)
			containerArgs = append(containerArgs, aiImage, "powershell", "-NoProfile", "-Command", formattedCmd)
		} else {
			formattedCmd = strings.ReplaceAll(aiCmdTemplate, "%s", `"$GH_PT_PROMPT"`)
			containerArgs = append(containerArgs, aiImage, "sh", "-c", formattedCmd)
		}
	} else {
		containerArgs = append(containerArgs, aiImage, aiCmdTemplate, prompt)
	}

	log.Info("running AI agent in container (stage 1)", "runtime", runtimeName, "image", aiImage)
	cmd := exec.Command(runtimeName, containerArgs...)
	cmd.Dir = repoDir
	cmd.Stdin = os.Stdin
	var outBuf bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &outBuf)
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	return outBuf.String(), err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func isSuspectedLocalSidecar(relPath string, fileName string) bool {
	lowerPath := strings.ToLower(relPath)
	lowerName := strings.ToLower(fileName)

	// Filter out standard bloat: README*, LICENSE*, .md, doc/, src/
	if strings.HasPrefix(lowerName, "readme") ||
		strings.HasPrefix(lowerName, "license") ||
		strings.HasPrefix(lowerName, "licence") ||
		strings.HasPrefix(lowerName, "copying") {
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

func (r *RootCLI) isInteractive() bool {
	if r == nil {
		return false
	}
	r.ensureCliParams()
	if r.CliParams.DisablePrompts || r.DisablePrompts {
		return false
	}
	if !r.CliParams.Interactive && !r.Interactive {
		return false
	}
	if r.IsTTYFunc != nil {
		return r.IsTTYFunc()
	}
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

func (r *RootCLI) interactiveMultiselect(prompt string, options []string) ([]string, error) {
	if r != nil && (r.DisablePrompts || (r.CliParams != nil && r.CliParams.DisablePrompts)) {
		return nil, nil
	}
	if r != nil && r.Prompter != nil {
		return r.Prompter.Multiselect(prompt, options)
	}
	return pterm.DefaultInteractiveMultiselect.WithOptions(options).Show(prompt)
}

func (r *RootCLI) shouldWarnUnmappedAssets(cfg *config.Config) bool {
	if r != nil {
		if r.CliParams != nil && !r.CliParams.WarnUnmappedAssets {
			return false
		}
	}
	if cfg == nil {
		cfg, _ = config.LoadConfig()
	}
	if cfg != nil && !cfg.Core.WarnUnmappedAssets {
		return false
	}
	return true
}

func sliceContains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

func (r *RootCLI) moveDistWithSidecarDetection(srcDir, dstDir string) ([]string, error) {
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return nil, err
	}

	var primaryBinaries []string
	var suspectedSidecars []string

	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		cleanRel := filepath.ToSlash(rel)
		if isSuspectedLocalSidecar(cleanRel, d.Name()) {
			suspectedSidecars = append(suspectedSidecars, cleanRel)
		} else if !strings.HasPrefix(d.Name(), ".") {
			primaryBinaries = append(primaryBinaries, cleanRel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Move primary binaries to dstDir
	for _, rel := range primaryBinaries {
		srcPath := filepath.Join(srcDir, rel)
		dstPath := filepath.Join(dstDir, rel)
		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			return nil, err
		}
		if err := os.Rename(srcPath, dstPath); err != nil {
			_ = os.Remove(dstPath)
			if err := copyFile(srcPath, dstPath); err != nil {
				return nil, err
			}
			_ = os.Remove(srcPath)
		}
		_ = os.Chmod(dstPath, 0755)
	}

	// Route suspected sidecars
	var selected []string
	if len(suspectedSidecars) > 0 {
		if r != nil && (r.IncludeSidecars || (r.CliParams != nil && r.CliParams.IncludeSidecars)) {
			selected = suspectedSidecars
		} else if r != nil && r.isInteractive() {
			sel, err := r.interactiveMultiselect("Suspected sidecar assets detected. Select items to deploy:", suspectedSidecars)
			if err != nil {
				return nil, err
			}
			selected = sel
		} else {
			if r == nil || r.shouldWarnUnmappedAssets(nil) {
				pterm.Warning.Printf("Release contains unmapped sidecar assets (%s). Pass --sidecars to capture them on future installs.\n", strings.Join(suspectedSidecars, ", "))
			}
		}
	}

	var installedSidecars []string
	if len(selected) > 0 {
		// Sidecar target path is now determined by IncludeSidecars mode in release.go
		// This code path is for suspected sidecars handling in compile flow
		sidecarTarget := filepath.Join(xdg.DataHome, "gh-pt", "sidecars")
		if err := os.MkdirAll(sidecarTarget, 0755); err != nil {
			log.Warn("could not create sidecar target directory", "error", err)
		}

		for _, item := range selected {
			srcPath := filepath.Join(srcDir, item)
			dstPath := filepath.Join(sidecarTarget, filepath.Base(item))
			if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
				return installedSidecars, err
			}
			if err := os.Rename(srcPath, dstPath); err != nil {
				_ = os.Remove(dstPath)
				if err := copyFile(srcPath, dstPath); err != nil {
					return installedSidecars, err
				}
				_ = os.Remove(srcPath)
			}
			_ = os.Chmod(dstPath, 0755)

			installedSidecars = append(installedSidecars, dstPath)
			if r != nil {
				r.InstalledSidecars = append(r.InstalledSidecars, dstPath)
			}
		}

		if r != nil && !r.NoSaveState && r.Repository != "" {
			st, err := state.LoadState()
			if err == nil {
				if st.Apps == nil {
					st.Apps = make(map[string]*state.InstalledApp)
				}
				app, ok := st.Apps[r.Repository]
				if !ok {
					if st.Repos != nil {
						app, ok = st.Repos[r.Repository]
					}
				}
				if !ok || app == nil {
					app = &state.InstalledApp{
						Repository: r.Repository,
						TargetPath: dstDir,
					}
					st.Apps[r.Repository] = app
				}
				// Store the sidecar regex pattern in state
				if r.CliParams != nil && r.CliParams.Sidecars != "" {
					app.Sidecars = r.CliParams.Sidecars
				}
				for _, sc := range installedSidecars {
					if !sliceContains(app.InstalledSidecars, sc) {
						app.InstalledSidecars = append(app.InstalledSidecars, sc)
					}
				}
				_ = st.Save()
			}
		}
	}

	// Clean up empty directories in srcDir
	if err := safety.AssertSafeToRemoveAll(srcDir); err == nil {
		_ = filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() && path != srcDir {
				_ = safety.Remove(path)
			}
			return nil
		})
		_ = safety.Remove(srcDir)
	}

	return installedSidecars, nil
}

func MoveDistWithSidecarDetection(r *RootCLI, srcDir, dstDir string) ([]string, error) {
	return r.moveDistWithSidecarDetection(srcDir, dstDir)
}

func MoveDistBinaries(srcDir, dstDir string) error {
	var r *RootCLI
	_, err := r.moveDistWithSidecarDetection(srcDir, dstDir)
	return err
}

func (r *RootCLI) resolveCompileDependencies(dependencies []ai.Dependency, repoDir string, cfg *config.Config) (*state.InstallPkg, error) {
	r.ensureCliParams()

	if len(dependencies) == 0 {
		return nil, nil
	}

	skipDeps := r.NoDeps || (r.CliParams != nil && r.CliParams.NoDeps) || (cfg != nil && cfg.Core.NoDeps)
	if skipDeps {
		log.Info("skipping dependency installation due to no-deps flag")
		return nil, nil
	}

	// 4a. Call heuristics.DetectEcosystem(repoPath) on the cloned repo to detect the primary ecosystem
	ecosystem, err := heuristics.DetectEcosystem(repoDir)
	if err != nil {
		log.Warn("failed to detect repository ecosystem", "error", err)
		ecosystem = heuristics.PriorityDefault
	}
	log.Info("detected repository ecosystem", "ecosystem", ecosystem)

	// 4b. Use heuristics.ResolvePriorityChain(config.DependencyResolution.Priorities, ecosystem) to get resolver priority order
	var prioritiesMap map[string][]string
	if cfg != nil {
		prioritiesMap = cfg.DependencyResolution.Priorities
	}
	priorityChain := heuristics.ResolvePriorityChain(prioritiesMap, ecosystem)
	log.Info("resolved dependency priority chain", "chain", priorityChain)

	// 4d. If r.CliParams.PromptDeps is set, present the dependency list to the user for approval before installing
	promptDeps := r.PromptDeps
	if r.CliParams != nil && r.CliParams.PromptDeps {
		promptDeps = true
	}

	if promptDeps {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return nil, fmt.Errorf("--prompt-deps requires an interactive terminal (isatty is false)")
		}

		fmt.Println("\nDiscovered build dependencies:")
		for _, dep := range dependencies {
			fmt.Printf("  - %s (resolver: %s)\n", dep.Name, dep.GetResolver())
		}
		confirmed, err := pterm.DefaultInteractiveConfirm.WithDefaultValue(true).Show("Do you want to install these dependencies?")
		if err != nil || !confirmed {
			return nil, fmt.Errorf("dependency installation aborted by user")
		}
	}

	var lastManager string
	var installedPackages []string

	// 4c. Iterate over payload.Dependencies and resolve each one using resolver.GetManager(dep.GetResolver()) -> mgr.Install([]string{dep.Name})
	for _, dep := range dependencies {
		var mgr resolver.PackageManager
		var mgrErr error
		resolverName := dep.GetResolver()
		if resolverName != "" {
			mgr, mgrErr = resolver.GetManager(resolverName)
		}
		if mgr == nil || mgrErr != nil {
			for _, name := range priorityChain {
				m, err := resolver.GetManager(name)
				if err == nil && m.IsInstalled() {
					mgr = m
					break
				}
			}
		}
		if mgr == nil {
			mgr, _ = resolver.GetNativeManager()
		}
		if mgr == nil {
			return nil, fmt.Errorf("could not find suitable package manager to install dependency '%s'", dep.Name)
		}

		log.Info("installing dependency", "dependency", dep.Name, "resolver", mgr.Name())
		if err := mgr.Install([]string{dep.Name}); err != nil {
			return nil, fmt.Errorf("failed to install dependency '%s' with resolver '%s': %w", dep.Name, mgr.Name(), err)
		}
		lastManager = mgr.Name()
		installedPackages = append(installedPackages, dep.Name)
	}

	var pkgInfo *state.InstallPkg
	if len(installedPackages) > 0 {
		pkgInfo = &state.InstallPkg{
			Manager:   lastManager,
			PackageID: strings.Join(installedPackages, ", "),
		}
	}

	return pkgInfo, nil
}

func (r *RootCLI) handleAISafetyScan(cfg *config.Config) error {
	if r.DryRun {
		log.Info(fmt.Sprintf("[dry-run] Would initiate AI safety scan for %s", r.Repository))
		return nil
	}

	aiCmdTemplate := r.AICmd
	if cfg != nil && cfg.AI.AICmd != "" && (r.AICmd == "" || r.AICmd == "agy -p \"%s\"") {
		aiCmdTemplate = cfg.AI.AICmd
	}
	if aiCmdTemplate == "" {
		aiCmdTemplate = "agy -p \"%s\""
	}

	prompt := fmt.Sprintf("Analyze the GitHub repository %s for safety concerns, malicious code, suspicious recent commits, or backdoors. Report your findings concisely and explicitly state if it appears safe or compromised.", r.Repository)

	log.Info(fmt.Sprintf("Initiating AI safety scan for %s...", r.Repository))
	if err := runAIAgent(aiCmdTemplate, prompt, ""); err != nil {
		return fmt.Errorf("AI safety scan failed to execute: %w", err)
	}

	if !r.DisablePrompts {
		var confirm string
		fmt.Printf("\nSafety scan complete. Do you want to proceed with the installation of %s? [y/N]: ", r.Repository)
		_, _ = fmt.Scanln(&confirm)
		if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
			return fmt.Errorf("installation aborted by user after AI safety scan")
		}
	}

	return nil
}

func forceRemoveAll(path string) error {
	if err := safety.AssertSafeToRemoveAll(path); err != nil {
		return err
	}
	err := safety.RemoveAll(path)
	if err != nil {
		if runtime.GOOS != "windows" {
			_ = exec.Command("rm", "-rf", path).Run()
		} else {
			_ = exec.Command("cmd", "/C", "rmdir", "/s", "/q", path).Run()
		}
		return safety.RemoveAll(path)
	}
	return nil
}

func (r *RootCLI) handleCompileFromSource(cfg *config.Config) error {
	r.ensureCliParams()
	manifestPath, compilePath := getSourcePaths(r.Repository)
	scriptPath := compilePath
	targetPath := r.TargetPath
	if targetPath == "" {
		targetPath = GetDefaultTargetPath()
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not determine home directory: %w", err)
	}
	parts := strings.Split(r.Repository, "/")
	repoName := parts[len(parts)-1]
	repoDir := filepath.Join(homeDir, "builds", repoName)

	// Source builds default to symlink mode for better isolation and updates
	symlinkDir := ""
	if r.CompileFromSource {
		// Source builds always use symlink by default
		var ownerID, repoID string
		if len(parts) >= 2 {
			ownerID = parts[0]
			repoID = parts[1]
		} else if len(parts) == 1 {
			ownerID = ""
			repoID = parts[0]
		}
		symlinkDir = filepath.Join(homeDir, "src", "apps", ownerID, repoID)
	} else if r.Symlink {
		// Regular installs only use symlink if explicitly requested
		var ownerID, repoID string
		if len(parts) >= 2 {
			ownerID = parts[0]
			repoID = parts[1]
		} else if len(parts) == 1 {
			ownerID = ""
			repoID = parts[0]
		}
		symlinkDir = filepath.Join(homeDir, "src", "apps", ownerID, repoID)
	}

	if r.DryRun {
		if symlinkDir != "" {
			log.Info(fmt.Sprintf("[dry-run] Would clone %s to %s, generate build script at %s, compile/install to %s, and symlink to %s", r.Repository, repoDir, scriptPath, symlinkDir, targetPath))
		} else {
			log.Info(fmt.Sprintf("[dry-run] Would clone %s to %s, generate build script at %s, and execute compilation", r.Repository, repoDir, scriptPath))
		}
		return nil
	}

	if r.Symlink {
		if err := os.MkdirAll(symlinkDir, 0755); err != nil {
			return fmt.Errorf("failed to create symlink apps directory: %w", err)
		}
		// Mark directory as managed by gh-pt for safe removal later
		_ = safety.WriteGhptManagedMarker(symlinkDir)
	}

	if err := os.MkdirAll(filepath.Dir(repoDir), 0755); err != nil {
		return fmt.Errorf("failed to create builds directory: %w", err)
	}

	// Ensure fresh clone
	if r.Overwrite {
		_ = forceRemoveAll(repoDir)
	} else if _, err := os.Stat(repoDir); err == nil {
		return fmt.Errorf("git repo already exists at %s, use -f or --force to overwrite", repoDir)
	}

	log.Info("handling compile-from-source with AI",
		"repository", r.Repository,
		"build_dir", repoDir,
		"script_path", scriptPath,
		"manifest_path", manifestPath,
		"target_path", targetPath,
		"symlink_dir", symlinkDir,
	)

	// 1. Clone repo into builds directory
	cloneArgs := []string{"repo", "clone", r.Repository, repoDir}
	if r.MaxDepth > 0 {
		cloneArgs = append(cloneArgs, "--", "--depth", fmt.Sprintf("%d", r.MaxDepth))
	}
	stdOut, stdErr, err := gh.Exec(cloneArgs...)
	if err != nil {
		return fmt.Errorf("failed to clone repository to builds dir: %s (%w)", stdErr.String(), err)
	}
	log.Info("cloned repository to builds directory", "output", stdOut.String())

	// 2. Setup .ghpt directory and symlinks
	ghptDir := filepath.Join(repoDir, ".ghpt")
	if err := os.MkdirAll(ghptDir, 0755); err != nil {
		return fmt.Errorf("failed to create .ghpt directory in repo: %w", err)
	}

	var installDirTarget string
	if r.Global {
		installDirTarget = "/usr/local/.ghpt"
	} else {
		installDirTarget = filepath.Join(homeDir, ".local", ".ghpt")
	}
	_ = os.MkdirAll(installDirTarget, 0755)

	installDirSymlink := filepath.Join(ghptDir, "install-dir")
	_ = os.Remove(installDirSymlink)
	if err := os.Symlink(installDirTarget, installDirSymlink); err != nil {
		log.Warn("could not create install-dir symlink", "target", installDirTarget, "link", installDirSymlink, "error", err)
	}

	manifestSymlink := filepath.Join(ghptDir, "manifest.json")
	_ = os.Remove(manifestSymlink)
	if err := os.Symlink(manifestPath, manifestSymlink); err != nil {
		log.Warn("could not create manifest.json symlink", "target", manifestPath, "link", manifestSymlink, "error", err)
	}

	compileSymlink := filepath.Join(ghptDir, "compile.sh")
	_ = os.Remove(compileSymlink)
	if err := os.Symlink(compilePath, compileSymlink); err != nil {
		log.Warn("could not create compile.sh symlink", "target", compilePath, "link", compileSymlink, "error", err)
	}

	if r.Overwrite {
		_ = os.Remove(scriptPath)
		_ = os.Remove(manifestPath)
	}

	// 3. Resolve AI command template
	aiCmdTemplate := r.AICmd
	if cfg != nil && cfg.AI.AICmd != "" && (r.AICmd == "" || r.AICmd == `agy -p "%s"`) {
		aiCmdTemplate = cfg.AI.AICmd
	}
	if aiCmdTemplate == "" {
		aiCmdTemplate = `agy -p "%s"`
	}

	var payload *ai.CompilePayload
	if _, err := os.Stat(scriptPath); err == nil {
		log.Info("found existing compile script, attempting to use it", "script", scriptPath)
		if scriptBytes, readErr := os.ReadFile(scriptPath); readErr == nil {
			var deps []ai.Dependency
			if manifestBytes, mErr := os.ReadFile(manifestPath); mErr == nil {
				var m ai.Manifest
				if jErr := json.Unmarshal(manifestBytes, &m); jErr == nil {
					deps = m.Dependencies
				}
			}
			payload = &ai.CompilePayload{
				Dependencies: deps,
				Script:       string(scriptBytes),
			}
		}
	}

	if payload == nil {
		// Ensure scripts directory exists
		if err := os.MkdirAll(filepath.Dir(scriptPath), 0755); err != nil {
			return fmt.Errorf("failed to create scripts directory: %w", err)
		}

		// Stage 1: Generate body.sh using AI container with network access
		prompt := buildCompilePrompt(r.Repository, repoDir, scriptPath, targetPath, symlinkDir)
		log.Info(fmt.Sprintf("Generating AI compilation body.sh using container: %s", aiCmdTemplate))
		aiResp, err := r.runAIAgentInContainer(aiCmdTemplate, prompt, repoDir)
		if err != nil {
			return fmt.Errorf("AI agent failed to generate body.sh: %w", err)
		}

		p, parseErr := ai.ParseAIOutput(aiResp)
		if parseErr != nil {
			// Fallback: try reading existing body.sh if available
			bodyPath := filepath.Join(ghptDir, "body.sh")
			if bodyContent, readErr := os.ReadFile(bodyPath); readErr == nil && len(bodyContent) > 0 {
				// Create a minimal payload from existing body.sh
				p = &ai.CompilePayload{
					Script: string(bodyContent),
				}
				parseErr = nil
			}
		}
		if parseErr != nil {
			return fmt.Errorf("failed to parse AI output: %w", parseErr)
		}
		payload = p

		// Extract body.sh from payload.Script (AI should output body.sh content)
		bodyContent := payload.Script
		if bodyContent == "" {
			return fmt.Errorf("AI did not generate body.sh content")
		}

		// Validate body.sh using AST-based validation
		bodyPath := filepath.Join(ghptDir, "body.sh")
		if err := os.WriteFile(bodyPath, []byte(bodyContent), 0644); err != nil {
			return fmt.Errorf("failed to write body.sh: %w", err)
		}
		if err := ai.ValidateBodyScript(bodyPath); err != nil {
			return fmt.Errorf("body.sh validation failed: %w", err)
		}
		log.Info("body.sh validation passed", "path", bodyPath)

		// Write manifest.json
		manifestContent := payload.ManifestJSON
		if manifestContent == "" {
			manifestContent = ai.ManifestJSONTemplate
		}
		_ = os.WriteFile(manifestPath, []byte(manifestContent), 0644)

		// Render and write header.sh with template substitution
		headerContent := ai.HeaderTemplate
		headerContent = strings.ReplaceAll(headerContent, "{{.InstallPrefix}}", installDirTarget)
		headerContent = strings.ReplaceAll(headerContent, "{{.RepoPath}}", repoDir)
		headerContent = strings.ReplaceAll(headerContent, "{{.Repository}}", r.Repository)
		headerContent = strings.ReplaceAll(headerContent, "{{.Version}}", "source-build")
		headerPath := filepath.Join(ghptDir, "header.sh")
		if err := os.WriteFile(headerPath, []byte(headerContent), 0644); err != nil {
			return fmt.Errorf("failed to write header.sh: %w", err)
		}

		// Write footer.sh (no template substitution needed)
		footerPath := filepath.Join(ghptDir, "footer.sh")
		if err := os.WriteFile(footerPath, []byte(ai.FooterTemplate), 0644); err != nil {
			return fmt.Errorf("failed to write footer.sh: %w", err)
		}

		// Also write the full compile.sh for backward compatibility (reconstructed)
		compileScript := headerContent + bodyContent + ai.FooterTemplate
		if err := os.WriteFile(scriptPath, []byte(compileScript), 0755); err != nil {
			return fmt.Errorf("failed to write compilation script '%s': %w", scriptPath, err)
		}
		_ = os.Chmod(scriptPath, 0755)
	}

	// 4. Resolve dependencies and watch install-dir
	beforeDepsSnapshot, _ := snapshotDirFiles(installDirTarget)
	depsPkg, depsErr := r.resolveCompileDependencies(payload.Dependencies, repoDir, cfg)
	if depsErr != nil {
		return fmt.Errorf("dependency resolution failed: %w", depsErr)
	}
	newDepsFiles, _ := diffDirFiles(installDirTarget, beforeDepsSnapshot)
	depsRecorded := r.processNewFilesAndSymlinks(newDepsFiles)

	if !r.NoSaveState {
		if st, err := state.LoadState(); err == nil {
			entry := st.GetInstallMap(r.Repository)
			if entry == nil {
				entry = &state.InstallMapEntry{}
			}
			entry.Deps = &state.InstallStep{
				Pkg:   depsPkg,
				Files: depsRecorded,
			}
			_ = st.SetInstallMap(r.Repository, entry)
		}
	}

	// 5. Run the generated compile script and watch install-dir
	beforeCompileSnapshot, _ := snapshotDirFiles(installDirTarget)
	maxRetries := 2
	var lastErr error
	var lastOutput string

	useContainer := !r.NoCompileContainer && os.Getenv("GHPT_NO_COMPILE_CONTAINER") != "1"
	
	if useContainer {
		// Verify container runtime is available
		if _, err := exec.LookPath("podman"); err != nil {
			if _, err := exec.LookPath("docker"); err != nil {
				return fmt.Errorf("mandatory AI compilation sandbox error: no container runtime (podman or docker) found in PATH. Running unvetted AI compilation scripts directly on the host is blocked by default to prevent supply-chain compromise. Install docker/podman, or explicitly opt out with --no-compile-container (or GHPT_NO_COMPILE_CONTAINER=1)")
			}
		}
	} else {
		log.Warn("*** [SECURITY: COMPILE_SANDBOX_BYPASS] Executing AI compile script directly on host without container isolation (--no-compile-container specified). Host compromise risk! ***")
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		log.Info("executing compile script in hardened container (stage 2)", "attempt", attempt+1)
		
		// Use the compile package's hardened container execution with three-part reconstruction
		// The container will reconstruct compile.sh from header.sh + body.sh + footer.sh at runtime
		reconstructCmd := "bash -c 'cat /build/.ghpt/header.sh /build/.ghpt/body.sh /build/.ghpt/footer.sh > /build/.ghpt/compile.sh && bash /build/.ghpt/compile.sh'"
		
		compileCfg := &compile.Config{
			Repository:       r.Repository,
			RepoDir:          repoDir,
			BuildDir:         repoDir,
			ScriptPath:       scriptPath, // Not used directly, but kept for compatibility
			ManifestPath:     manifestPath,
			TargetPath:       targetPath,
			SymlinkDir:       symlinkDir,
			InstallDirTarget: installDirTarget,
			Track:            r.Track,
			Version:          "source-build",
			CompileScript:    reconstructCmd,
		}
		
		// Execute in hardened container (network=none, cap-drop=ALL, read-only, seccomp, non-root user)
		err := compile.ExecuteInContainer(compileCfg)
		if err == nil {
			lastErr = nil
			break
		}
		
		lastErr = err
		log.Warn("compile script failed", "error", err, "attempt", attempt+1)
		
		if attempt < maxRetries {
			log.Info(fmt.Sprintf("Prompting AI to fix body.sh (retry %d of %d)...", attempt+1, maxRetries))
			// Re-read body.sh for context
			bodyPath := filepath.Join(ghptDir, "body.sh")
			fixPrompt := buildCompileFixPrompt(r.Repository, repoDir, bodyPath, targetPath, symlinkDir, err.Error(), attempt+1)
			fixResp, fixErr := r.runAIAgentInContainer(aiCmdTemplate, fixPrompt, repoDir)
			if fixErr != nil {
				log.Warn("AI repair command execution failed", "error", fixErr)
			}
			fixPayload, pErr := ai.ParseAIOutput(fixResp)
			if pErr != nil {
				log.Warn("failed to parse AI fix response", "error", pErr)
			}
			if fixPayload != nil && fixPayload.Script != "" {
				// Validate and write new body.sh
				newBodyContent := fixPayload.Script
				if err := os.WriteFile(bodyPath, []byte(newBodyContent), 0644); err != nil {
					log.Warn("failed to write fixed body.sh", "error", err)
				} else if err := ai.ValidateBodyScript(bodyPath); err != nil {
					log.Warn("fixed body.sh validation failed", "error", err)
				} else {
					log.Info("fixed body.sh validation passed")
					// Update manifest if provided
					if fixPayload.ManifestJSON != "" {
						_ = os.WriteFile(manifestPath, []byte(fixPayload.ManifestJSON), 0644)
					}
					// Reconstruct compile.sh for fallback
					headerContent := ai.HeaderTemplate
					headerContent = strings.ReplaceAll(headerContent, "{{.InstallPrefix}}", installDirTarget)
					headerContent = strings.ReplaceAll(headerContent, "{{.RepoPath}}", repoDir)
					headerContent = strings.ReplaceAll(headerContent, "{{.Repository}}", r.Repository)
					headerContent = strings.ReplaceAll(headerContent, "{{.Version}}", "source-build")
					compileScript := headerContent + newBodyContent + ai.FooterTemplate
					_ = os.WriteFile(scriptPath, []byte(compileScript), 0755)
				}
			}
		}
	}

	if lastErr != nil {
		return fmt.Errorf("compile script execution failed after %d retries: %w (output: %s)", maxRetries, lastErr, lastOutput)
	}

	newCompileFiles, _ := diffDirFiles(installDirTarget, beforeCompileSnapshot)
	compileRecorded := r.processNewFilesAndSymlinks(newCompileFiles)

	// Check for legacy .ghpt/dist/ handoff
	distDir := filepath.Join(repoDir, ".ghpt", "dist")
	var installedSidecars []string
	if info, err := os.Stat(distDir); err == nil && info.IsDir() {
		installDest := targetPath
		if symlinkDir != "" {
			installDest = symlinkDir
		}
		log.Info("moving binaries from .ghpt/dist to install destination", "distDir", distDir, "installDest", installDest)
		sidecars, err := r.moveDistWithSidecarDetection(distDir, installDest)
		if err != nil {
			return fmt.Errorf("failed to move binaries from .ghpt/dist to install path: %w", err)
		}
		installedSidecars = sidecars
	}

	if symlinkDir != "" {
		if err := createSymlinks(symlinkDir, r.TargetPath); err != nil {
			return fmt.Errorf("failed to create symlinks: %w", err)
		}
	}

	// Determine version based on track
	var version string
	if r.Track == "latest-commit" {
		// Use commit hash for latest-commit track
		commitHash := "unknown"
		revParseCmd := exec.Command("git", "rev-parse", "HEAD")
		revParseCmd.Dir = repoDir
		if out, err := revParseCmd.Output(); err == nil {
			commitHash = strings.TrimSpace(string(out))
		}
		version = commitHash
	} else {
		// Use git tag for stable/prerelease tracks
		version = "unknown"
		describeCmd := exec.Command("git", "describe", "--tags", "--abbrev=0")
		describeCmd.Dir = repoDir
		if out, err := describeCmd.Output(); err == nil {
			version = strings.TrimSpace(string(out))
		} else {
			// Fallback to commit hash if no tags
			revParseCmd := exec.Command("git", "rev-parse", "HEAD")
			revParseCmd.Dir = repoDir
			if out, err := revParseCmd.Output(); err == nil {
				version = strings.TrimSpace(string(out))
			}
		}
	}

	if !r.NoSaveState {
		st, err := state.LoadState()
		if err == nil {
			entry := st.GetInstallMap(r.Repository)
			if entry == nil {
				entry = &state.InstallMapEntry{}
			}
			entry.Installed = &state.InstallStep{
				Files: compileRecorded,
			}
			_ = st.SetInstallMap(r.Repository, entry)

			var existingHooks map[string]string
			if existing, ok := st.Apps[r.Repository]; ok && existing != nil && len(existing.Hooks) > 0 {
				existingHooks = existing.Hooks
			}
			sidecarList := r.InstalledSidecars
			if len(sidecarList) == 0 && len(installedSidecars) > 0 {
				sidecarList = installedSidecars
			}
			_ = st.AddApp(&state.InstalledApp{
				Repository:        r.Repository,
				TargetPath:        targetPath,
				Global:            r.Global,
				Version:           version,
				CommitHash:        version, // For source builds, version is commit hash or tag
				CompileScript:     scriptPath,
				Pinned:            r.PinInstall,
				MaxDepth:          r.MaxDepth,
				SymlinkDir:        symlinkDir,
				Sidecars:          r.CliParams.Sidecars,
				InstalledSidecars: sidecarList,
				Hooks:             existingHooks,
			})
		}
	}

	// 6. AI Verification Prompt
	verifyPrompt := fmt.Sprintf("Please test that the compilation and installation for repository '%s' (cloned at '%s') were successful. Verify that all files in '%s' ('install-dir') and its subfolders are supposed to be there, and test running the installed binary/binaries to confirm they function correctly. Respond confirming verification.", r.Repository, repoDir, filepath.Join(ghptDir, "install-dir"))
	log.Info("Prompting AI to test and verify compilation and installed files...")
	_, verifyErr := runAIAgentWithOutput(aiCmdTemplate, verifyPrompt, repoDir)
	if verifyErr != nil {
		log.Warn("AI verification returned warning or non-zero exit", "error", verifyErr)
	}

	indicator := GetStateIndicator(true, r.PinInstall, false, false, false, r.DisableIcons || r.NoEmojis)
	displayRepo := r.Repository
	if indicator != "" {
		displayRepo = indicator + " " + r.Repository
	}

	// 7. Upon confirmation:
	// If --symlink was passed: output success message and exit.
	// If there was no --symlink: each file logged in state.json is moved to overwrite its symlink, then output success and exit.
	if !r.Symlink {
		if st, err := state.LoadState(); err == nil {
			if entry := st.GetInstallMap(r.Repository); entry != nil && entry.Installed != nil {
				for _, filePath := range entry.Installed.Files {
					if strings.Contains(filePath, ".ghpt/") {
						stagedPath := filePath
						activePath := strings.Replace(stagedPath, ".ghpt/", "", 1)
						if _, err := os.Lstat(activePath); err == nil {
							_ = os.Remove(activePath)
						}
						if err := os.MkdirAll(filepath.Dir(activePath), 0755); err == nil {
							if err := moveFile(stagedPath, activePath); err != nil {
								log.Warn("failed to move staged file over symlink", "staged", stagedPath, "active", activePath, "error", err)
							}
						}
					}
				}
			}
		}
	}

	log.Info(fmt.Sprintf("Successfully compiled and installed %s from source!", displayRepo))
	pterm.Success.Printf("Successfully compiled and installed %s from source!\n", displayRepo)

	return r.runPostInstallHook(r.Repository)
}

func (r *RootCLI) runPostInstallHook(repo string) error {
	st, err := state.LoadState()
	if err != nil || st == nil || st.Apps == nil {
		return nil
	}
	app, exists := st.Apps[repo]
	if !exists || app == nil || app.Hooks == nil {
		return nil
	}
	script, exists := app.Hooks["post-install"]
	if !exists || strings.TrimSpace(script) == "" {
		return nil
	}
	return executeHookScript("post-install", repo, script)
}

func GetDefaultTargetPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(homeDir, ".local", "bin")
}

func GetDefaultClonePath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(homeDir, "src", "repos")
}

func GetDefaultForkPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(homeDir, "projects")
}

func GetDefaultInstallTypes() string {
	tarballRgx := `t(ar\\.)?([gxl]z|bz2?|zst),tar(\\.lzma)?`
	switch runtime.GOOS {
	case "windows":
		return fmt.Sprintf("exe,msi,7z,%s,zip,py,ts,js", tarballRgx)
	case "darwin":
		return fmt.Sprintf("dmg,7z,%s,zip,py,ts,js,none", tarballRgx)
	case "linux":
		hasDpkg := false
		hasRpm := false
		if _, err := exec.LookPath("dpkg"); err == nil {
			hasDpkg = true
		}
		if _, err := exec.LookPath("rpm"); err == nil {
			hasRpm = true
		}

		if hasDpkg && !hasRpm {
			return fmt.Sprintf("deb,snap,flatpak,appimage,7z,%s,zip,py,ts,js,none", tarballRgx)
		} else if hasRpm && !hasDpkg {
			return fmt.Sprintf("rpm,snap,flatpak,appimage,7z,%s,zip,py,ts,js,none", tarballRgx)
		} else if hasRpm && hasDpkg {
			// If both exist (e.g. alien installed), try to check os-release
			osRelease, err := os.ReadFile("/etc/os-release")
			if err == nil {
				content := strings.ToLower(string(osRelease))
				if strings.Contains(content, "id=fedora") || strings.Contains(content, "id=rhel") || strings.Contains(content, "id=centos") {
					return fmt.Sprintf("rpm,deb,snap,flatpak,appimage,7z,%s,zip,py,ts,js,none", tarballRgx)
				}
			}
			return fmt.Sprintf("deb,rpm,snap,flatpak,appimage,7z,%s,zip,py,ts,js,none", tarballRgx)
		}

		// Arch or others without rpm/deb natively
		return fmt.Sprintf("appimage,flatpak,snap,7z,%s,zip,py,ts,js,none", tarballRgx)
	case "freebsd":
		return fmt.Sprintf("pkg,txz,7z,%s,zip,py,ts,js,none", tarballRgx)
	default:
		return fmt.Sprintf("7z,%s,zip,none", tarballRgx)
	}
}
func buildRegexFromTypes(types []string, wine string) []string {
	if len(types) == 0 {
		typesStr := GetDefaultInstallTypes()
		for _, t := range strings.Split(typesStr, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				types = append(types, t)
			}
		}
	}
	archRegex := runtime.GOARCH
	switch runtime.GOARCH {
	case "amd64":
		archRegex = "(?:amd64|x86_64|x64)"
	case "arm64":
		archRegex = "(?:arm64|aarch64)"
	}

	var osRegexList []string
	switch runtime.GOOS {
	case "darwin":
		osRegexList = append(osRegexList, "(?:darwin|macos|apple)")
	case "windows":
		osRegexList = append(osRegexList, "(?:windows|win)")
	case "freebsd":
		osRegexList = append(osRegexList, "(?:freebsd)")
	case "linux":
		osRelease, err := os.ReadFile("/etc/os-release")
		if err == nil {
			content := string(osRelease)
			lines := strings.Split(content, "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "ID=") {
					distro := strings.ToLower(strings.Trim(strings.TrimPrefix(line, "ID="), "\""))
					if distro != "" && distro != "linux" {
						osRegexList = append(osRegexList, distro)
					}
				} else if strings.HasPrefix(line, "ID_LIKE=") {
					idLike := strings.ToLower(strings.Trim(strings.TrimPrefix(line, "ID_LIKE="), "\""))
					if idLike != "" {
						for _, likeDistro := range strings.Fields(idLike) {
							if likeDistro != "linux" {
								osRegexList = append(osRegexList, likeDistro)
							}
						}
					}
				}
			}
		}
		osRegexList = append(osRegexList, "linux")
	}

	hwSpecific := ""
	if runtime.GOOS == "linux" {
		if _, err := os.Stat("/sys/class/accel"); err == nil {
			hwSpecific = "npu"
		}
		// Interrogate exact compute nodes, ignoring generic display interfaces
		if _, err := os.Stat("/dev/nvidia0"); err == nil {
			if hwSpecific != "" {
				hwSpecific += "|"
			}
			hwSpecific += "cuda"
		} else if _, err := os.Stat("/dev/kfd"); err == nil {
			if hwSpecific != "" {
				hwSpecific += "|"
			}
			hwSpecific += "rocm"
		}
	}

	buildFinal := func(baseRegex string, t string) string {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "none" {
			return fmt.Sprintf(`^%s$`, baseRegex)
		} else if t != "" {
			return fmt.Sprintf(`^%s\.(?i:%s)$`, baseRegex, t)
		}
		return fmt.Sprintf(`^%s$`, baseRegex)
	}

	var matchers []string

	for _, t := range types {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}

		lowerT := strings.ToLower(t)
		isOsFormatPkg := lowerT == "deb" || lowerT == "rpm" || lowerT == "pkg" || lowerT == "txz" || lowerT == "dmg"
		// AppImage, Flatpak, and Snap are Linux-only universal packages — no OS token needed in filename.
		isUniversalLinux := lowerT == "appimage" || lowerT == "flatpak" || lowerT == "snap"

		for _, osRegex := range osRegexList {
			if hwSpecific != "" {
				hwBaseRegex := fmt.Sprintf(`.*(?:%s.+%s.+%s|%s.+%s.+%s|%s.+%s|%s.+%s).*`, archRegex, osRegex, hwSpecific, osRegex, archRegex, hwSpecific, hwSpecific, archRegex, archRegex, hwSpecific)
				matchers = append(matchers, buildFinal(hwBaseRegex, t))
			}

			baseRegex := fmt.Sprintf(`.*(?:%s.+%s|%s.+%s).*`, archRegex, osRegex, osRegex, archRegex)
			matchers = append(matchers, buildFinal(baseRegex, t))
		}

		// Format-specific fallback: format already implies OS (e.g. .deb implies debian/ubuntu), match arch
		if isOsFormatPkg {
			archOnlyRegex := fmt.Sprintf(`.*%s.*`, archRegex)
			matchers = append(matchers, buildFinal(archOnlyRegex, t))
		}

		// Universal Linux packages: only need arch match, no OS token required
		if isUniversalLinux {
			archOnlyRegex := fmt.Sprintf(`.*%s.*`, archRegex)
			matchers = append(matchers, buildFinal(archOnlyRegex, t))
			// Also allow bare format matches (no arch specified)
			matchers = append(matchers, buildFinal(`.*`, t))
		}

		// Fallback: OS only
		for _, osRegex := range osRegexList {
			fallbackRegex := fmt.Sprintf(`.*%s.*`, osRegex)
			matchers = append(matchers, buildFinal(fallbackRegex, t))
		}
	}

	for _, t := range types {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		matchers = append(matchers, buildFinal(`.*`, t))
	}

	var normalMatchers []string
	// Copy current matchers to normalMatchers
	normalMatchers = append(normalMatchers, matchers...)
	matchers = []string{} // Reset matchers

	var winMatchers []string
	if (wine == "allow" || wine == "priority" || wine == "force") && runtime.GOOS != "windows" {
		winOsRegex := "(?:windows|win)"
		winBaseRegex := fmt.Sprintf(`.*(?:%s.+%s|%s.+%s).*`, archRegex, winOsRegex, winOsRegex, archRegex)
		for _, t := range append([]string{"exe", "msi"}, types...) {
			winMatchers = append(winMatchers, buildFinal(winBaseRegex, t))
		}
	}

	if wine == "force" && runtime.GOOS != "windows" {
		matchers = append(matchers, winMatchers...)
	} else if wine == "priority" && runtime.GOOS != "windows" {
		matchers = append(matchers, winMatchers...)
		matchers = append(matchers, normalMatchers...)
	} else {
		matchers = append(matchers, normalMatchers...)
		matchers = append(matchers, winMatchers...)
	}

	return matchers
}

func GetEnvPrefix() string {
	envPrefix := os.Getenv(GH_PT_PREFIX_ENV)
	if envPrefix == "" {
		envPrefix = os.Getenv(GH_INSTALL_PREFIX_ENV)
	}
	if envPrefix == "" {
		envPrefix = GH_PT_DEFAULT_PREFIX
	}

	return strings.ToUpper(envPrefix)
}

func loadConfig() *config.Config {
	cfg, _ := config.LoadConfig()
	return cfg
}

// createSymlinks creates symlinks from executables in symlinkDir to targetPath
func createSymlinks(symlinkDir, targetPath string) error {
	if err := os.MkdirAll(targetPath, 0755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	// Find all executable files in symlinkDir
	entries, err := os.ReadDir(symlinkDir)
	if err != nil {
		return fmt.Errorf("failed to read symlinkDir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			// Check if file is executable
			info, err := entry.Info()
			if err != nil {
				continue
			}
			if info.Mode()&0111 != 0 { // Check if executable
				srcPath := filepath.Join(symlinkDir, entry.Name())
				destPath := filepath.Join(targetPath, entry.Name())

				// Remove existing symlink/file if exists
				if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("failed to remove existing file/symlink %s: %w", destPath, err)
				}

				if err := os.Symlink(srcPath, destPath); err != nil {
					return fmt.Errorf("failed to create symlink %s -> %s: %w", destPath, srcPath, err)
				}

				log.Info("created symlink", "source", srcPath, "dest", destPath)
			}
		}
	}

	return nil
}
