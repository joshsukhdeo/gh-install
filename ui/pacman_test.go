package ui

import (
	"bytes"
	"math"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestPacmanModel_Init(t *testing.T) {
	m := NewPacmanModel("test/repo")
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected Init() to return a tick command, got nil")
	}
}

func TestPacmanModel_StageToProgress(t *testing.T) {
	tests := []struct {
		stage    int
		expected float64
	}{
		{stage: 0, expected: 0.08},
		{stage: 1, expected: 0.22},
		{stage: 2, expected: 0.40},
		{stage: 3, expected: 0.58},
		{stage: 4, expected: 0.75},
		{stage: 5, expected: 0.88},
		{stage: 6, expected: 1.00},
	}

	for _, tt := range tests {
		actual := stageToProgress(tt.stage, 0, 0)
		if actual != tt.expected {
			t.Errorf("stageToProgress(%d) = %f, want %f", tt.stage, actual, tt.expected)
		}
	}
}

func TestPacmanModel_HarmonicaInterpolation(t *testing.T) {
	m := NewPacmanModel("owner/repo")
	m.ProgressCurrent = 0.0

	// Progress jumps to stage 4 (target = 0.75)
	model, _ := m.Update(PacmanProgressMsg{Stage: 4})
	m = model.(*PacmanModel)

	if m.ProgressTarget != 0.75 {
		t.Fatalf("expected ProgressTarget = 0.75, got %f", m.ProgressTarget)
	}

	// First tick: harmonica should smoothly move ProgressCurrent forward, NOT jump directly to 0.75
	model, _ = m.Update(PacmanTickMsg{})
	m = model.(*PacmanModel)

	if m.ProgressCurrent <= 0.0 {
		t.Errorf("expected ProgressCurrent to advance past 0, got %f", m.ProgressCurrent)
	}
	if m.ProgressCurrent >= 0.75 {
		t.Errorf("expected ProgressCurrent to smoothly glide and not jump directly to target, got %f", m.ProgressCurrent)
	}

	// Advance frames to let the harmonica spring converge smoothly
	for i := 0; i < 120 && math.Abs(m.ProgressCurrent-0.75) > 0.005; i++ {
		model, _ = m.Update(PacmanTickMsg{})
		m = model.(*PacmanModel)
	}

	if math.Abs(m.ProgressCurrent-0.75) > 0.01 {
		t.Errorf("expected ProgressCurrent to converge near 0.75, got %f", m.ProgressCurrent)
	}

	// Jump to completion (Stage 6 -> 1.00)
	model, _ = m.Update(PacmanProgressMsg{Stage: 6})
	m = model.(*PacmanModel)

	if m.ProgressTarget != 1.00 {
		t.Fatalf("expected ProgressTarget = 1.00, got %f", m.ProgressTarget)
	}

	// Glides smoothly toward 1.00
	for i := 0; i < 120 && math.Abs(m.ProgressCurrent-1.00) > 0.005; i++ {
		model, _ = m.Update(PacmanTickMsg{})
		m = model.(*PacmanModel)
	}

	if m.ProgressCurrent < 0.99 {
		t.Errorf("expected ProgressCurrent to converge near 1.0, got %f", m.ProgressCurrent)
	}
}

func TestPacmanModel_View(t *testing.T) {
	m := NewPacmanModel("test/repo")
	m.Version = "v1.2.3"
	m.Archive = "archive.tar.gz"

	view := m.View().Content

	if !strings.Contains(view, "test/repo") {
		t.Errorf("expected view to contain repo name, got:\n%s", view)
	}
	if !strings.Contains(view, "v1.2.3") {
		t.Errorf("expected view to contain version, got:\n%s", view)
	}
	if !strings.Contains(view, "archive.tar.gz") {
		t.Errorf("expected view to contain archive name, got:\n%s", view)
	}
	if !strings.Contains(view, "ᗧ") {
		t.Errorf("expected view to contain pacman 'ᗧ', got:\n%s", view)
	}
	if !strings.Contains(view, "•") {
		t.Errorf("expected view to contain dots '•', got:\n%s", view)
	}
	if !strings.Contains(view, "🍒") {
		t.Errorf("expected view to contain cherry '🍒', got:\n%s", view)
	}
}

func TestPacmanModel_DisableIcons(t *testing.T) {
	m := NewPacmanModel("test/repo")
	m.DisableIcons = true
	m.Version = "v1.0"
	m.Archive = "test.zip"

	view := m.View().Content

	if !strings.Contains(view, "R: test/repo") {
		t.Errorf("expected disabled icons header prefix 'R:', got:\n%s", view)
	}
	if !strings.Contains(view, "C ") {
		t.Errorf("expected pacman 'C' with disabled icons, got:\n%s", view)
	}
	if !strings.Contains(view, ". ") {
		t.Errorf("expected dot '.' with disabled icons, got:\n%s", view)
	}
	if !strings.Contains(view, "*") {
		t.Errorf("expected target '*' with disabled icons, got:\n%s", view)
	}
}

