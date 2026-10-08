package selector

import (
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/charmbracelet/log"
)

type Selector struct {
	Kind             SelectorKind
	Items            []*SelectorItem
	NamesMatcher     []string
	RegexpMatchers   []string
	Single           bool
	AllowForeignArch bool
	Repository       string
	OnlyFirstMatch   bool
	MaxExeInstalls   int
	AvxLevel         string
}

func getMajorityPrefix(items []*SelectorItem) string {
	n := len(items)
	if n == 0 {
		return ""
	}
	bestPrefix := ""
	for _, item := range items {
		for l := len(item.Name); l > len(bestPrefix); l-- {
			prefix := item.Name[:l]
			count := 0
			for _, other := range items {
				if strings.HasPrefix(other.Name, prefix) {
					count++
				}
			}
			if count > n/2 {
				if len(prefix) > len(bestPrefix) {
					bestPrefix = prefix
				}
				break
			}
		}
	}
	return bestPrefix
}

type fallbackLevel struct {
	name   string
	filter func(string) bool
}

func (s *Selector) Run() ([]*SelectorItem, error) {
	var selectedItems []*SelectorItem

	if s.Kind == Binary && len(s.NamesMatcher) == 0 && (len(s.RegexpMatchers) == 0 || (len(s.RegexpMatchers) == 1 && s.RegexpMatchers[0] == "")) {
		var binMatches []*SelectorItem
		seen := make(map[string]bool)
		for _, item := range s.Items {
			ext := strings.ToLower(filepath.Ext(item.Name))
			isExec := false
			switch ext {
			case ".exe", ".appimage", ".bin", ".deb", ".rpm", ".msi", ".dmg", ".pkg":
				isExec = true
			case "":
				if IsActuallyExecutable(item) {
					isExec = true
				}
			}
			if isExec && !seen[item.Name] {
				seen[item.Name] = true
				item.Selected = true
				binMatches = append(binMatches, item)
			}
		}
		if len(binMatches) > 0 {
			if (s.OnlyFirstMatch || s.Single) && len(binMatches) > 1 {
				binMatches = binMatches[:1]
			}
			if s.MaxExeInstalls > 0 && len(binMatches) > s.MaxExeInstalls {
				binMatches = binMatches[:s.MaxExeInstalls]
			}
			return binMatches, nil
		}
	}

	if s.Kind == Asset && len(s.NamesMatcher) == 0 {
		classifier := DefaultClassifier()
		if s.AvxLevel != "" && s.AvxLevel != "auto" {
			classifier.HostAVXLevel = s.AvxLevel
		}
		var assetNames []string
		for _, it := range s.Items {
			assetNames = append(assetNames, it.Name)
		}
		defaultAssets := classifier.PickDefaultAssetsWithContext(s.Repository, assetNames, s.RegexpMatchers)
		if len(defaultAssets) > 0 {
			var matchedItems []*SelectorItem
			defaultMap := make(map[string]bool)
			for _, da := range defaultAssets {
				defaultMap[da] = true
			}
			for _, it := range s.Items {
				if defaultMap[it.Name] {
					it.Selected = true
					matchedItems = append(matchedItems, it)
				}
			}
			if len(matchedItems) > 0 {
				if s.OnlyFirstMatch && len(matchedItems) > 1 {
					matchedItems = matchedItems[:1]
				}
				return matchedItems, nil
			}
		}
	}

	if len(s.NamesMatcher) > 0 {
		for _, item := range s.Items {
			for _, name := range s.NamesMatcher {
				if strings.Compare(strings.ToLower(name), strings.ToLower(item.Name)) == 0 {
					item.Selected = true
					selectedItems = append(selectedItems, item)
				}
			}
		}
		if s.OnlyFirstMatch && len(selectedItems) > 1 {
			selectedItems = selectedItems[:1]
		}
		if s.MaxExeInstalls > 0 && len(selectedItems) > s.MaxExeInstalls {
			selectedItems = selectedItems[:s.MaxExeInstalls]
		}
	} else if len(s.RegexpMatchers) > 0 {
		muslRegex := regexp.MustCompile("(?i)[-_]musl[-_.]")
		foreignArchRegex := getForeignArchRegex(runtime.GOARCH)

		effectiveAVX := s.AvxLevel
		if effectiveAVX == "" || effectiveAVX == "auto" {
			effectiveAVX = DetectHostAVXLevel()
		}

		var ownerid, repoid string
		parts := strings.Split(s.Repository, "/")
		if len(parts) == 2 {
			ownerid = strings.ToLower(parts[0])
			repoid = strings.ToLower(parts[1])
		} else if len(parts) == 1 {
			repoid = strings.ToLower(parts[0])
		}

		lcp := getMajorityPrefix(s.Items)

		var levels []fallbackLevel
		if repoid != "" {
			normalizedRepoid := strings.ReplaceAll(strings.ReplaceAll(repoid, "-", ""), "_", "")
			levels = append(levels, fallbackLevel{"repoid", func(n string) bool {
				normalizedN := strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(n), "-", ""), "_", "")
				return strings.Contains(normalizedN, normalizedRepoid)
			}})
			// Add a fallback for cases where the binary is a prefix of the repo (e.g., 'nu' for 'nushell')
			levels = append(levels, fallbackLevel{"repoid_prefix", func(n string) bool {
				normalizedN := strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(n), "-", ""), "_", "")
				return strings.HasPrefix(normalizedRepoid, normalizedN) || strings.HasPrefix(normalizedN, normalizedRepoid)
			}})
		}
		if lcp != "" {
			levels = append(levels, fallbackLevel{"lcp", func(n string) bool { return strings.HasPrefix(n, lcp) }})
		}
		if ownerid != "" {
			levels = append(levels, fallbackLevel{"ownerid", func(n string) bool { return strings.Contains(strings.ToLower(n), ownerid) }})
		}
		levels = append(levels, fallbackLevel{"blind", func(n string) bool { return true }})

		var strictRegexps []string
		var weakRegexps []string
		for _, rx := range s.RegexpMatchers {
			if rx == "^.*$" || strings.HasPrefix(rx, "^.*\\.(?i:") {
				weakRegexps = append(weakRegexps, rx)
			} else {
				strictRegexps = append(strictRegexps, rx)
			}
		}

		executePass := func(regexps []string) ([]*SelectorItem, error) {
			for _, level := range levels {
				for _, rx := range regexps {
					compiledRx, err := regexp.Compile(rx)
					if err != nil {
						return nil, err
					}
					var currentMatches []*SelectorItem
					for _, item := range s.Items {
						if !level.filter(item.Name) {
							continue
						}
						if compiledRx.MatchString(item.Name) {
							if s.Kind == Asset {
								lowerName := strings.ToLower(item.Name)
								if strings.Contains(lowerName, "checksum") ||
									strings.Contains(lowerName, "sha256") ||
									strings.Contains(lowerName, "sha512") ||
									strings.Contains(lowerName, "source") ||
									strings.HasSuffix(lowerName, ".txt") ||
									strings.HasSuffix(lowerName, ".md") ||
									strings.HasSuffix(lowerName, ".pem") ||
									strings.HasSuffix(lowerName, ".sig") {
									// Only allow if the regex explicitly looks for this type of file
									if !strings.Contains(strings.ToLower(rx), "txt") &&
										!strings.Contains(strings.ToLower(rx), "checksum") &&
										!strings.Contains(strings.ToLower(rx), "sha") &&
										!strings.Contains(strings.ToLower(rx), "source") {
										continue
									}
								}
							}

							if !s.AllowForeignArch && foreignArchRegex != nil && foreignArchRegex.MatchString(item.Name) {
								// Only apply foreign filter if the regex itself didn't explicitly ask for it
								if !foreignArchRegex.MatchString(rx) && !strings.Contains(strings.ToLower(rx), "arm") && !strings.Contains(strings.ToLower(rx), "386") {
									continue
								}
							}

							if s.Kind == Asset && !s.AllowForeignArch && !IsAVXCompatible(item.Name, effectiveAVX) {
								// Only apply AVX filter if the regex itself didn't explicitly ask for it
								if !strings.Contains(strings.ToLower(rx), "avx") {
									continue
								}
							}

							currentMatches = append(currentMatches, item)
						}
					}
					if len(currentMatches) > 0 {
						if s.Kind == Binary {
							var execMatches []*SelectorItem
							for _, item := range currentMatches {
								ext := strings.ToLower(filepath.Ext(item.Name))
								switch ext {
								case ".exe", ".appimage", ".bin", ".deb", ".rpm", ".msi", ".dmg", ".pkg":
									execMatches = append(execMatches, item)
								case "":
									// Strictly filter extensionless files using magic bytes
									if IsActuallyExecutable(item) {
										execMatches = append(execMatches, item)
									}
								}
							}
							if len(execMatches) > 0 {
								currentMatches = execMatches
							}
						}
						// If multiple items match, prefer non-musl over musl on Linux/standard distros
						var nonMusl []*SelectorItem
						for _, item := range currentMatches {
							if !muslRegex.MatchString(item.Name) {
								nonMusl = append(nonMusl, item)
							}
						}
						if len(nonMusl) > 0 {
							currentMatches = nonMusl
						}

						// If multiple assets match, prioritize highest compatible AVX level
						if s.Kind == Asset {
							var avxMatches []*SelectorItem
							highestRank := -1
							for _, item := range currentMatches {
								rank := AVXLevelRank(AssetAVXRequirement(item.Name))
								if rank > highestRank {
									highestRank = rank
								}
							}
							if highestRank > 0 {
								for _, item := range currentMatches {
									if AVXLevelRank(AssetAVXRequirement(item.Name)) == highestRank {
										avxMatches = append(avxMatches, item)
									}
								}
								if len(avxMatches) > 0 {
									currentMatches = avxMatches
								}
							}
						}

						// Prefer native packages over universal packages (AppImage, Flatpak, Snap)
						// when multiple assets have the same AVX level
						if s.Kind == Asset && len(currentMatches) > 1 {
							var nativeMatches []*SelectorItem
							for _, item := range currentMatches {
								lower := strings.ToLower(item.Name)
								if !strings.HasSuffix(lower, ".appimage") &&
									!strings.HasSuffix(lower, ".flatpak") &&
									!strings.HasSuffix(lower, ".snap") {
									nativeMatches = append(nativeMatches, item)
								}
							}
							if len(nativeMatches) > 0 {
								currentMatches = nativeMatches
							}
						}

						for _, item := range currentMatches {
							item.Selected = true
							selectedItems = append(selectedItems, item)
						}
						return selectedItems, nil // Return immediately upon finding the highest priority match
					}
				}
			}
			return nil, nil
		}

		if matches, err := executePass(strictRegexps); err != nil {
			return nil, err
		} else if len(matches) > 0 {
			if s.OnlyFirstMatch && len(matches) > 1 {
				matches = matches[:1]
			}
			if s.MaxExeInstalls > 0 && len(matches) > s.MaxExeInstalls {
				matches = matches[:s.MaxExeInstalls]
			}
			return matches, nil
		}

		if matches, err := executePass(weakRegexps); err != nil {
			return nil, err
		} else if len(matches) > 0 {
			for _, m := range matches {
				log.Warn("low confidence (<80%) asset selection; asset matched via weak fallback patterns, verify asset contents", "asset", m.Name)
			}
			if s.OnlyFirstMatch && len(matches) > 1 {
				matches = matches[:1]
			}
			if s.MaxExeInstalls > 0 && len(matches) > s.MaxExeInstalls {
				matches = matches[:s.MaxExeInstalls]
			}
			return matches, nil
		}
	}

	if len(selectedItems) == 0 {
		return nil, fmt.Errorf("no %s matches found for the requested criteria", s.Kind.String())
	}
	return selectedItems, nil
}

func (s *Selector) GetKind() SelectorKind {
	return s.Kind
}

func getForeignArchRegex(goarch string) *regexp.Regexp {
	var foreign []string
	switch goarch {
	case "amd64":
		foreign = []string{"arm64", "aarch64", "armhf", "armv7", "armv6", "386", "i386", "32-bit", "mips64", "ppc64le", "s390x", "riscv64"}
	case "arm64":
		foreign = []string{"amd64", "x86_64", "x64", "x86", "386", "i386", "armhf", "armv7", "armv6", "mips64", "ppc64le", "s390x", "riscv64"}
	default:
		return nil
	}
	pattern := "(?i)[-_\\.](?:" + strings.Join(foreign, "|") + ")(?:[-_\\.]|$)"
	return regexp.MustCompile(pattern)
}
