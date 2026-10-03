package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestModel_InitialState(t *testing.T) {
	m := NewModel("test/repo")

	if m.Repo != "test/repo" {
		t.Errorf("expected Repo to be 'test/repo', got %q", m.Repo)
	}
	if m.Version != "?" {
		t.Errorf("expected Version to be '?', got %q", m.Version)
	}
	if m.Archive != "?" {
		t.Errorf("expected Archive to be '?', got %q", m.Archive)
	}
	if m.phase != PhaseHeader {
		t.Errorf("expected initial phase to be PhaseHeader, got %v", m.phase)
	}
	if m.headerEaten != 0 {
		t.Errorf("expected headerEaten to be 0, got %d", m.headerEaten)
	}
	if len(m.Assets) != 0 {
		t.Errorf("expected Assets to be empty, got %d items", len(m.Assets))
	}
}

func TestModel_Init(t *testing.T) {
	m := NewModel("test/repo")
	cmd := m.Init()

	if cmd != nil {
		t.Error("expected Init() to return nil command")
	}
}

func TestModel_PhaseTransitions_HeaderToHorizontal(t *testing.T) {
	m := NewModel("test/repo")
	m.phase = PhaseHeader
	m.headerEaten = 3 // One more tick should transition

	// Simulate tick
	model, _ := m.Update(TickMsg{})
	m = model.(*Model)

	if m.phase != PhaseHeader {
		t.Errorf("expected to still be in PhaseHeader, got %v", m.phase)
	}
	if m.headerEaten != 4 {
		t.Errorf("expected headerEaten to be 4, got %d", m.headerEaten)
	}

	// Another tick should transition to PhaseHorizontal
	model, _ = m.Update(TickMsg{})
	m = model.(*Model)

	if m.phase != PhaseHorizontal {
		t.Errorf("expected to transition to PhaseHorizontal, got %v", m.phase)
	}
}

func TestModel_PhaseTransitions_HorizontalToAssets(t *testing.T) {
	m := NewModel("test/repo")
	m.phase = PhaseHorizontal
	m.Assets = []AssetInfo{
		{Name: "gh-pt", FullName: "/usr/local/bin/gh-pt"},
	}

	// Simulate tick
	model, _ := m.Update(TickMsg{})
	m = model.(*Model)

	if m.phase != PhaseAssets {
		t.Errorf("expected to transition to PhaseAssets, got %v", m.phase)
	}
	if m.currentAsset != 0 {
		t.Errorf("expected currentAsset to be 0, got %d", m.currentAsset)
	}
}

func TestModel_AssetCompletion(t *testing.T) {
	m := NewModel("test/repo")
	m.phase = PhaseAssets
	m.Assets = []AssetInfo{
		{Name: "asset1", FullName: "/path/asset1"},
		{Name: "asset2", FullName: "/path/asset2"},
	}

	// First asset should complete after 3 ticks
	for i := 0; i < 3; i++ {
		model, _ := m.Update(TickMsg{})
		m = model.(*Model)
	}

	if !m.Assets[0].Completed {
		t.Error("expected first asset to be completed after 3 ticks")
	}
	if m.currentAsset != 1 {
		t.Errorf("expected currentAsset to be 1, got %d", m.currentAsset)
	}

	// Second asset should complete after 3 more ticks
	for i := 0; i < 3; i++ {
		model, _ := m.Update(TickMsg{})
		m = model.(*Model)
	}

	if !m.Assets[1].Completed {
		t.Error("expected second asset to be completed after 3 more ticks")
	}
	if m.currentAsset != 2 {
		t.Errorf("expected currentAsset to be 2, got %d", m.currentAsset)
	}
}

func TestModel_AssetResolvedMessage(t *testing.T) {
	m := NewModel("test/repo")
	m.phase = PhaseAssets
	m.Assets = []AssetInfo{
		{Name: "?"},
	}

	msg := AssetResolvedMsg{
		Name:       "gh-pt",
		FullName:   "/usr/local/bin/gh-pt",
		Symlink:    "/usr/bin/gh-pt",
		InstallCmd: "sudo apt install gh-pt",
	}

	model, _ := m.Update(msg)
	m = model.(*Model)

	if m.Assets[0].Name != "gh-pt" {
		t.Errorf("expected asset name to be 'gh-pt', got %q", m.Assets[0].Name)
	}
	if m.Assets[0].FullName != "/usr/local/bin/gh-pt" {
		t.Errorf("expected asset FullName to be '/usr/local/bin/gh-pt', got %q", m.Assets[0].FullName)
	}
	if m.Assets[0].Symlink != "/usr/bin/gh-pt" {
		t.Errorf("expected asset Symlink to be '/usr/bin/gh-pt', got %q", m.Assets[0].Symlink)
	}
	if m.Assets[0].InstallCmd != "sudo apt install gh-pt" {
		t.Errorf("expected asset InstallCmd to be 'sudo apt install gh-pt', got %q", m.Assets[0].InstallCmd)
	}
}

func TestModel_AssetCompletedMessage(t *testing.T) {
	m := NewModel("test/repo")
	m.phase = PhaseAssets
	m.Assets = []AssetInfo{
		{Name: "asset1", Completed: false},
		{Name: "asset2", Completed: false},
	}
	m.currentAsset = 0

	model, _ := m.Update(AssetCompletedMsg{})
	m = model.(*Model)

	if !m.Assets[0].Completed {
		t.Error("expected first asset to be completed")
	}
	if m.currentAsset != 1 {
		t.Errorf("expected currentAsset to be 1, got %d", m.currentAsset)
	}
}

