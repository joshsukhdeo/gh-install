package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/adrg/xdg"
	"github.com/alecthomas/kong"
	"github.com/charmbracelet/log"
	"github.com/cli/go-gh/v2"
	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/joshsukhdeo/gh-pt/ai"
	"github.com/joshsukhdeo/gh-pt/config"
	"github.com/joshsukhdeo/gh-pt/heuristics"
	"github.com/joshsukhdeo/gh-pt/params"
	"github.com/joshsukhdeo/gh-pt/release"
	"github.com/joshsukhdeo/gh-pt/resolver"
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
)

func (r *RootCLI) Validate() error {
	r.ensureCliParams()
	if runtime.GOOS == "windows" && r.Wine != "off" && r.Wine != "" {
		pterm.Warning.Println("Wine is not supported on Windows. Continuing with wine disabled.")
		r.Wine = "off"
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

	if r.Clone || r.Fork || r.CompileFromSource || r.Show || r.ShowAssets > -1 || r.ShowVersions > -1 || r.ShowDescription > -1 || r.ShowReadme > -1 {
		return nil
	}

	// Detect root user and handle global install path
	if os.Getuid() == 0 {
		if r.Global {
			// Root + global: ensure we're using /usr/local/bin
			if r.TargetPath == GetDefaultTargetPath() {
				r.TargetPath = "/usr/local/bin"
			}
		} else if !r.AllowRootUserInstall {
			// Root without global flag and without explicit permission
			err := fmt.Errorf("running as root without --global flag. Use --global for system-wide install or --allow-root-user-install to install to user-local paths")
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
	if r.Barbarous {
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
	if r.LogQuietInteractive && r.Interactive {
		log.SetLevel(log.FatalLevel)
	}

	cfg := loadConfig()

	stdoutWrapper := ui.PacmanLogWriter{Writer: os.Stdout}

	if cfg != nil && cfg.Core.LogToFile {
		fileLogger := &lumberjack.Logger{
			Filename:   filepath.Join(xdg.DataHome, "gh-pt", "gh-pt.log"),
			MaxSize:    10,
			MaxBackups: 5,
			MaxAge:     30,
			Compress:   true,
		}
		log.SetOutput(io.MultiWriter(stdoutWrapper, fileLogger))
	} else {
		log.SetOutput(stdoutWrapper)
	}

	if cfg != nil {
		if r.VTApiKey == "" {
			r.VTApiKey = cfg.Core.VTApiKey
		}
		if cfg.Core.AllowPrerelease {
			r.Prerelease = true
		}
		r.DisableIcons = cfg.Core.DisableIcons
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

	ghClient, err := api.DefaultRESTClient()
	if err != nil {
		log.Error("could not init Gihub REST client", "error", err)
		return err
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
		return RemoveApp(r.Rm, false)
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
		return RemoveApp(r.Purge, true)
	}
	if r.Pin != "" {
		return PinAppState(r.Pin)
	}

	if r.Update || r.UpdateAll {
		return DoUpdate(r, ghClient)
	}

	if r.Overwrite {
		// If overwrite/force is used, attempt to purge any existing installation first
		_ = RemoveApp(r.Repository, true)
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
		"release asset binary names", r.AssetBinaries,
		"release asset binary name regexp", r.AssetBinariesRegexp,
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
	ext := ".sh"
	if runtime.GOOS == "windows" {
		ext = ".ps1"
	}

	var basePrompt string
	if symlinkDir != "" {
		basePrompt = fmt.Sprintf("Please inspect the repository '%s' (cloned at '%s') and generate an automated compilation/build script at '%s'. The script should follow all build instructions for '%s', compile and install the application/binaries into '%s' (this is the staging directory), then create symlink(s) in '%s' pointing to the executable(s) in '%s'. IMPORTANT: First install/stage everything in '%s', then symlink from there to '%s'. Purge any temporary build artifacts. Format the output as an executable %s script. Please test and then attempt to run the compile script and it is only done when script runs successfully.", repo, buildDir, scriptPath, repo, symlinkDir, targetPath, symlinkDir, symlinkDir, targetPath, ext)
	} else {
		basePrompt = fmt.Sprintf("Please inspect the repository '%s' (cloned at '%s') and generate an automated compilation/build script at '%s'. The script should follow all build instructions for '%s', compile the application/binaries, install or copy them to '%s', and purge any temporary build artifacts. Format the output as an executable %s script. Please test and then attempt to run the compile script and it is only done when script runs successfully.", repo, buildDir, scriptPath, repo, targetPath, ext)
	}

	instruction := fmt.Sprintf("\n\nCRITICAL: Output exactly two structured blocks (JSON manifest + Bash script) rather than a single monolithic script. Follow these templates:\n\nMANIFEST TEMPLATE (output as ```json code block):\n%s\n(Optional version requirement can be min, max, exact, or suggested)\n\nCOMPILE SCRIPT TEMPLATE (output as ```bash code block):\n%s\nTreat \"install-dir\" (.ghpt/install-dir) the same as /usr/local or $HOME/.local and place binaries into the same structure (install-dir/bin, install-dir/libs, install-dir/share, install-dir/state).\nAlternatively, binaries can be placed in '.ghpt/dist/' for automatic installation handoff.", ai.ManifestJSONTemplate, ai.CompileScriptTemplate)

	return basePrompt + instruction
}

func buildCompileFixPrompt(repo, buildDir, scriptPath, targetPath, symlinkDir, errorOutput string, attempt int) string {
	ext := ".sh"
	if runtime.GOOS == "windows" {
		ext = ".ps1"
	}

	var basePrompt string
	if symlinkDir != "" {
		basePrompt = fmt.Sprintf("The automated compilation script at '%s' for repository '%s' (cloned at '%s') failed to run with the following error output (attempt %d of 2):\n\n%s\n\nPlease fix the script at '%s' so that it successfully compiles and installs the application into '%s' (staging directory), then creates symlink(s) in '%s' pointing to the executable(s) in '%s'. REMEMBER: First stage everything in '%s', then symlink from there to '%s'. Format the output as an executable %s script. Please fix and then attempt to run the compile script and it is only done when script runs successfully.", scriptPath, repo, buildDir, attempt, errorOutput, scriptPath, symlinkDir, targetPath, symlinkDir, symlinkDir, targetPath, ext)
	} else {
		basePrompt = fmt.Sprintf("The automated compilation script at '%s' for repository '%s' (cloned at '%s') failed to run with the following error output (attempt %d of 2):\n\n%s\n\nPlease fix the script at '%s' so that it successfully compiles and installs the binaries into '%s'. Format the output as an executable %s script. Please fix and then attempt to run the compile script and it is only done when script runs successfully.", scriptPath, repo, buildDir, attempt, errorOutput, scriptPath, targetPath, ext)
	}

	instruction := fmt.Sprintf("\n\nCRITICAL: Output exactly two structured blocks (JSON manifest + Bash script) rather than a single monolithic script. Follow these templates:\n\nMANIFEST TEMPLATE (output as ```json code block):\n%s\n(Optional version requirement can be min, max, exact, or suggested)\n\nCOMPILE SCRIPT TEMPLATE (output as ```bash code block):\n%s\nTreat \"install-dir\" (.ghpt/install-dir) the same as /usr/local or $HOME/.local and place binaries into the same structure (install-dir/bin, install-dir/libs, install-dir/share, install-dir/state).\nAlternatively, binaries can be placed in '.ghpt/dist/' for automatic installation handoff.", ai.ManifestJSONTemplate, ai.CompileScriptTemplate)

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
		if r != nil && (r.IncludeSidecars != "" || (r.CliParams != nil && r.CliParams.IncludeSidecars != "")) {
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
	_ = filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && path != srcDir {
			_ = os.Remove(path)
		}
		return nil
	})
	_ = os.Remove(srcDir)

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
	err := os.RemoveAll(path)
	if err != nil {
		if runtime.GOOS != "windows" {
			_ = exec.Command("rm", "-rf", path).Run()
		} else {
			_ = exec.Command("cmd", "/C", "rmdir", "/s", "/q", path).Run()
		}
		return os.RemoveAll(path)
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

	var symlinkDir string
	if r.Symlink {
		var ownerID, repoID string
		if len(parts) >= 2 {
			ownerID = parts[0]
			repoID = parts[1]
		} else if len(parts) == 1 {
			ownerID = ""
			repoID = parts[0]
		}
		symlinkDir = filepath.Join(homeDir, "src", "apps", ownerID, repoID)
		if err := os.MkdirAll(symlinkDir, 0755); err != nil {
			return fmt.Errorf("failed to create symlink apps directory: %w", err)
		}
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

	if r.DryRun {
		if symlinkDir != "" {
			log.Info(fmt.Sprintf("[dry-run] Would clone %s to %s, generate build script at %s, compile/install to %s, and symlink to %s", r.Repository, repoDir, scriptPath, symlinkDir, targetPath))
		} else {
			log.Info(fmt.Sprintf("[dry-run] Would clone %s to %s, generate build script at %s, and execute compilation", r.Repository, repoDir, scriptPath))
		}
		return nil
	}

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

		prompt := buildCompilePrompt(r.Repository, repoDir, scriptPath, targetPath, symlinkDir)
		log.Info(fmt.Sprintf("Generating AI compilation script using: %s", aiCmdTemplate))
		aiResp, err := runAIAgentWithOutput(aiCmdTemplate, prompt, repoDir)
		if err != nil {
			return fmt.Errorf("AI agent failed to generate/test compilation script: %w", err)
		}

		p, parseErr := ai.ParseAIOutput(aiResp)
		if parseErr != nil {
			if scriptContent, readErr := os.ReadFile(scriptPath); readErr == nil && len(scriptContent) > 0 {
				p, parseErr = ai.ParseAIOutput(string(scriptContent))
			}
		}
		if parseErr != nil {
			return fmt.Errorf("failed to parse AI output: %w", parseErr)
		}
		payload = p

		manifestContent := payload.ManifestJSON
		if manifestContent == "" {
			manifestContent = ai.ManifestJSONTemplate
		}
		_ = os.WriteFile(manifestPath, []byte(manifestContent), 0644)

		if err := os.WriteFile(scriptPath, []byte(payload.Script), 0755); err != nil {
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

	for attempt := 0; attempt <= maxRetries; attempt++ {
		log.Info("executing compile script", "script", scriptPath, "attempt", attempt+1)
		var execScriptCmd *exec.Cmd
		if runtime.GOOS == "windows" {
			execScriptCmd = exec.Command("powershell", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
		} else {
			if _, err := exec.LookPath("bash"); err == nil {
				execScriptCmd = exec.Command("bash", scriptPath)
			} else {
				execScriptCmd = exec.Command("sh", scriptPath)
			}
		}
		execScriptCmd.Dir = repoDir

		outputBytes, runErr := execScriptCmd.CombinedOutput()
		if len(outputBytes) > 0 {
			_, _ = os.Stdout.Write(outputBytes)
		}

		if runErr == nil {
			lastErr = nil
			break
		}

		lastErr = runErr
		lastOutput = string(outputBytes)
		log.Warn("compile script failed", "error", runErr, "attempt", attempt+1)

		if attempt < maxRetries {
			log.Info(fmt.Sprintf("Prompting AI to fix compile script (retry %d of %d)...", attempt+1, maxRetries))
			fixPrompt := buildCompileFixPrompt(r.Repository, repoDir, scriptPath, targetPath, symlinkDir, lastOutput, attempt+1)
			fixResp, fixErr := runAIAgentWithOutput(aiCmdTemplate, fixPrompt, repoDir)
			if fixErr != nil {
				log.Warn("AI repair command execution failed", "error", fixErr)
			}
			fixPayload, pErr := ai.ParseAIOutput(fixResp)
			if pErr != nil {
				if data, rErr := os.ReadFile(scriptPath); rErr == nil {
					fixPayload, _ = ai.ParseAIOutput(string(data))
				}
			}
			if fixPayload != nil {
				if len(fixPayload.Dependencies) > 0 {
					_, _ = r.resolveCompileDependencies(fixPayload.Dependencies, repoDir, cfg)
				}
				if fixPayload.ManifestJSON != "" {
					_ = os.WriteFile(manifestPath, []byte(fixPayload.ManifestJSON), 0644)
				}
				_ = os.WriteFile(scriptPath, []byte(fixPayload.Script), 0755)
			}
			_ = os.Chmod(scriptPath, 0755)
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

	commitHash := "unknown"
	revParseCmd := exec.Command("git", "rev-parse", "HEAD")
	revParseCmd.Dir = repoDir
	if out, err := revParseCmd.Output(); err == nil {
		commitHash = strings.TrimSpace(string(out))
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
				Version:           commitHash,
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

	indicator := GetStateIndicator(true, r.PinInstall, false, false, false, r.DisableIcons)
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
