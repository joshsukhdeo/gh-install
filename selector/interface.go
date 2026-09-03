package selector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/mholt/archiver/v4"
	"github.com/rs/zerolog/log"
)

type SelectorKind int

type GithubClient interface {
	Get(path string, response interface{}) error
	Request(method string, path string, body io.Reader) (*http.Response, error)
}

const (
	Release SelectorKind = iota
	Asset
	Binary
)

func (ss SelectorKind) String() string {
	switch ss {
	case Release:
		return "release_selector"
	case Asset:
		return "asset_selector"
	case Binary:
		return "binary_selector"
	default:
		return fmt.Sprintf("Unknown(%d)", ss)
	}
}

type ISelector interface {
	GetKind() SelectorKind
	Run() ([]*SelectorItem, error)
}

func ReleaseSelector(ghClient GithubClient, repo string, version string, interactive bool) (ISelector, error) {
	log.Info().
		Str("repository", repo).
		Msg("getting Github repository releases")

	response := []struct {
		Tag_name string
		Id       int
	}{}
	err := ghClient.Get(fmt.Sprintf("repos/%s/releases", repo), &response)
	if err != nil {
		return nil, err
	}

	var items []*SelectorItem

	for _, val := range response {
		log.Debug().
			Str("repository", repo).
			Str("release tag", val.Tag_name).
			Int("release id", val.Id).
			Msg("got release...")

		items = append(items, &SelectorItem{Name: val.Tag_name, Id: val.Id})
	}

	if interactive {
		return &InteractiveSelector{
			Kind:  Release,
			Items: items,

			Prompt: fmt.Sprintf("Please select %s release tag", repo),
			Single: true,
		}, nil
	}

	versionMatcher := version
	if versionMatcher == "latest" {
		log.Debug().
			Str("repository", repo).
			Msg("needed release version is 'latest', getting actual version")
		response := struct {
			Tag_name string
		}{}

		err := ghClient.Get(fmt.Sprintf("repos/%s/releases/latest", repo), &response)
		if err != nil {
			return nil, err
		}
		versionMatcher = response.Tag_name
		log.Debug().
			Str("repository", repo).
			Str("release tag", versionMatcher).
			Msg("got 'latest' release")
	}

	log.Info().
		Str("repository", repo).
		Msgf("using release %s", versionMatcher)

	return &Selector{
		Kind:  Release,
		Items: items,

		RegexpMatchers: []string{versionMatcher},
		Single:         true,
	}, nil
}

type AssetMatchCriteria struct {
	ReleaseId        int
	Name             string
	Regexps          []string
	Interactive      bool
	AllowForeignArch bool
}

type BinaryMatchCriteria struct {
	DownloadPath  string
	Names         []string
	Matcher       string
	Interactive   bool
	NativeExtract bool
}

func AssetSelector(ghClient GithubClient, repo string, criteria AssetMatchCriteria) (ISelector, error) {
	var linkRE = regexp.MustCompile(`<([^>]+)>;\s*rel="([^"]+)"`)

	var items []*SelectorItem
	page := 1
	requestPath := fmt.Sprintf("repos/%s/releases/%d/assets", repo, criteria.ReleaseId)

	log.Debug().
		Str("repository", repo).
		Int("release id", criteria.ReleaseId).
		Str("asset matching name", criteria.Name).
		Msg("getting release assets")

	findNextPage := func(response *http.Response) (string, bool) {
		for _, m := range linkRE.FindAllStringSubmatch(response.Header.Get("Link"), -1) {
			if len(m) > 2 && m[2] == "next" {
				return m[1], true
			}
		}
		return "", false
	}

	for {
		response, err := ghClient.Request(http.MethodGet, requestPath, nil)
		if err != nil {
			return nil, err
		}

		decoder := json.NewDecoder(response.Body)
		responseData := []struct{ Name string }{}
		err = decoder.Decode(&responseData)
		if err != nil {
			return nil, err
		}
		if err := response.Body.Close(); err != nil {
			return nil, err
		}

		for index, val := range responseData {

			items = append(items, &SelectorItem{Name: val.Name, Id: index})
			log.Debug().
				Str("repository", repo).
				Int("release id", criteria.ReleaseId).
				Str("asset name", val.Name).
				Int("asset index (id)", index).
				Msg("got release asset")
		}

		var hasNextPage bool
		if requestPath, hasNextPage = findNextPage(response); !hasNextPage {
			log.Debug().
				Str("repository", repo).
				Int("release id", criteria.ReleaseId).
				Msg("end of asset list")
			break
		}
		log.Debug().
			Str("repository", repo).
			Int("release id", criteria.ReleaseId).
			Msg("getting next page of release assets")
		page++
	}

	if criteria.Interactive {
		return &InteractiveSelector{
			Kind:  Asset,
			Items: items,

			Prompt: fmt.Sprintf("Please select %s asset", repo),
			Single: true,
		}, nil
	}

	var namesMatcher []string
	if criteria.Name != "" {
		namesMatcher = []string{criteria.Name}
	}

	return &Selector{
		Kind:  Asset,
		Items: items,

		NamesMatcher:     namesMatcher,
		RegexpMatchers:   criteria.Regexps,
		Single:           true,
		AllowForeignArch: criteria.AllowForeignArch,
	}, nil
}

