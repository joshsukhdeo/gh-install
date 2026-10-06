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
	"sort"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/mholt/archiver/v4"
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

func ReleaseSelector(ghClient GithubClient, repo string, version string, interactive bool, opts ...bool) (ISelector, error) {
	log.Info("getting Github repository releases", "repository", repo)

	var prerelease, stable bool
	if len(opts) > 0 {
		prerelease = opts[0]
	}
	if len(opts) > 1 {
		stable = opts[1]
	}

	response := []struct {
		Tag_name   string
		Id         int
		Prerelease bool
	}{}
	err := ghClient.Get(fmt.Sprintf("repos/%s/releases", repo), &response)
	if err != nil {
		return nil, err
	}

	var items []*SelectorItem

	for _, val := range response {
		log.Debug("got release...", "repository", repo, "release tag", val.Tag_name, "release id", val.Id, "prerelease", val.Prerelease)

		items = append(items, &SelectorItem{Name: val.Tag_name, Id: val.Id, Prerelease: val.Prerelease})
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
		if prerelease {
			if len(response) > 0 {
				versionMatcher = response[0].Tag_name
			}
		} else {
			log.Debug("needed release version is 'latest', getting actual version", "repository", repo)
			latestResp := struct {
				Tag_name   string
				Prerelease bool
			}{}

			err := ghClient.Get(fmt.Sprintf("repos/%s/releases/latest", repo), &latestResp)
			if err == nil && latestResp.Tag_name != "" {
				versionMatcher = latestResp.Tag_name
			} else {
				for _, val := range response {
					if !val.Prerelease {
						versionMatcher = val.Tag_name
						break
					}
				}
				if versionMatcher == "latest" {
					if err != nil && (len(response) == 0 || stable) {
						return nil, err
					}
					if len(response) > 0 && !stable {
						versionMatcher = response[0].Tag_name
					}
				}
			}
			log.Debug("got 'latest' release", "repository", repo, "release tag", versionMatcher)
		}
	}

	log.Info(fmt.Sprintf("using release %s", versionMatcher), "repository", repo)

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
	Single           bool
	AvxLevel         string
}

// IsAllowedBinaryDir checks if a file inside an archive sits in root, bin, bin32, or bin64.
func IsAllowedBinaryDir(relPath string) bool {
	clean := filepath.ToSlash(filepath.Clean(relPath))
	parts := strings.Split(clean, "/")
	if len(parts) == 1 {
		return true
	}
	if len(parts) == 2 {
		dir := parts[0]
		return dir == "bin" || dir == "bin32" || dir == "bin64" || !strings.Contains(dir, "/")
	}
	if len(parts) == 3 {
		dir := parts[1]
		return dir == "bin" || dir == "bin32" || dir == "bin64"
	}
	return false
}

type BinaryMatchCriteria struct {
	DownloadPath   string
	Names          []string
	Matcher        string
	Interactive    bool
	Extractor      string
	Repository     string
	OnlyFirstMatch bool
	MaxExeInstalls int
}

func AssetSelector(ghClient GithubClient, repo string, criteria AssetMatchCriteria) (ISelector, error) {
	var linkRE = regexp.MustCompile(`<([^>]+)>;\s*rel="([^"]+)"`)

	var items []*SelectorItem
	page := 1
	requestPath := fmt.Sprintf("repos/%s/releases/%d/assets", repo, criteria.ReleaseId)

	log.Debug("getting release assets", "repository", repo, "release id", criteria.ReleaseId, "asset matching name", criteria.Name)

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
			log.Debug("got release asset", "repository", repo, "release id", criteria.ReleaseId, "asset name", val.Name, "asset index (id)", index)
		}

		var hasNextPage bool
		if requestPath, hasNextPage = findNextPage(response); !hasNextPage {
			log.Debug("end of asset list", "repository", repo, "release id", criteria.ReleaseId)
			break
		}
		log.Debug("getting next page of release assets", "repository", repo, "release id", criteria.ReleaseId)
		page++
	}

	if criteria.Interactive {
		for _, item := range items {
			FormatCategorizedItem(item)
		}
		sort.SliceStable(items, func(i, j int) bool {
			return AssetCategoryPriority(items[i].Name) < AssetCategoryPriority(items[j].Name)
		})

		return &InteractiveSelector{
			Kind:  Asset,
			Items: items,

			Prompt: fmt.Sprintf("Please select %s asset", repo),
			Single: criteria.Single,
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
		Repository:       repo,
		AvxLevel:         criteria.AvxLevel,
	}, nil
}

