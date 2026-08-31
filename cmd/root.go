package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/cli/go-gh/v2"
	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/joshsukhdeo/gh-install/config"
	"github.com/joshsukhdeo/gh-install/params"
	"github.com/joshsukhdeo/gh-install/release"
	"github.com/joshsukhdeo/gh-install/state"
	"github.com/pterm/pterm"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	GH_INSTALL_PREFIX_ENV           = "GH_INSTALL_ENV_PREFIX"
	GH_INSTALL_DEFAULT_PREFIX       = "GH_INSTALL"
	GH_INSTALL_CHECKSUM_ASSET_REGEX = ".*(?:checksum|txt)+.*$"
)

type RootCLI params.CLI

func (r *RootCLI) Validate() error {
	if !r.Update && !r.UpdateAll && !r.ListSavedState && !r.EditSavedState && r.RmSavedState == "" {
		match, _ := regexp.MatchString(`.+/.+`, r.Repository)
		if !match {
			return fmt.Errorf("repository must be in 'user/repository' format (provided: '%s')", r.Repository)
		}
	}

	if r.CompileFromSource && !r.AI {
		return fmt.Errorf("--compile-from-source can only be used with --ai")
	}

	if r.Clone || r.Fork || r.CompileFromSource {
		return nil
	}

	if r.TargetPath == "" {
		err := fmt.Errorf("could not determine default install path, use '--install-path' flag")
		log.Error().
			Err(err).
			Msg("init error")
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
					log.Error().
						Err(err).
						Msgf("target installation path '%s' error", r.TargetPath)
					return err
				}
				return nil
			} else {
				log.Error().
					Err(err).
					Msgf("target installation path '%s' error", r.TargetPath)
				return err
			}

		}
		log.Error().
			Err(err).
			Msgf("target installation path '%s' error", r.TargetPath)
		return err
	}

	if !targetPathInfo.Mode().IsDir() {
		err = errors.New("not a directory")
		log.Error().
			Err(err).
			Msgf("target installation path '%s' error", r.TargetPath)
	}

	return nil
}

func PostBuild(k *kong.Kong) error {
	k.Model.Positional[0].Tag.Envs = []string{fmt.Sprintf("%s_REPOSITORY", GetEnvPrefix())}
	return nil
}

