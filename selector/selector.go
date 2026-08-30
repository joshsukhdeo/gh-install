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
		// Try regex matchers in priority order
		for _, rx := range s.RegexpMatchers {
			for _, item := range s.Items {
				match, err := regexp.MatchString(rx, item.Name)
				if err != nil {
					return nil, err
				}
				if match {
					item.Selected = true
					selectedItems = append(selectedItems, item)
				}
			}
			if len(selectedItems) > 0 {
				break // Found matches with this priority regex, don't fallback
			}
		}
	}

	if len(selectedItems) == 0 {
		return nil, fmt.Errorf("no %d matches for names '%s' or regexps '%v' found", s.Kind, s.NamesMatcher, s.RegexpMatchers)
	}
	return selectedItems, nil
}