func BinarySelector(criteria BinaryMatchCriteria) (ISelector, error) {
	log.Info("getting release asset binaries", "asset download path", criteria.DownloadPath, "asset matching binary names", criteria.Names, "asset matching binary regexp", criteria.Matcher)

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
	} else if isTarball || isZip {
		precedence := []string{"ouch", "native", "internal"}
		if criteria.Extractor != "" && criteria.Extractor != "default" {
			switch criteria.Extractor {
			case "internal":
				precedence = []string{"internal"}
			case "native":
				precedence = []string{"native", "internal"}
			case "ouch":
				precedence = []string{"ouch", "native", "internal"}
			}
		}

		extractDir, _ := os.MkdirTemp("", "gh-ext-")
		extracted := false

		for _, method := range precedence {
			if method == "ouch" {
				if _, err := exec.LookPath("ouch"); err == nil {
					if err := exec.Command("ouch", "d", criteria.DownloadPath, "-y", "-q", "--dir", extractDir).Run(); err == nil {
						log.Info("delegated archive extraction to ouch")
						extracted = true
					}
					break
				}
			} else if method == "native" {
				if isZip {
					if _, err := exec.LookPath("unzip"); err == nil {
						if err := exec.Command("unzip", "-q", criteria.DownloadPath, "-d", extractDir).Run(); err == nil {
							extracted = true
						}
					} else if _, err := exec.LookPath("7z"); err == nil {
						if err := exec.Command("7z", "x", criteria.DownloadPath, "-o"+extractDir, "-y").Run(); err == nil {
							extracted = true
						}
					}
				} else {
					if _, err := exec.LookPath("tar"); err == nil {
						if err := exec.Command("tar", "-xzf", criteria.DownloadPath, "-C", extractDir).Run(); err == nil {
							extracted = true
						}
					}
				}
				if extracted {
					log.Info("delegated archive extraction to native OS utilities")
					break
				}
			} else if method == "internal" {
				break
			}
		}

		if extracted {
			_ = filepath.Walk(extractDir, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					relPath, relErr := filepath.Rel(extractDir, path)
					if relErr == nil && !IsAllowedBinaryDir(relPath) {
						return nil
					}
					items = append(items, &SelectorItem{
						Name:         info.Name(),
						Compressed:   false,
						BinaryType:   BinaryTypeFromPath(info.Name()),
						DownloadPath: path,
						ExtractDir:   extractDir,
					})
				}
				return nil
			})
			if criteria.Interactive {
				return &InteractiveSelector{
					Kind:       Binary,
					Items:      items,
					Prompt:     "Select binaries to be installed",
					Single:     criteria.OnlyFirstMatch,
					Repository: criteria.Repository,
				}, nil
			}
			return &Selector{
				Kind:           Binary,
				Items:          items,
				NamesMatcher:   criteria.Names,
				RegexpMatchers: []string{criteria.Matcher},
				Single:         criteria.OnlyFirstMatch,
				Repository:     criteria.Repository,
				OnlyFirstMatch: criteria.OnlyFirstMatch,
				MaxExeInstalls: criteria.MaxExeInstalls,
			}, nil
		}
		log.Warn("native extraction failed, falling back to pure Go archiver")
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
					Kind:       Binary,
					Items:      items,
					Prompt:     "Confirm release binary to be installed",
					Single:     true,
					Repository: criteria.Repository,
				}, nil
			}

			return &Selector{
				Kind:           Binary,
				Items:          items,
				NamesMatcher:   criteria.Names,
				RegexpMatchers: []string{regexp.QuoteMeta(filepath.Base(criteria.DownloadPath))},
				Single:         true,
				Repository:     criteria.Repository,
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
			if !IsAllowedBinaryDir(fsPath) {
				return nil
			}
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
			Kind:       Binary,
			Items:      items,
			Prompt:     "Select binaries to be installed",
			Single:     criteria.OnlyFirstMatch,
			Repository: criteria.Repository,
		}, nil
	}

	return &Selector{
		Kind:           Binary,
		Items:          items,
		NamesMatcher:   criteria.Names,
		RegexpMatchers: []string{criteria.Matcher},
		Single:         criteria.OnlyFirstMatch,
		Repository:     criteria.Repository,
		OnlyFirstMatch: criteria.OnlyFirstMatch,
		MaxExeInstalls: criteria.MaxExeInstalls,
	}, nil
}
