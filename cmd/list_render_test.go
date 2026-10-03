package cmd

import (
	"strings"
	"testing"

	"github.com/pterm/pterm"
)

func sampleState() pterm.TableData {
	return pterm.TableData{
		{"Repository", "Version", "Target Path"},
		{"owner/app", "v1.0.0", "/home/tay/builds/app-build-scripts-ubuntu26/intel/llama-cpp-build"},
		{"owner/other", "v2.0.0", "/usr/local/bin"},
	}
}

func isTable(s string) bool { return strings.Contains(s, "┌") }

func TestAutoRender_FitsAsTable(t *testing.T) {
	out := autoRender(sampleState(), 500)
	if !isTable(out) {
		t.Fatalf("expected table at width 500, got:\n%s", out)
	}
	if renderedWidth(out) > 500 {
		t.Fatalf("table exceeds width")
	}
}

func TestAutoRender_WrapsToFit(t *testing.T) {
	unwrapped := renderedWidth(renderTable(sampleState(), 0))
	width := unwrapped - 10
	out := autoRender(sampleState(), width)
	if !isTable(out) {
		t.Fatalf("expected wrapped table at width %d, got:\n%s", width, out)
	}
	if got := renderedWidth(out); got > width {
		t.Fatalf("wrapped table width %d exceeds %d", got, width)
	}
}

func TestAutoRender_FallsBackToList(t *testing.T) {
	out := autoRender(sampleState(), 30)
	if isTable(out) {
		t.Fatalf("expected list at width 30, got:\n%s", out)
	}
	plain := pterm.RemoveColorFromString(out)
	for _, want := range []string{"owner/app", "Version", "v1.0.0", "owner/other"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("list missing %q:\n%s", want, plain)
		}
	}
	// No characters may be lost to wrapping.
	if !strings.Contains(strings.ReplaceAll(plain, "\n", ""), "llama-cpp-build") {
		t.Fatalf("list lost path characters:\n%s", plain)
	}
}

func TestAutoRender_UnknownWidthIsTable(t *testing.T) {
	if !isTable(autoRender(sampleState(), 0)) {
		t.Fatal("expected table when width is unknown")
	}
}
