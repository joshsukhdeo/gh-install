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
	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/joshsukhdeo/gh-install/config"
	"github.com/joshsukhdeo/gh-install/params"
	"github.com/joshsukhdeo/gh-install/release"
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

	if r.AssetBinariesRegexp == "" {
		r.AssetBinariesRegexp = fmt.Sprintf("^%s$", strings.Split(r.Repository, "/")[1])
	}

	if r.ReleaseAssetRegexp == "" {
		r.ReleaseAssetRegexps = buildRegexFromTypes(r.Type, r.AllowWine)
		r.ReleaseAssetRegexp = strings.Join(r.ReleaseAssetRegexps, " | ")
	} else {
		r.ReleaseAssetRegexps = []string{r.ReleaseAssetRegexp}
	}

	log.Info().
		Str("repository", r.Repository).
		Str("release version", r.ReleaseVersion).
		Str("release asset name", r.ReleaseAsset).
		Strs("release asset regexps", r.ReleaseAssetRegexps).
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

func GetDefaultTargetPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(homeDir, ".local", "bin")
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

	var matchers []string

	hwSpecific := ""
	if runtime.GOOS == "linux" {
		if _, err := os.Stat("/sys/class/accel"); err == nil {
			hwSpecific = "npu"
		} else if _, err := os.Stat("/dev/dri"); err == nil {
			hwSpecific = "(?:gpu|cuda|rocm)"
		}
	}

	buildFinal := func(baseRegex string, currentTypes []string) string {
		var exts []string
		hasNone := false
		for _, t := range currentTypes {
			t = strings.ToLower(strings.TrimSpace(t))
			if t == "none" {
				hasNone = true
			} else if t != "" {
				exts = append(exts, t)
			}
		}
		if len(exts) > 0 {
			extPattern := strings.Join(exts, "|")
			if hasNone {
				return fmt.Sprintf(`^(?:%s|%s\.(?i:%s))$`, baseRegex, baseRegex, extPattern)
			}
			return fmt.Sprintf(`^%s\.(?i:%s)$`, baseRegex, extPattern)
		}
		return fmt.Sprintf(`^%s$`, baseRegex)
	}

	for _, osRegex := range osRegexList {
		if hwSpecific != "" {
			hwBaseRegex := fmt.Sprintf(`.*(?:%s.+%s.+%s|%s.+%s.+%s|%s.+%s|%s.+%s).*`, archRegex, osRegex, hwSpecific, osRegex, archRegex, hwSpecific, hwSpecific, archRegex, archRegex, hwSpecific)
			matchers = append(matchers, buildFinal(hwBaseRegex, types))
		}

		baseRegex := fmt.Sprintf(`.*(?:%s.+%s|%s.+%s).*`, archRegex, osRegex, osRegex, archRegex)
		matchers = append(matchers, buildFinal(baseRegex, types))

		fallbackRegex := fmt.Sprintf(`.*%s.*`, osRegex)
		matchers = append(matchers, buildFinal(fallbackRegex, types))
	}

	if allowWine && runtime.GOOS != "windows" {
		winOsRegex := "(?:windows|win)"
		winBaseRegex := fmt.Sprintf(`.*(?:%s.+%s|%s.+%s).*`, archRegex, winOsRegex, winOsRegex, archRegex)

		// Ensure windows extensions are checked
		winTypes := append([]string{"exe", "msi"}, types...)
		matchers = append(matchers, buildFinal(winBaseRegex, winTypes))
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
