package release

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/cli/go-gh/v2"
	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/joshsukhdeo/gh-install/params"
	"github.com/joshsukhdeo/gh-install/selector"
	"github.com/joshsukhdeo/gh-install/state"
	"github.com/pterm/pterm"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type IRelease interface {
	Install() error
}

type GithubRelease struct {
	CliParams *params.CLI
	Client    *api.RESTClient
}

func MakeGithubRelease(cliParams *params.CLI, cli *api.RESTClient) IRelease {

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
	binaryName := path.Base(binaryPath)
	destinationPath := path.Join(r.CliParams.TargetPath, binaryName)

	if targetBinaryName, exists := r.CliParams.Rename[strings.ToLower(binaryName)]; exists {
		return path.Join(r.CliParams.TargetPath, targetBinaryName)
	}

	if r.CliParams.PromptRename && !r.CliParams.DisablePrompts {
		repoParts := strings.Split(r.CliParams.Repository, "/")
		repoName := repoParts[len(repoParts)-1]

		proposedName := repoName
		if !strings.HasPrefix(strings.ToLower(binaryName), strings.ToLower(repoName)) {
			proposedName = binaryName
		}
		if runtime.GOOS == "windows" && !strings.HasSuffix(proposedName, ".exe") {
			proposedName += ".exe"
		}

		if len(binaryName) > len(proposedName)+3 { // If it has significant affixes
			if r.interactiveConfirm(fmt.Sprintf("Binary name '%s' is long. Do you want to strip OS/hardware affixes?", binaryName)) {
				newName := r.interactiveInput(fmt.Sprintf("Rename '%s' to", binaryName), proposedName)
				if newName != "" {
					return path.Join(r.CliParams.TargetPath, newName)
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

func (r *GithubRelease) installDeb(binaryPath string) error {
	var args []string
	if r.CliParams.NoDeps {
		args = []string{"dpkg", "-i", binaryPath}
	} else if r.CliParams.AddDeps {
		args = []string{"apt-get", "install", "-y", binaryPath}
	} else {
		args = []string{"apt-get", "install", binaryPath}
	}

	if r.CliParams.Interactive {
		if !r.interactiveConfirm(fmt.Sprintf("Run 'sudo %s'?", strings.Join(args, " "))) {
			return fmt.Errorf("'%s' is a DEB installer and user did not want to run it", binaryPath)
		}
	}

	cmd := exec.Command("sudo", args...)
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
			pterm.Error.Println("Failed to install .deb package")
		}
		return err
	}
	log.Info().
		Str("installer binary", binaryPath).
		Msgf("ran 'sudo %s'", strings.Join(args, " "))
	if r.CliParams.Interactive {
		pterm.Success.Println("Successfully installed .deb package!")
	}
	return nil
}

func (r *GithubRelease) installRpm(binaryPath string) error {
	var args []string
	if r.CliParams.NoDeps {
		args = []string{"rpm", "-i", binaryPath}
	} else if r.CliParams.AddDeps {
		args = []string{"dnf", "localinstall", "-y", binaryPath}
	} else {
		args = []string{"dnf", "localinstall", binaryPath}
	}

	if r.CliParams.Interactive {
		if !r.interactiveConfirm(fmt.Sprintf("Run 'sudo %s'?", strings.Join(args, " "))) {
			return fmt.Errorf("'%s' is a RPM installer and user did not want to run it", binaryPath)
		}
	}

	cmd := exec.Command("sudo", args...)
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
			pterm.Error.Println("Failed to install .rpm package")
		}
		return err
	}
	log.Info().
		Str("installer binary", binaryPath).
		Msgf("ran 'sudo %s'", strings.Join(args, " "))
	if r.CliParams.Interactive {
		pterm.Success.Println("Successfully installed .rpm package!")
	}
	return nil
}

func getScore(name string, types []string) int {
	name = strings.ToLower(name)
	for i, t := range types {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "none" {
			if !strings.Contains(path.Base(name), ".") {
				return len(types) - i
			}
		} else if t != "" {
			matched, _ := regexp.MatchString(`(?i)\.`+t+`$`, name)
			if matched {
				return len(types) - i
			}
		}
	}

	for i, t := range types {
		if strings.ToLower(strings.TrimSpace(t)) == "none" {
			return len(types) - i
		}
	}

	return -1
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

	assetSelector, err := selector.AssetSelector(r.Client, r.CliParams.Repository, releases[0].GetId(),
		r.CliParams.ReleaseAsset, r.CliParams.ReleaseAssetRegexps, r.CliParams.Interactive)
	if err != nil {
		log.Error().
			Str("repository", r.CliParams.Repository).
			Int("release id", releases[0].GetId()).
			Str("release name", releases[0].Name).
			Str("asset name matcher", r.CliParams.ReleaseAsset).
			Strs("asset regexps matcher", r.CliParams.ReleaseAssetRegexps).
			Err(err).
			Msg("could not create release asset selector")
		return err
	}
	assets, err := assetSelector.Run()
	if err != nil {
		log.Error().
			Str("repository", r.CliParams.Repository).
			Int("release id", releases[0].GetId()).
			Str("release asset name matcher", r.CliParams.ReleaseAsset).
			Strs("release asset regexps matcher", r.CliParams.ReleaseAssetRegexps).
			Err(err).
			Msg("could not select release asset")
		return err
	}

	sort.Slice(assets, func(i, j int) bool {
		scoreI := getScore(assets[i].Name, r.CliParams.Type)
		scoreJ := getScore(assets[j].Name, r.CliParams.Type)
		return scoreI > scoreJ
	})

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

	for _, asset := range assets {
		var downloadSpinner *pterm.SpinnerPrinter
		if r.CliParams.Interactive {
			downloadSpinner, _ = pterm.DefaultSpinner.Start(fmt.Sprintf("Downloading asset '%s'...", asset.Name))
		}

		stdOut, stdErr, execErr := gh.Exec("release", "download", releases[0].Name,
			"--repo", r.CliParams.Repository, "--pattern", asset.Name, "--dir", downloadDir)
		if execErr != nil {
			execErr = fmt.Errorf("failed to run gh command: %s", stdErr.String())
			log.Error().
				Str("repository", r.CliParams.Repository).
				Int("release id", releases[0].GetId()).
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
			Int("release id", releases[0].GetId()).
			Str("release name", releases[0].Name).
			Str("release asset name", asset.Name).
			Str("download directory", downloadDir).
			Str("output", stdOut.String()).
			Msg("downloaded release asset")

		binarySelector, execErr := selector.BinarySelector(path.Join(downloadDir,
			asset.Name), r.CliParams.AssetBinaries, r.CliParams.AssetBinariesRegexp, r.CliParams.Interactive)
		if execErr != nil {
			log.Error().
				Str("repository", r.CliParams.Repository).
				Int("release id", releases[0].GetId()).
				Str("release name", releases[0].Name).
				Str("release asset name", asset.Name).
				Str("downloaded asset", path.Join(downloadDir, asset.Name)).
				Array("asset binary name matchers", func() *zerolog.Array {
					arr := zerolog.Arr()
					for _, i := range r.CliParams.AssetBinaries {
						arr = arr.Str(i)
					}
					return arr
				}()).
				Str("asset binary regexp matcher", r.CliParams.AssetBinariesRegexp).
				Err(execErr).
				Msg("could not create release asset binary selector")
			return execErr
		}
		binaries, execErr := binarySelector.Run()
		if execErr != nil {
			log.Error().
				Str("repository", r.CliParams.Repository).
				Int("release id", releases[0].GetId()).
				Str("release name", releases[0].Name).
				Str("release asset name", asset.Name).
				Str("downloaded asset", path.Join(downloadDir, asset.Name)).
				Array("asset binary name matchers", func() *zerolog.Array {
					arr := zerolog.Arr()
					for _, i := range r.CliParams.AssetBinaries {
						arr = arr.Str(i)
					}
					return arr
				}()).
				Str("asset binary regexp matcher", r.CliParams.AssetBinariesRegexp).
				Err(execErr).
				Msg("could not select release asset binary")
			return execErr
		}

		binariesOutput := make(map[string]string)
		for _, binary := range binaries {
			log.Info().
				Str("repository", r.CliParams.Repository).
				Int("release id", releases[0].GetId()).
				Str("release name", releases[0].Name).
				Str("release asset name", asset.Name).
				Str("downloaded asset", path.Join(downloadDir, asset.Name)).
				Str("release asset binary", binary.Name).
				Msg("processing selected release asset binary")
			if binary.GetCompressed() {
				log.Debug().
					Str("release asset binary", binary.Name).
					Str("release asset binary archive path", binary.GetFsPath()).
					Msg("binary is part of an archive")
				binariesOutput[binary.Name] = "compressed"
				execErr = r.installArchivedBinary(binary.GetFs(), binary.GetFsPath())
			} else {
				switch binary.GetBinaryType() {
				case selector.BinaryDebInstaller:
					log.Debug().
						Str("release asset binary", binary.Name).
						Msg("binary is a deb installer")
					binariesOutput[binary.Name] = "deb"
					execErr = r.installDeb(binary.GetDownloadPath())
				case selector.BinaryRpmInstaller:
					log.Debug().
						Str("release asset binary", binary.Name).
						Msg("binary is a rpm installer")
					binariesOutput[binary.Name] = "rpm"
					execErr = r.installRpm(binary.GetDownloadPath())
				default:
					log.Debug().
						Str("release asset binary", binary.Name).
						Msg("binary is a plain executable")
					binariesOutput[binary.Name] = "binary"
					execErr = r.installBinary(binary.GetDownloadPath())
				}
			}
			if execErr != nil {
				log.Error().
					Str("repository", r.CliParams.Repository).
					Int("release id", releases[0].GetId()).
					Str("release name", releases[0].Name).
					Str("release asset name", asset.Name).
					Str("downloaded asset", path.Join(downloadDir, asset.Name)).
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

	if !r.CliParams.NoSaveState {
		st, err := state.LoadState()
		if err == nil {
			st.AddApp(&state.InstalledApp{
				Repository:    r.CliParams.Repository,
				TargetPath:    r.CliParams.TargetPath,
				Global:        r.CliParams.Global,
				ReleaseAsset:  r.CliParams.ReleaseAsset,
				ReleaseRegexp: r.CliParams.ReleaseAssetRegexp,
				Version:       releases[0].Name,
				Rename:        r.CliParams.Rename,
			})
		} else {
			log.Warn().Err(err).Msg("could not save installed app state")
		}
	}

	return nil
}