func BinarySelector(criteria BinaryMatchCriteria) (ISelector, error) {
	log.Info().
		Str("asset download path", criteria.DownloadPath).
		Strs("asset matching binary names", criteria.Names).
		Str("asset matching binary regexp", criteria.Matcher).
		Msg("getting release asset binaries")

	inputStream, err := os.Open(criteria.DownloadPath)
	if err != nil {
		return nil, err
	}

	var items []*SelectorItem
	lowerPath := strings.ToLower(criteria.DownloadPath)
	isNativePkg := strings.HasSuffix(lowerPath, ".pkg.tar.zst") || strings.HasSuffix(lowerPath, ".pkg.tar.xz") || strings.HasSuffix(lowerPath, ".deb") || strings.HasSuffix(lowerPath, ".rpm") || strings.HasSuffix(lowerPath, ".msi") || strings.HasSuffix(lowerPath, ".dmg") || strings.HasSuffix(lowerPath, ".pkg")
	isTarball := strings.HasSuffix(lowerPath, ".tar.gz") || strings.HasSuffix(lowerPath, ".tgz")
	isZip := strings.HasSuffix(lowerPath, ".zip")

	if isNativePkg {
		err = archiver.ErrNoMatch
	} else if criteria.NativeExtract && (isTarball || isZip) {
		log.Info().Msg("delegating archive extraction to native OS utilities for maximum performance")
		extractDir, _ := os.MkdirTemp("", "gh-ext-")
		extracted := false
		if isZip {
			if err := exec.Command("unzip", "-q", criteria.DownloadPath, "-d", extractDir).Run(); err == nil {
				extracted = true
			} else if err := exec.Command("7z", "x", criteria.DownloadPath, "-o"+extractDir, "-y").Run(); err == nil {
				extracted = true
			} else if err := exec.Command("tar", "-xf", criteria.DownloadPath, "-C", extractDir).Run(); err == nil {
				extracted = true
			}
		} else {
			if err := exec.Command("tar", "-xzf", criteria.DownloadPath, "-C", extractDir).Run(); err == nil {
				extracted = true
			} else {
				if runtime.GOOS == "windows" {
					if err := exec.Command("cmd", "/c", fmt.Sprintf("7z x %s -so | 7z x -si -ttar -o%s -y", criteria.DownloadPath, extractDir)).Run(); err == nil {
						extracted = true
					}
				} else {
					if err := exec.Command("sh", "-c", fmt.Sprintf("7z x %s -so | 7z x -si -ttar -o%s -y", criteria.DownloadPath, extractDir)).Run(); err == nil {
						extracted = true
					}
				}
			}
		}

		if extracted {
			_ = filepath.Walk(extractDir, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					items = append(items, &SelectorItem{
						Name:         info.Name(),
						Compressed:   false,
						BinaryType:   BinaryTypeFromPath(info.Name()),
						DownloadPath: path,
					})
				}
				return nil
			})
			if criteria.Interactive {
				return &InteractiveSelector{
					Kind:   Binary,
					Items:  items,
					Prompt: "Select binaries to be installed",
					Single: false,
				}, nil
			}
			return &Selector{
				Kind:           Binary,
				Items:          items,
				NamesMatcher:   criteria.Names,
				RegexpMatchers: []string{criteria.Matcher},
				Single:         false,
			}, nil
		}
		log.Warn().Msg("native extraction failed, falling back to pure Go archiver")
		_, _, err = archiver.Identify(criteria.DownloadPath, inputStream)
	} else {
		_, _, err = archiver.Identify(criteria.DownloadPath, inputStream)
	}

	if err != nil {
		if err == archiver.ErrNoMatch {
			items = append(items, &SelectorItem{
				Name:         filepath.Base(criteria.DownloadPath),
				Compressed:   false,
				BinaryType:   BinaryTypeFromPath(criteria.DownloadPath),
				DownloadPath: criteria.DownloadPath,
			})

			if criteria.Interactive {
				return &InteractiveSelector{
					Kind:   Binary,
					Items:  items,
					Prompt: "Confirm release binary to be installed",
					Single: true,
				}, nil
			}

			return &Selector{
				Kind:           Binary,
				Items:          items,
				NamesMatcher:   criteria.Names,
				RegexpMatchers: []string{regexp.QuoteMeta(filepath.Base(criteria.DownloadPath))},
				Single:         true,
			}, nil
		}
		return nil, err
	}

	fileSystem, err := archiver.FileSystem(context.TODO(), criteria.DownloadPath)
	if err != nil {
		return nil, err
	}

	err = fs.WalkDir(fileSystem, ".", func(fsPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			items = append(items, &SelectorItem{
				Name:       d.Name(),
				Compressed: true,
				BinaryType: BinaryTypeFromPath(d.Name()),
				FsPath:     fsPath,
				Fs:         fileSystem,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if criteria.Interactive {
		return &InteractiveSelector{
			Kind:   Binary,
			Items:  items,
			Prompt: "Select binaries to be installed",
			Single: false,
		}, nil
	}

	return &Selector{
		Kind:           Binary,
		Items:          items,
		NamesMatcher:   criteria.Names,
		RegexpMatchers: []string{criteria.Matcher},
		Single:         false,
	}, nil
}
