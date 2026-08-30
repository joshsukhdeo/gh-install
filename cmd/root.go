package cmd

import (
	"errors"
	"fmt"
	"os"
	"path"
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
	if !r.Update && !r.UpdateAll && !r.SetupTopgradeStep {
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
		if envDeps == "TRUE" {
			r.AddDeps = true
		} else if envDeps == "FALSE" {
			r.NoDeps = true
		} else {
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
			}
		}
	}

	if r.Global && r.TargetPath == GetDefaultTargetPath() {
		if runtime.GOOS == "windows" {
			r.TargetPath = os.Getenv("ProgramFiles")
			if r.TargetPath == "" {
				r.TargetPath = "C:\\Program Files"
			}
		} else {
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

	if r.SetupTopgradeStep {
		return SetupTopgrade()
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
		r.ReleaseAssetRegexps = buildRegexFromTypes(r.Type)
		r.ReleaseAssetRegexp = strings.Join(r.ReleaseAssetRegexps, " | ")
	} else {
		r.ReleaseAssetRegexps = []string{r.ReleaseAssetRegexp}
	}

	log.Info().
		Str("repository", r.Repository).
		Str("release version", r.ReleaseVersion).
		Str("release asset name", r.ReleaseAsset).
		Strs("release asset regexps", r.ReleaseAssetRegexps).
		Array("release asset binary names", func() *zerolog.Array {
			arr := zerolog.Arr()
			for _, i := range r.AssetBinaries {
				arr = arr.Str(i)
			}
			return arr
		}()).
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

	return path.Join(homeDir, ".local", "bin")
}

func getTarballRgx() string {
	val := os.Getenv("TARBALL_RGX")
	if val != "" {
		return val
	}
	return `t(ar\.)?([gxl]z|bz2?|zst),tar(\.lzma)?`
}

func GetDefaultInstallTypes() string {
	tarballRgx := getTarballRgx()
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("exe,msi,7z,%s,zip,py,ts,js", tarballRgx)
	} else if runtime.GOOS == "darwin" {
		return fmt.Sprintf("dmg,7z,%s,zip,py,ts,js,none", tarballRgx)
	} else if runtime.GOOS == "linux" {
		osRelease, err := os.ReadFile("/etc/os-release")
		if err == nil {
			content := strings.ToLower(string(osRelease))
			if strings.Contains(content, "id=ubuntu") || strings.Contains(content, "id=debian") {
				return fmt.Sprintf("deb,snap,flatpak,appimage,7z,%s,zip,py,ts,js,none", tarballRgx)
			} else if strings.Contains(content, "id=fedora") || strings.Contains(content, "id=rhel") || strings.Contains(content, "id=centos") {
				return fmt.Sprintf("rpm,snap,flatpak,appimage,7z,%s,zip,py,ts,js,none", tarballRgx)
			}
		}
		return fmt.Sprintf("deb,rpm,snap,flatpak,appimage,7z,%s,zip,py,ts,js,none", tarballRgx)
	}
	return fmt.Sprintf("7z,%s,zip,none", tarballRgx)
}

func buildRegexFromTypes(types []string) []string {
	archRegex := runtime.GOARCH
	if runtime.GOARCH == "amd64" {
		archRegex = "(?:amd64|x86_64|x64)"
	} else if runtime.GOARCH == "arm64" {
		archRegex = "(?:arm64|aarch64)"
	}

	osRegex := runtime.GOOS
	if runtime.GOOS == "darwin" {
		osRegex = "(?:darwin|macos|apple)"
	} else if runtime.GOOS == "windows" {
		osRegex = "(?:windows|win)"
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

	buildFinal := func(baseRegex string) string {
		var exts []string
		hasNone := false
		for _, t := range types {
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
				return fmt.Sprintf(`^(?:%s|.*%s.*\.(?i:%s))$`, baseRegex, archRegex, extPattern)
			}
			return fmt.Sprintf(`^.*%s.*\.(?i:%s)$`, archRegex, extPattern)
		}
		return fmt.Sprintf(`^%s$`, baseRegex)
	}

	if hwSpecific != "" {
		hwBaseRegex := fmt.Sprintf(`.*(?:%s.+%s.+%s|%s.+%s.+%s|%s.+%s|%s.+%s).*`, archRegex, osRegex, hwSpecific, osRegex, archRegex, hwSpecific, hwSpecific, archRegex, archRegex, hwSpecific)
		matchers = append(matchers, buildFinal(hwBaseRegex))
	}

	baseRegex := fmt.Sprintf(`.*(?:%s.+%s|%s.+%s).*`, archRegex, osRegex, osRegex, archRegex)
	matchers = append(matchers, buildFinal(baseRegex))

	return matchers
}

func GetEnvPrefix() string {
	envPrefix := os.Getenv(GH_INSTALL_PREFIX_ENV)
	if envPrefix == "" {
		envPrefix = GH_INSTALL_DEFAULT_PREFIX
	}

	return strings.ToUpper(envPrefix)
}