func (r *RootCLI) Run() error {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	logLevel, _ := zerolog.ParseLevel(r.LogLevel)
	if r.Verbose {
		logLevel = zerolog.DebugLevel
	}
	if r.LogQuietInteractive && r.Interactive {
		logLevel = zerolog.Disabled
	}
	zerolog.SetGlobalLevel(logLevel)
	if r.LogFormat == "console" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})
	}

	if r.AddDeps && r.NoDeps {
		r.AddDeps = false
		r.NoDeps = false
	} else if !r.AddDeps && !r.NoDeps {
		envDeps := strings.ToUpper(os.Getenv("GH_INSTALL_ADD_DEPS"))
		switch envDeps {
		case "TRUE":
			r.AddDeps = true
		case "FALSE":
			r.NoDeps = true
		default:
			cfg, _ := config.LoadConfig()
			if cfg != nil {
				r.AddDeps = cfg.AddDeps
				r.NoDeps = cfg.NoDeps
				if !r.PromptRename {
					r.PromptRename = cfg.PromptRename
				}
				if !r.DisablePrompts {
					r.DisablePrompts = cfg.DisablePrompts
				}
				if !r.NoSaveState {
					r.NoSaveState = cfg.NoSaveState
				}
				if !r.AllowWine {
					r.AllowWine = cfg.AllowWine
				}
				if !r.NativeExtract {
					r.NativeExtract = cfg.NativeExtract
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
		log.Error().
			Err(err).
			Msg("could not init Gihub REST client")
		return err
	}

	if r.ListSavedState {
		return ListState()
	}
	if r.EditSavedState {
		return EditState()
	}
	if r.RmSavedState != "" {
		return RmState(r.RmSavedState)
	}

	if r.Update || r.UpdateAll {
		return DoUpdate(r, ghClient)
	}

	if r.Repository == "" {
		return fmt.Errorf("repository argument is required for installation")
	}

	if r.Clone || r.Fork {
		cfg, _ := config.LoadConfig()
		return r.handleRepoCloneOrFork(cfg)
	}

	if r.CompileFromSource {
		cfg, _ := config.LoadConfig()
		return r.handleCompileFromSource(cfg)
	}

	if r.AssetBinariesRegexp == "" {
		r.AssetBinariesRegexp = fmt.Sprintf("^%s$", strings.Split(r.Repository, "/")[1])
	}

	if r.ReleaseAssetRegexp == "" {
		r.ReleaseAssetRegexps = buildRegexFromTypes(r.Type, r.AllowWine)
		r.ReleaseAssetRegexp = strings.Join(r.ReleaseAssetRegexps, " | ")
	} else {
		r.ReleaseAssetRegexps = []string{r.ReleaseAssetRegexp}
	}

	log.Debug().
		Str("repository", r.Repository).
		Str("release version", r.ReleaseVersion).
		Str("release asset name", r.ReleaseAsset).
		Strs("release asset binary names", r.AssetBinaries).
		Str("release asset binary name regexp", r.AssetBinariesRegexp).
		Str("target path", r.TargetPath).
		Dict("renaming binaries", func() *zerolog.Event {
			d := zerolog.Dict()
			for k, v := range r.Rename {
				d = d.Str(k, v)
			}
			return d
		}()).
		Msg("installing with values")

	response := struct{ Name string }{}
	err = ghClient.Get(fmt.Sprintf("repos/%s", r.Repository), &response)
	if err != nil {
		log.Error().
			Err(err).
			Msgf("repository %s doesn't exist", r.Repository)
	}

	installRelease := release.MakeGithubRelease(
		(*params.CLI)(r),
		ghClient)
	return installRelease.Install()
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

func (r *RootCLI) handleRepoCloneOrFork(cfg *config.Config) error {
	var cloneBase string
	var forkBase string
	if cfg != nil {
		cloneBase = cfg.ClonePath
		forkBase = cfg.ForkPath
	}

	targetDir := resolveRepoPath(r.Repository, r.Clone, r.Fork, cloneBase, forkBase)
	if r.TargetPath != "" && r.TargetPath != GetDefaultTargetPath() {
		targetDir = r.TargetPath
	}

	log.Info().
		Str("repository", r.Repository).
		Str("target_directory", targetDir).
		Bool("clone", r.Clone).
		Bool("fork", r.Fork).
		Msg("handling repository clone/fork")

	if r.DryRun {
		if r.Fork {
			log.Info().Msgf("[dry-run] Would fork and clone %s to %s", r.Repository, targetDir)
		} else {
			log.Info().Msgf("[dry-run] Would clone %s to %s", r.Repository, targetDir)
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(targetDir), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	var args []string
	if r.Fork {
		args = []string{"repo", "fork", r.Repository, "--clone", "--", targetDir}
	} else {
		args = []string{"repo", "clone", r.Repository, targetDir}
	}

	stdOut, stdErr, err := gh.Exec(args...)
	if err != nil {
		return fmt.Errorf("failed to execute gh %s: %s (%w)", strings.Join(args, " "), stdErr.String(), err)
	}
	log.Info().Str("output", stdOut.String()).Msg("repository cloned successfully")

	if !r.NoSaveState {
		st, err := state.LoadState()
		if err == nil {
			err = st.AddApp(&state.InstalledApp{
				Repository: r.Repository,
				TargetPath: targetDir,
				Global:     r.Global,
				Clone:      r.Clone,
				Fork:       r.Fork,
				Pinned:     r.Pin,
			})
			if err != nil {
				log.Warn().Err(err).Msg("could not save repository state")
			} else {
				log.Info().Msgf("Saved %s to state tracking.", r.Repository)
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

func buildCompilePrompt(repo, buildDir, scriptPath, targetPath string) string {
	ext := ".sh"
	if runtime.GOOS == "windows" {
		ext = ".ps1"
	}

	return fmt.Sprintf("Please inspect the repository '%s' (cloned at '%s') and generate an automated compilation/build script at '%s'. The script should follow all build instructions for '%s', compile the application/binaries, install or copy them to '%s', and purge any temporary build artifacts. Format the output as an executable %s script. Please test and then attempt to run the compile script and it is only done when script runs successfully.", repo, buildDir, scriptPath, repo, targetPath, ext)
}

func buildCompileFixPrompt(repo, buildDir, scriptPath, targetPath, errorOutput string, attempt int) string {
	ext := ".sh"
	if runtime.GOOS == "windows" {
		ext = ".ps1"
	}

	return fmt.Sprintf("The automated compilation script at '%s' for repository '%s' (cloned at '%s') failed to run with the following error output (attempt %d of 2):\n\n%s\n\nPlease fix the script at '%s' so that it successfully compiles and installs the binaries into '%s'. Format the output as an executable %s script. Please fix and then attempt to run the compile script and it is only done when script runs successfully.", scriptPath, repo, buildDir, attempt, errorOutput, scriptPath, targetPath, ext)
}

func runAIAgent(aiCmdTemplate, prompt, dir string) error {
	var cmd *exec.Cmd
	if strings.Contains(aiCmdTemplate, "%s") {
		formattedCmd := fmt.Sprintf(aiCmdTemplate, prompt)
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/C", formattedCmd)
		} else {
			cmd = exec.Command("sh", "-c", formattedCmd)
		}
	} else {
		cmd = exec.Command(aiCmdTemplate, prompt)
	}
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (r *RootCLI) handleCompileFromSource(cfg *config.Config) error {
	scriptPath := getCompileScriptPath(r.Repository)
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

	if err := os.MkdirAll(filepath.Dir(repoDir), 0755); err != nil {
		return fmt.Errorf("failed to create builds directory: %w", err)
	}

	// Ensure fresh clone
	_ = os.RemoveAll(repoDir)

	log.Info().
		Str("repository", r.Repository).
		Str("build_dir", repoDir).
		Str("script_path", scriptPath).
		Str("target_path", targetPath).
		Msg("handling compile-from-source with AI")

	if r.DryRun {
		log.Info().Msgf("[dry-run] Would clone %s to %s, generate build script at %s, and execute compilation", r.Repository, repoDir, scriptPath)
		return nil
	}

	// 1. Clone repo into builds directory
	cloneArgs := []string{"repo", "clone", r.Repository, repoDir}
	stdOut, stdErr, err := gh.Exec(cloneArgs...)
	if err != nil {
		return fmt.Errorf("failed to clone repository to builds dir: %s (%w)", stdErr.String(), err)
	}
	log.Info().Str("output", stdOut.String()).Msg("cloned repository to builds directory")

	// 2. Ensure scripts directory exists
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0755); err != nil {
		return fmt.Errorf("failed to create scripts directory: %w", err)
	}

	// 3. Resolve AI command template
	aiCmdTemplate := r.AICmd
	if cfg != nil && cfg.AICmd != "" && (r.AICmd == "" || r.AICmd == `agy -p "%s"`) {
		aiCmdTemplate = cfg.AICmd
	}
	if aiCmdTemplate == "" {
		aiCmdTemplate = `agy -p "%s"`
	}

	prompt := buildCompilePrompt(r.Repository, repoDir, scriptPath, targetPath)
	log.Info().Msgf("Generating AI compilation script using: %s", aiCmdTemplate)
	if err := runAIAgent(aiCmdTemplate, prompt, repoDir); err != nil {
		return fmt.Errorf("AI agent failed to generate/test compilation script: %w", err)
	}

	if _, err := os.Stat(scriptPath); err != nil {
		return fmt.Errorf("expected compile script '%s' was not created: %w", scriptPath, err)
	}
	_ = os.Chmod(scriptPath, 0755)

	// 4. Run the generated compile script with up to 2 retry fix loops
	maxRetries := 2
	var lastErr error
	var lastOutput string

	for attempt := 0; attempt <= maxRetries; attempt++ {
		log.Info().Str("script", scriptPath).Int("attempt", attempt+1).Msg("executing generated compile script")
		var execScriptCmd *exec.Cmd
		if runtime.GOOS == "windows" {
			execScriptCmd = exec.Command("powershell", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
		} else {
			execScriptCmd = exec.Command("sh", scriptPath)
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
		log.Warn().Err(runErr).Int("attempt", attempt+1).Msg("compile script failed")

		if attempt < maxRetries {
			log.Info().Msgf("Prompting AI to fix compile script (retry %d of %d)...", attempt+1, maxRetries)
			fixPrompt := buildCompileFixPrompt(r.Repository, repoDir, scriptPath, targetPath, lastOutput, attempt+1)
			if fixErr := runAIAgent(aiCmdTemplate, fixPrompt, repoDir); fixErr != nil {
				log.Warn().Err(fixErr).Msg("AI repair command execution failed")
			}
			_ = os.Chmod(scriptPath, 0755)
		}
	}

	if lastErr != nil {
		return fmt.Errorf("compile script execution failed after %d retries: %w (output: %s)", maxRetries, lastErr, lastOutput)
	}

	log.Info().Msgf("Successfully compiled and installed %s from source!", r.Repository)

	// 5. Save compileScript to state
	if !r.NoSaveState {
		st, err := state.LoadState()
		if err == nil {
			err = st.AddApp(&state.InstalledApp{
				Repository:    r.Repository,
				TargetPath:    targetPath,
				Global:        r.Global,
				CompileScript: scriptPath,
				Pinned:        r.Pin,
			})
			if err != nil {
				log.Warn().Err(err).Msg("could not save repository state")
			} else {
				log.Info().Msgf("Saved %s with compileScript to state tracking.", r.Repository)
			}
		}
	}

	return nil
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

	return filepath.Join(homeDir, "src")
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
func buildRegexFromTypes(types []string, allowWine bool) []string {
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
		} else if _, err := os.Stat("/dev/dri"); err == nil {
			hwSpecific = "(?:gpu|cuda|rocm)"
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

		// Fallback: OS only
		for _, osRegex := range osRegexList {
			fallbackRegex := fmt.Sprintf(`.*%s.*`, osRegex)
			matchers = append(matchers, buildFinal(fallbackRegex, t))
		}
	}

	if allowWine && runtime.GOOS != "windows" {
		winOsRegex := "(?:windows|win)"
		winBaseRegex := fmt.Sprintf(`.*(?:%s.+%s|%s.+%s).*`, archRegex, winOsRegex, winOsRegex, archRegex)
		for _, t := range append([]string{"exe", "msi"}, types...) {
			matchers = append(matchers, buildFinal(winBaseRegex, t))
		}
	}

	return matchers
}

func GetEnvPrefix() string {
	envPrefix := os.Getenv(GH_INSTALL_PREFIX_ENV)
	if envPrefix == "" {
		envPrefix = GH_INSTALL_DEFAULT_PREFIX
	}

	return strings.ToUpper(envPrefix)
}
