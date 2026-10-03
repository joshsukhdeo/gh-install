package cmd

import (
	"strings"
	"testing"
)

func TestWrapCell(t *testing.T) {
	if got := wrapCell("short", 40); got != "short" {
		t.Fatalf("short string changed: %q", got)
	}

	path := "/home/tay/builds/app-build-scripts-ubuntu26/intel/llama-cpp-build"
	got := wrapCell(path, 40)
	if strings.ReplaceAll(got, "\n", "") != path {
		t.Fatalf("wrapping lost characters: %q", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if len([]rune(line)) > 40 {
			t.Fatalf("line exceeds width: %q", line)
		}
	}
	first := strings.Split(got, "\n")[0]
	if !strings.ContainsRune(" ,/-_.", []rune(first)[len([]rune(first))-1]) {
		t.Fatalf("expected break after a separator: %q", got)
	}

	list := "alpha, bravo, charlie, delta, echo, foxtrot, golf"
	for _, line := range strings.Split(wrapCell(list, 20), "\n") {
		if strings.HasPrefix(line, " ") || len([]rune(line)) > 20 {
			t.Fatalf("bad wrapped line: %q", line)
		}
	}
}
