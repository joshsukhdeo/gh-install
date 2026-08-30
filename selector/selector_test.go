package selector

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSelector_PrioritizesNonMusl(t *testing.T) {
	items := []*SelectorItem{
		{Name: "app-linux-musl-x64.tar.gz"},
		{Name: "app-linux-x64.tar.gz"},
	}

	sel := &Selector{
		Kind:           Asset,
		Items:          items,
		RegexpMatchers: []string{`.*(?:amd64|x86_64|x64).*\.(?i:tar\.gz)$`},
		Single:         true,
	}

	selected, err := sel.Run()
	assert.NoError(t, err)
	assert.Len(t, selected, 1)
	assert.Equal(t, "app-linux-x64.tar.gz", selected[0].Name)
}
