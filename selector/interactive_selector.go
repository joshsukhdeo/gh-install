package selector

import (
	"atomicgo.dev/keyboard/keys"
	"github.com/pterm/pterm"
)

type InteractiveSelector struct {
	Kind   SelectorKind
	Items  []*SelectorItem
	Prompt string
	Single bool
}

func (s *InteractiveSelector) showPrompt() ([]string, error) {
	var itemOrder []string
	for _, item := range s.Items {
		itemOrder = append(itemOrder, item.Name)
	}

	if s.Single {
		selectedItem, err := pterm.DefaultInteractiveSelect.
			WithOptions(itemOrder).
			WithDefaultText(s.Prompt).Show()
		if err != nil {
			return nil, err
		}
		return []string{selectedItem}, nil
	}

	selectedItems, err := pterm.DefaultInteractiveMultiselect.
		WithOptions(itemOrder).
		WithDefaultText(s.Prompt).
		WithKeyConfirm(keys.Enter).
		WithKeySelect(keys.Space).
		WithFilter(false).
		Show()
	if err != nil {
		return nil, err
	}
	return selectedItems, nil
}

func (s *InteractiveSelector) Run() ([]*SelectorItem, error) {
	selectedNames, err := s.showPrompt()
	if err != nil {
		return nil, err
	}

	var selectedItems []*SelectorItem
	for _, selectedName := range selectedNames {
		for _, item := range s.Items {
			if item.Name == selectedName {
				selectedItems = append(selectedItems, item)
				break
			}
		}
	}

	return selectedItems, nil
}

