package selector

import (
	"fmt"
	"regexp"
	"runtime"
	"strings"
)

type Selector struct {
	Kind             SelectorKind
	Items            []*SelectorItem
	NamesMatcher     []string
	RegexpMatchers   []string
	Single           bool
	AllowForeignArch bool
}

func (s *Selector) Run() ([]*SelectorItem, error) {
	var selectedItems []*SelectorItem

	if len(s.NamesMatcher) > 0 {
		for _, item := range s.Items {
			for _, name := range s.NamesMatcher {
				if strings.Compare(strings.ToLower(name), strings.ToLower(item.Name)) == 0 {
					item.Selected = true
					selectedItems = append(selectedItems, item)
				}
			}
		}
	} else if len(s.RegexpMatchers) > 0 {
		muslRegex := regexp.MustCompile(`(?i)[-_]musl[-_.]`)
		foreignArchRegex := getForeignArchRegex(runtime.GOARCH)
		// Try regex matchers in priority order
		for _, rx := range s.RegexpMatchers {
			compiledRx, err := regexp.Compile(rx)
			if err != nil {
				return nil, err
			}
			var currentMatches []*SelectorItem
			for _, item := range s.Items {
				if compiledRx.MatchString(item.Name) {
					if !s.AllowForeignArch && foreignArchRegex != nil && foreignArchRegex.MatchString(item.Name) {
						// Only apply foreign filter if the regex itself didn't explicitly ask for it
						if !foreignArchRegex.MatchString(rx) && !strings.Contains(strings.ToLower(rx), "arm") && !strings.Contains(strings.ToLower(rx), "386") {
							continue
						}
					}
					currentMatches = append(currentMatches, item)
				}
			}
			if len(currentMatches) > 0 {
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

				for _, item := range currentMatches {
					item.Selected = true
					selectedItems = append(selectedItems, item)
				}
				break // Found matches with this priority regex, don't fallback
			}
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