func TestPacmanModel_AssetHandling(t *testing.T) {
	m := NewPacmanModel("test/repo")

	// Add asset 1
	model, _ := m.Update(PacmanAddAssetMsg{Asset: AssetInfo{Name: "bin1", FullName: "/usr/local/bin/bin1"}})
	m = model.(*PacmanModel)

	// Add asset 2
	model, _ = m.Update(PacmanAddAssetMsg{Asset: AssetInfo{Name: "bin2"}})
	m = model.(*PacmanModel)

	if len(m.Assets) != 2 {
		t.Fatalf("expected 2 assets, got %d", len(m.Assets))
	}

	// Update asset 2 symlink and install cmd
	model, _ = m.Update(PacmanSetAssetIndexMsg{Index: 1})
	m = model.(*PacmanModel)
	model, _ = m.Update(PacmanSymlinkMsg{Symlink: "/usr/bin/bin2"})
	m = model.(*PacmanModel)
	model, _ = m.Update(PacmanInstallCmdMsg{InstallCmd: "sudo cp bin2 /usr/local/bin/"})
	m = model.(*PacmanModel)

	if m.Assets[1].Symlink != "/usr/bin/bin2" {
		t.Errorf("expected symlink '/usr/bin/bin2', got %q", m.Assets[1].Symlink)
	}
	if m.Assets[1].InstallCmd != "sudo cp bin2 /usr/local/bin/" {
		t.Errorf("expected install cmd 'sudo cp bin2 /usr/local/bin/', got %q", m.Assets[1].InstallCmd)
	}

	// Mark completed
	model, _ = m.Update(PacmanCompleteMsg{Message: "All done"})
	m = model.(*PacmanModel)

	if !m.Completed {
		t.Error("expected model to be completed")
	}
	for i, a := range m.Assets {
		if !a.Completed {
			t.Errorf("expected asset %d to be marked completed", i)
		}
	}

	view := m.View().Content
	if !strings.Contains(view, "bin1") || !strings.Contains(view, "bin2") {
		t.Errorf("expected view to contain asset names, got:\n%s", view)
	}
	if !strings.Contains(view, "🏁") {
		t.Errorf("expected view to contain completion flag '🏁', got:\n%s", view)
	}
}

func TestPacmanModel_PauseResume(t *testing.T) {
	m := NewPacmanModel("test/repo")
	m.ProgressTarget = 0.8

	// Pause
	model, _ := m.Update(PacmanPauseMsg{Paused: true})
	m = model.(*PacmanModel)

	initPos := m.ProgressCurrent
	model, _ = m.Update(PacmanTickMsg{})
	m = model.(*PacmanModel)

	if m.ProgressCurrent != initPos {
		t.Errorf("expected ProgressCurrent unchanged while paused, got %f vs %f", m.ProgressCurrent, initPos)
	}

	// Resume
	model, _ = m.Update(PacmanPauseMsg{Paused: false})
	m = model.(*PacmanModel)
	model, _ = m.Update(PacmanTickMsg{})
	m = model.(*PacmanModel)

	if m.ProgressCurrent == initPos {
		t.Errorf("expected ProgressCurrent to advance after resume, stayed at %f", initPos)
	}
}

func TestPacmanModel_Error(t *testing.T) {
	m := NewPacmanModel("test/repo")
	model, _ := m.Update(PacmanErrorMsg{Error: "verification failed"})
	m = model.(*PacmanModel)

	if !m.HasError || m.ErrorMsg != "verification failed" {
		t.Errorf("expected error state, got hasError=%v, errorMsg=%q", m.HasError, m.ErrorMsg)
	}

	view := m.View().Content
	if !strings.Contains(view, "👻") {
		t.Errorf("expected error view to contain ghost '👻', got:\n%s", view)
	}
	if !strings.Contains(view, "verification failed") {
		t.Errorf("expected error view to contain error message, got:\n%s", view)
	}
}

func TestPacmanModel_WindowSize(t *testing.T) {
	m := NewPacmanModel("test/repo")
	model, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = model.(*PacmanModel)

	if m.Width != 100 || m.Height != 30 {
		t.Errorf("expected 100x30, got %dx%d", m.Width, m.Height)
	}
	if m.TrackWidth <= 0 {
		t.Errorf("expected positive TrackWidth, got %d", m.TrackWidth)
	}
}

