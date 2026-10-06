package selector

import (
	"fmt"

	"atomicgo.dev/keyboard/keys"
	"github.com/pterm/pterm"
)

type Prompter interface {
	Select(options []string, prompt string) (string, error)
	MultiSelect(options []string, prompt string) ([]string, error)
}

type PtermPrompter struct{}

func (p PtermPrompter) Select(options []string, prompt string) (string, error) {
	return pterm.DefaultInteractiveSelect.
		WithOptions(options).
		WithDefaultText(prompt).Show()
}

func (p PtermPrompter) MultiSelect(options []string, prompt string) ([]string, error) {
	return pterm.DefaultInteractiveMultiselect.
		WithOptions(options).
		WithDefaultText(prompt).
		WithKeyConfirm(keys.Enter).
		WithKeySelect(keys.Space).
		WithFilter(false).
		Show()
}

type InteractiveSelector struct {
	Kind       SelectorKind
	Items      []*SelectorItem
	Prompt     string
	Repository string
	Single     bool
	Prompter   Prompter
}

func (s *InteractiveSelector) showPrompt() ([]string, error) {
	var itemOrder []string
	displayToName := make(map[string]string)
	for _, item := range s.Items {
		disp := item.DisplayName
		if disp == "" {
			disp = item.Name
		}
		itemOrder = append(itemOrder, disp)
		displayToName[disp] = item.Name
	}

	if s.Prompter == nil {
		s.Prompter = PtermPrompter{}
	}

	if s.Single {
		selectedItem, err := s.Prompter.Select(itemOrder, s.Prompt)
		if err != nil {
			return nil, err
		}
		if selectedItem == "" {
			return []string{}, nil
		}
		name := displayToName[selectedItem]
		if name == "" {
			name = selectedItem
		}
		return []string{name}, nil
	}

	selectedDisplays, err := s.Prompter.MultiSelect(itemOrder, s.Prompt)
	if err != nil {
		return nil, err
	}
	var selectedNames []string
	for _, disp := range selectedDisplays {
		name := displayToName[disp]
		if name == "" {
			name = disp
		}
		selectedNames = append(selectedNames, name)
	}
	return selectedNames, nil
}

func (s *InteractiveSelector) Run() ([]*SelectorItem, error) {
	selectedNames, err := s.showPrompt()
	if err != nil {
		return nil, fmt.Errorf("interactive prompt failed: %w", err)
	}
	if len(selectedNames) == 0 {
		return nil, fmt.Errorf("no items were selected")
	}

	var selectedItems []*SelectorItem
	for _, selectedName := range selectedNames {
		for _, item := range s.Items {
			if item.Name == selectedName || item.DisplayName == selectedName {
				selectedItems = append(selectedItems, item)
				break
			}
		}
	}

	if len(selectedItems) == 0 {
		return nil, fmt.Errorf("could not match selected items with internal list")
	}

	return selectedItems, nil
}

func (s *InteractiveSelector) GetKind() SelectorKind {
	return s.Kind
}