func TestModel_WindowSizeMessage(t *testing.T) {
	m := NewModel("test/repo")

	msg := tea.WindowSizeMsg{
		Width:  120,
		Height: 40,
	}

	model, _ := m.Update(msg)
	m = model.(*Model)

	if m.width != 120 {
		t.Errorf("expected width to be 120, got %d", m.width)
	}
	if m.height != 40 {
		t.Errorf("expected height to be 40, got %d", m.height)
	}
}

func TestModel_KeyboardQuit(t *testing.T) {
	m := NewModel("test/repo")

	// Test 'q' key
	model, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	m = model.(*Model)

	if cmd == nil {
		t.Error("expected quit command for 'q' key")
	}

	// Test 'esc' key
	model, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	_ = model.(*Model)

	if cmd == nil {
		t.Error("expected quit command for 'esc' key")
	}

	// Note: ctrl+c handling depends on terminal and bubbletea version
	// Skipping explicit test for now as it requires proper KeyMsg construction
}

func TestModel_View_HeaderPhase(t *testing.T) {
	m := NewModel("test/repo")
	m.Version = "v1.0.0"
	m.Archive = "test.tar.gz"
	m.phase = PhaseHeader
	m.headerEaten = 2

	view := m.View()

	// Should contain header elements
	if !strings.Contains(view.Content, "test/repo") {
		t.Error("expected view to contain repo name")
	}
	if !strings.Contains(view.Content, "v1.0.0") {
		t.Error("expected view to contain version")
	}
	if !strings.Contains(view.Content, "test.tar.gz") {
		t.Error("expected view to contain archive name")
	}
}
func TestModel_View_HorizontalPhase(t *testing.T) {
	m := NewModel("test/repo")
	m.phase = PhaseHorizontal
	m.horizontalPos = 10
	m.width = 80

	view := m.View()

	// Should contain header
	if !strings.Contains(view.Content, "test/repo") {
		t.Error("expected view to contain repo name")
	}

	// View should not be empty
	if len(view.Content) == 0 {
		t.Error("expected view to have content")
	}
}

func TestModel_View_AssetsPhase(t *testing.T) {
	m := NewModel("test/repo")
	m.phase = PhaseAssets
	m.Assets = []AssetInfo{
		{Name: "asset1", FullName: "/path/asset1", Completed: false},
		{Name: "asset2", FullName: "/path/asset2", Completed: true},
	}
	m.currentAsset = 0

	view := m.View()

	// Should contain asset names
	if !strings.Contains(view.Content, "asset1") {
		t.Error("expected view to contain asset1")
	}
	if !strings.Contains(view.Content, "asset2") {
		t.Error("expected view to contain asset2")
	}

	// Should contain completion indicators
	if !strings.Contains(view.Content, "/path/asset2") {
		t.Error("expected view to contain completed asset path")
	}
}

func TestModel_HorizontalMovement(t *testing.T) {
	m := NewModel("test/repo")
	m.phase = PhaseHorizontal
	m.width = 20

	// Simulate multiple ticks
	for i := 0; i < 25; i++ {
		model, _ := m.Update(TickMsg{})
		m = model.(*Model)
	}

	// Should wrap around
	if m.horizontalPos >= float64(m.width) {
		t.Errorf("expected horizontalPos to wrap, got %f (width=%d)", m.horizontalPos, m.width)
	}
}

func TestModel_NoPhaseTransitionWithoutAssets(t *testing.T) {
	m := NewModel("test/repo")
	m.phase = PhaseHorizontal
	m.Assets = []AssetInfo{} // No assets

	// Simulate multiple ticks
	for i := 0; i < 10; i++ {
		model, _ := m.Update(TickMsg{})
		m = model.(*Model)
	}

	// Should stay in PhaseHorizontal
	if m.phase != PhaseHorizontal {
		t.Errorf("expected to stay in PhaseHorizontal without assets, got %v", m.phase)
	}
}

func TestModel_AssetCompletionBoundary(t *testing.T) {
	m := NewModel("test/repo")
	m.phase = PhaseAssets
	m.Assets = []AssetInfo{
		{Name: "asset1", Completed: false},
	}
	m.currentAsset = 0

	// Complete the only asset
	for i := 0; i < 3; i++ {
		model, _ := m.Update(TickMsg{})
		m = model.(*Model)
	}

	// Should not panic or go out of bounds
	if m.currentAsset != 1 {
		t.Errorf("expected currentAsset to be 1, got %d", m.currentAsset)
	}

	// Additional ticks should not cause issues
	for i := 0; i < 5; i++ {
		model, _ := m.Update(TickMsg{})
		m = model.(*Model)
	}
}

func TestModel_RenderMethods(t *testing.T) {
	m := NewModel("test/repo")
	m.Version = "v1.0.0"
	m.Archive = "test.tar.gz"

	// Test renderHeader
	header := m.renderHeader()
	if !strings.Contains(header, "test/repo") {
		t.Error("renderHeader should contain repo name")
	}
	if !strings.Contains(header, "v1.0.0") {
		t.Error("renderHeader should contain version")
	}

	// Test renderHeaderAnimation
	m.phase = PhaseHeader
	m.headerEaten = 2
	headerAnim := m.renderHeaderAnimation()
	if len(headerAnim) == 0 {
		t.Error("renderHeaderAnimation should produce output")
	}

	// Test renderHorizontalAnimation
	m.phase = PhaseHorizontal
	m.horizontalPos = 5
	horizAnim := m.renderHorizontalAnimation()
	if len(horizAnim) == 0 {
		t.Error("renderHorizontalAnimation should produce output")
	}

	// Test renderAssets
	m.phase = PhaseAssets
	m.Assets = []AssetInfo{
		{Name: "test", FullName: "/path/test", Completed: true},
	}
	assetsView := m.renderAssets()
	if !strings.Contains(assetsView, "test") {
		t.Error("renderAssets should contain asset name")
	}
}
