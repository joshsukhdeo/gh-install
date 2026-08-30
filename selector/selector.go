package selector

import (
	"fmt"
	"regexp"
	"strings"
)

type Selector struct {
	Kind           SelectorKind
	Items          map[string]*SelectorItem
	ItemOrder      []string
	NamesMatcher   []string
	RegexpMatchers []string
	Single         bool
}

func (s *Selector) Run() ([]*SelectorItem, error) {
	var selectedItems []*SelectorItem

	// Ensure we iterate in the provided order if available
	var order []string
	if len(s.ItemOrder) > 0 {
		order = s.ItemOrder
	} else {
		for k := range s.Items {
			order = append(order, k)
		}
	}

	if len(s.NamesMatcher) > 0 {
		for _, itemName := range order {
			item, ok := s.Items[itemName]
			if !ok {
				continue
			}
			for _, name := range s.NamesMatcher {
				if strings.Compare(strings.ToLower(name), strings.ToLower(itemName)) == 0 {
					item.Selected = true
					selectedItems = append(selectedItems, item)
				}
			}
		}
	} else if len(s.RegexpMatchers) > 0 {
		// Try regex matchers in priority order
		for _, rx := range s.RegexpMatchers {
			for _, itemName := range order {
				item, ok := s.Items[itemName]
				if !ok {
					continue
				}
				match, err := regexp.MatchString(rx, itemName)
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
		return nil, fmt.Errorf("no %s matches for names '%s' or regexps '%v' found", s.Kind.String(), s.NamesMatcher, s.RegexpMatchers)
	}
	return selectedItems, nil
}

func (s *Selector) GetKind() SelectorKind {
	return s.Kind
}
