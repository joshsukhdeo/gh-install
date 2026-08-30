package selector

import (
	"fmt"
	"regexp"
	"strings"
)

type Selector struct {
	Kind           SelectorKind
	Items          []*SelectorItem
	NamesMatcher   []string
	RegexpMatchers []string
	Single         bool
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
		// Try regex matchers in priority order
		for _, rx := range s.RegexpMatchers {
			compiledRx, err := regexp.Compile(rx)
			if err != nil {
				return nil, err
			}
			var currentMatches []*SelectorItem
			for _, item := range s.Items {
				if compiledRx.MatchString(item.Name) {
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
		return nil, fmt.Errorf("no %s matches for names '%s' or regexps '%v' found", s.Kind.String(), s.NamesMatcher, s.RegexpMatchers)
	}
	return selectedItems, nil
}

func (s *Selector) GetKind() SelectorKind {
	return s.Kind
}