func TestPacmanUI_ProgressBarInterface(t *testing.T) {
	p := NewPacmanUI("test/repo")
	p.SetTTY(false)

	// Verify all ProgressBar interface methods work smoothly
	p.Start()
	p.Update(1, "v1.0.0", "archive.tar.gz", "asset.bin", "/target", "")
	p.AddAsset("asset.bin", "/target/asset.bin", "", "")
	p.SetCurrentAsset(0)
	p.UpdateSymlink("/usr/bin/asset")
	p.SetAssetInstallCmd("install asset")
	p.Pause()
	p.Resume()

	view := p.View()
	if !strings.Contains(view, "test/repo") {
		t.Errorf("expected view to contain repo name, got:\n%s", view)
	}

	p.Success("Installed successfully")
	p.WaitForAnimation()
	p.Stop()
	p.Finish()
}

func TestPacmanUI_NonTTY(t *testing.T) {
	var buf bytes.Buffer
	p := NewPacmanUI("test/non-tty-repo")
	p.SetTTY(false)
	p.SetOutput(&buf)
	p.Version = "v2.0"
	p.Archive = "pack.tar.gz"

	p.Start()
	p.AddAsset("bin", "/usr/bin/bin", "", "")
	p.Stop()

	output := buf.String()
	if !strings.Contains(output, "test/non-tty-repo") {
		t.Errorf("expected output to contain repo name, got:\n%s", output)
	}
	if strings.Contains(output, "\033[") {
		t.Errorf("non-TTY mode should not contain ANSI escape codes, got:\n%s", output)
	}
}

func TestPacmanUI_CompleteAllAnimations(t *testing.T) {
	p := NewPacmanUI("test/repo")
	p.SetTTY(false)
	p.AddAsset("a1", "/p/a1", "", "")
	p.AddAsset("a2", "/p/a2", "", "")

	p.completeAllAnimations()

	if p.CurrentAsset != 2 {
		t.Errorf("expected CurrentAsset = 2, got %d", p.CurrentAsset)
	}
	for i, a := range p.Assets {
		if !a.Completed {
			t.Errorf("expected asset %d to be completed", i)
		}
	}
	if p.Model().ProgressTarget != 1.0 {
		t.Errorf("expected ProgressTarget = 1.0, got %f", p.Model().ProgressTarget)
	}
}

func TestPacmanLogWriter(t *testing.T) {
	var buf bytes.Buffer
	writer := PacmanLogWriter{Writer: &buf}

	GlobalPacman = NewPacmanUI("test/repo")
	GlobalPacman.SetTTY(false)

	n, err := writer.Write([]byte("log message\n"))
	if err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if n == 0 {
		t.Errorf("expected non-zero bytes written")
	}
	if !strings.Contains(buf.String(), "log message") {
		t.Errorf("expected buffer to contain log message, got %q", buf.String())
	}
}

func TestPacmanLogWriter_NoDeadlockWhenTTY(t *testing.T) {
	var buf bytes.Buffer
	writer := PacmanLogWriter{Writer: &buf}

	GlobalPacman = NewPacmanUI("test/repo")
	GlobalPacman.SetTTY(true)
	defer func() {
		GlobalPacman = nil
	}()

	done := make(chan struct{})
	go func() {
		_, _ = writer.Write([]byte("tty log message\n"))
		close(done)
	}()

	select {
	case <-done:
		// Success: no deadlock!
	case <-time.After(2 * time.Second):
		t.Fatal("PacmanLogWriter deadlocked while writing on TTY!")
	}
}

func TestPacmanModel_ErrorQuits(t *testing.T) {
	m := NewPacmanModel("test/repo")
	model, cmd := m.Update(PacmanErrorMsg{Error: "something broke"})
	updated := model.(*PacmanModel)
	if !updated.HasError {
		t.Errorf("expected HasError = true")
	}
	if updated.ErrorMsg != "something broke" {
		t.Errorf("expected ErrorMsg = 'something broke', got %q", updated.ErrorMsg)
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd from PacmanErrorMsg")
	}
	// Verify cmd returns tea.QuitMsg
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestPacmanUI_ErrorStopsCleanly(t *testing.T) {
	p := NewPacmanUI("test/repo")
	p.SetTTY(false)
	GlobalPacman = p

	p.Error("critical failure")

	if GlobalPacman != nil {
		t.Errorf("expected GlobalPacman to be nil after Stop/Error, got %v", GlobalPacman)
	}
	if !p.stopped {
		t.Errorf("expected p.stopped = true")
	}
	if !p.Model().HasError {
		t.Errorf("expected model HasError = true")
	}
}
