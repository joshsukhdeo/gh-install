package ui

import (
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/harmonica"
	"golang.org/x/term"
)

// Ensure PacmanUI implements ProgressBar interface.
var _ ProgressBar = (*PacmanUI)(nil)

// Ensure PacmanModel implements tea.Model.
var _ tea.Model = (*PacmanModel)(nil)

// AssetInfo holds information about an asset being installed.
type AssetInfo struct {
	Name       string
	FullName   string
	Symlink    string
	InstallCmd string
	Completed  bool
}

// --- Bubble Tea v2 Messages ---

// PacmanTickMsg is sent on each animation frame.
type PacmanTickMsg struct{}

// PacmanProgressMsg updates installation progress stage and release metadata.
type PacmanProgressMsg struct {
	Stage     int
	Version   string
	Archive   string
	Asset     string
	Target    string
	GhostType string
}

// PacmanAddAssetMsg adds an asset to be tracked.
type PacmanAddAssetMsg struct {
	Asset AssetInfo
}

// PacmanSetAssetIndexMsg sets the active asset index.
type PacmanSetAssetIndexMsg struct {
	Index int
}

// PacmanSymlinkMsg updates symlink path for the active asset.
type PacmanSymlinkMsg struct {
	Symlink string
}

// PacmanInstallCmdMsg updates install command for the active asset.
type PacmanInstallCmdMsg struct {
	InstallCmd string
}

// PacmanCompleteMsg marks the animation as complete and glides to 100%.
type PacmanCompleteMsg struct {
	Message string
}

// PacmanErrorMsg indicates an error occurred during installation.
type PacmanErrorMsg struct {
	Error string
}

// PacmanPauseMsg pauses or resumes the animation.
type PacmanPauseMsg struct {
	Paused bool
}

// --- Bubble Tea v2 Model ---

// PacmanModel implements tea.Model for physics-interpolated pacman progress.
type PacmanModel struct {
	Repo         string
	Version      string
	Archive      string
	Target       string
	Assets       []AssetInfo
	CurrentAsset int
	DisableIcons bool

	// Progress state
	Stage           int
	ProgressTarget  float64 // Target progress [0.0, 1.0]
	ProgressCurrent float64 // Current smoothly interpolated progress [0.0, 1.0]
	ProgressVel     float64 // Velocity for harmonica spring
	Spring          harmonica.Spring
	TrackWidth      int // Number of dot positions on the screen

	// Animation lifecycle
	Frame     int
	Paused    bool
	Completed bool
	HasError  bool
	ErrorMsg  string
	StatusMsg string
	GhostType string

	// Terminal dimensions
	Width  int
	Height int

	// Styles
	pacmanStyle  lipgloss.Style
	dotStyle     lipgloss.Style
	headerStyle  lipgloss.Style
	successStyle lipgloss.Style
	errorStyle   lipgloss.Style
}

// NewPacmanModel creates a new pacman animation model.
func NewPacmanModel(repo string) *PacmanModel {
	return &PacmanModel{
		Repo:       repo,
		Version:    "?",
		Archive:    "?",
		Target:     "?",
		Assets:     []AssetInfo{},
		TrackWidth: 28,
		Spring:     harmonica.NewSpring(harmonica.FPS(60), 5.0, 1.0),
		Width:      80,
		Height:     24,
		pacmanStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("226")).
			Bold(true),
		dotStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("250")),
		headerStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			Bold(true),
		successStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")),
		errorStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")),
	}
}

func pacmanTickCmd() tea.Cmd {
	return tea.Tick(33*time.Millisecond, func(_ time.Time) tea.Msg {
		return PacmanTickMsg{}
	})
}

// Init implements tea.Model.
func (m *PacmanModel) Init() tea.Cmd {
	return pacmanTickCmd()
}

// Update implements tea.Model.
func (m *PacmanModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		if m.Width > 40 {
			m.TrackWidth = int(math.Min(float64(m.Width-30), 40))
		}

	case PacmanPauseMsg:
		m.Paused = msg.Paused

	case PacmanTickMsg:
		if m.Paused {
			return m, pacmanTickCmd()
		}

		m.Frame++

		// Harmonica spring physics smoothly interpolates ProgressCurrent toward ProgressTarget.
		m.ProgressCurrent, m.ProgressVel = m.Spring.Update(m.ProgressCurrent, m.ProgressVel, m.ProgressTarget)

		// Clamp within bounds
		if m.ProgressCurrent < 0 {
			m.ProgressCurrent = 0
			m.ProgressVel = 0
		}
		if m.ProgressCurrent > 1.0 {
			m.ProgressCurrent = 1.0
			m.ProgressVel = 0
		}

		// Snap to target if near convergence to prevent micro-jitter
		if math.Abs(m.ProgressTarget-m.ProgressCurrent) < 0.005 && math.Abs(m.ProgressVel) < 0.05 {
			m.ProgressCurrent = m.ProgressTarget
			m.ProgressVel = 0
		}

		// When complete and pacman has reached the destination, exit cleanly
		if m.Completed && m.ProgressCurrent >= 0.999 {
			return m, tea.Quit
		}

		return m, pacmanTickCmd()

	case PacmanProgressMsg:
		if msg.Version != "" {
			m.Version = msg.Version
		}
		if msg.Archive != "" {
			m.Archive = msg.Archive
		}
		if msg.Target != "" {
			m.Target = msg.Target
		}
		if msg.GhostType != "" {
			m.GhostType = msg.GhostType
		}
		if msg.Asset != "" && len(m.Assets) == 0 {
			m.Assets = []AssetInfo{{Name: msg.Asset}}
		}
		m.Stage = msg.Stage
		m.ProgressTarget = stageToProgress(m.Stage, m.CurrentAsset, len(m.Assets))

	case PacmanAddAssetMsg:
		m.Assets = append(m.Assets, msg.Asset)
		m.ProgressTarget = stageToProgress(m.Stage, m.CurrentAsset, len(m.Assets))

	case PacmanSetAssetIndexMsg:
		if msg.Index >= 0 && msg.Index < len(m.Assets) {
			m.CurrentAsset = msg.Index
		}
		m.ProgressTarget = stageToProgress(m.Stage, m.CurrentAsset, len(m.Assets))

	case PacmanSymlinkMsg:
		if len(m.Assets) > 0 && m.CurrentAsset < len(m.Assets) {
			m.Assets[m.CurrentAsset].Symlink = msg.Symlink
		}

	case PacmanInstallCmdMsg:
		if len(m.Assets) > 0 && m.CurrentAsset < len(m.Assets) {
			m.Assets[m.CurrentAsset].InstallCmd = msg.InstallCmd
		}

	case PacmanCompleteMsg:
		m.Completed = true
		m.ProgressTarget = 1.0
		if msg.Message != "" {
			m.StatusMsg = msg.Message
		}
		for i := range m.Assets {
			m.Assets[i].Completed = true
		}
		m.CurrentAsset = len(m.Assets)

	case PacmanErrorMsg:
		m.HasError = true
		m.ErrorMsg = msg.Error
	}

	return m, nil
}

// stageToProgress calculates the target progress fraction for a given installation stage.
func stageToProgress(stage int, currentAsset int, totalAssets int) float64 {
	var base float64
	switch {
	case stage <= 0:
		base = 0.08
	case stage == 1:
		base = 0.22
	case stage == 2:
		base = 0.40
	case stage == 3:
		base = 0.58
	case stage == 4:
		base = 0.75
	case stage == 5:
		base = 0.88
	default:
		base = 1.00
	}

	if totalAssets > 1 && stage >= 4 && stage < 6 {
		assetFrac := float64(currentAsset) / float64(totalAssets)
		base = 0.75 + (0.13 * assetFrac)
	}

	if base > 1.0 {
		base = 1.0
	}
	return base
}

// View implements tea.Model.
func (m *PacmanModel) View() tea.View {
	var sb strings.Builder

	// Header line
	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	// Pacman track line
	sb.WriteString(m.renderTrack())

	// Assets lines
	if len(m.Assets) > 0 {
		sb.WriteString("\n")
		sb.WriteString(m.renderAssets())
	}

	return tea.NewView(sb.String())
}

// renderHeader renders the repository metadata header.
func (m *PacmanModel) renderHeader() string {
	octopus := "🐙"
	tag := "🏷️"
	pkg := "📦"
	if m.DisableIcons {
		octopus = "R:"
		tag = "V:"
		pkg = "A:"
	}
	return fmt.Sprintf("%s %s      %s %s      %s %s",
		octopus, m.Repo, tag, m.Version, pkg, m.Archive)
}

// renderTrack renders the pacman eating dots progress track across the screen.
func (m *PacmanModel) renderTrack() string {
	trackWidth := m.TrackWidth
	if trackWidth <= 0 {
		trackWidth = 28
	}

	// Pacman slot along the track [0, trackWidth]
	pacPos := int(m.ProgressCurrent * float64(trackWidth))
	if pacPos < 0 {
		pacPos = 0
	}
	if pacPos > trackWidth {
		pacPos = trackWidth
	}

	var sb strings.Builder
	sb.WriteString("  ") // Left margin

	// Dots eaten behind pacman (empty space)
	for i := 0; i < pacPos; i++ {
		sb.WriteString("  ")
	}

	// Pacman at current position
	if pacPos < trackWidth {
		if m.DisableIcons {
			sb.WriteString("C ")
		} else {
			pacChar := "ᗧ"
			sb.WriteString(m.pacmanStyle.Render(pacChar) + " ")
		}

		// Uneaten dots ahead of pacman
		dotChar := m.dotStyle.Render("•") + " "
		if m.DisableIcons {
			dotChar = ". "
		}
		for i := pacPos + 1; i < trackWidth; i++ {
			sb.WriteString(dotChar)
		}
	} else {
		if m.DisableIcons {
			sb.WriteString("C ")
		} else {
			sb.WriteString(m.pacmanStyle.Render("ᗧ") + " ")
		}
	}

	// End target: cherry, flag, or ghost
	endIcon := "🍒"
	if m.DisableIcons {
		endIcon = "*"
	}
	if m.ProgressCurrent >= 0.999 || m.Completed {
		endIcon = "🏁"
		if m.DisableIcons {
			endIcon = "F"
		}
	}
	if m.HasError {
		endIcon = "👻"
		if m.DisableIcons {
			endIcon = "X"
		}
	}
	sb.WriteString(endIcon)

	// Percentage
	percent := int(m.ProgressCurrent * 100)
	if percent > 100 {
		percent = 100
	}
	fmt.Fprintf(&sb, " %3d%%", percent)

	if m.GhostType != "" && !m.DisableIcons {
		fmt.Fprintf(&sb, " [%s]", m.GhostType)
	}
	if m.HasError && m.ErrorMsg != "" {
		sb.WriteString(m.errorStyle.Render(fmt.Sprintf(" (%s)", m.ErrorMsg)))
	}

	return sb.String()
}

// renderAssets renders the list of discovered and installed assets.
func (m *PacmanModel) renderAssets() string {
	var sb strings.Builder
	cherry := "🍒"
	flag := "🏁"
	link := "🔗"
	pacman := "ᗧ"
	dot := "•"

	if m.DisableIcons {
		cherry = "*"
		flag = "F"
		link = "L"
		pacman = "C"
		dot = "."
	}

	for i, asset := range m.Assets {
		if i > 0 {
			sb.WriteString("\n")
		}

		if asset.Completed {
			name := asset.Name
			targetInfo := asset.FullName
			if targetInfo == "" {
				targetInfo = asset.InstallCmd
			}
			line := fmt.Sprintf("    %s %s %s %s", cherry, name, flag, targetInfo)
			if asset.Symlink != "" {
				line += fmt.Sprintf(" <--%s %s", link, asset.Symlink)
			}
			sb.WriteString(line)
		} else if i == m.CurrentAsset {
			fmt.Fprintf(&sb, "    %s %s %s %s", pacman, dot, dot, asset.Name)
		} else {
			fmt.Fprintf(&sb, "    %s %s %s %s", dot, dot, dot, asset.Name)
		}
	}

	return sb.String()
}

// --- PacmanUI Orchestrator ---

// PacmanUI orchestrates the Bubble Tea v2 pacman progress animation.
type PacmanUI struct {
	Repo    string
	Version string
	Archive string
	Target  string

	Assets       []AssetInfo
	CurrentAsset int
	DisableIcons bool

	model   *PacmanModel
	program *tea.Program
	isTTY   bool
	output  io.Writer
	mu      sync.Mutex

	stopCh        chan struct{}
	animationDone chan struct{}
	started       bool
	stopped       bool
	paused        bool
	headerPrinted bool

	// Debug mode
	Debug    bool
	debugLog []string
}

// GlobalPacman provides a singleton pointer for logging interceptors.
var GlobalPacman *PacmanUI

// NewPacmanUI creates a new PacmanUI orchestrator for a given repository.
func NewPacmanUI(repo string) *PacmanUI {
	isTTY := term.IsTerminal(int(os.Stdout.Fd()))
	model := NewPacmanModel(repo)
	p := &PacmanUI{
		Repo:          repo,
		Version:       "?",
		Archive:       "?",
		Target:        "?",
		model:         model,
		isTTY:         isTTY,
		output:        os.Stdout,
		stopCh:        make(chan struct{}),
		animationDone: make(chan struct{}),
		Debug:         os.Getenv("GH_PT_DEBUG_PACMAN") == "1",
	}
	return p
}

// Model returns the underlying Bubble Tea model.
func (p *PacmanUI) Model() *PacmanModel {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.model
}

// SetTTY forces TTY mode setting (for testing purposes).
func (p *PacmanUI) SetTTY(isTTY bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.isTTY = isTTY
}

// SetOutput configures where output is written.
func (p *PacmanUI) SetOutput(w io.Writer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.output = w
}

// Start begins the animation program.
func (p *PacmanUI) Start() {
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return
	}
	p.started = true
	p.model.DisableIcons = p.DisableIcons

	slog.Debug("pacman animation starting",
		"repo", p.Repo,
		"version", p.Version,
		"archive", p.Archive,
		"tty", p.isTTY,
	)

	// In non-interactive mode, print the header once and avoid TUI renderer
	if !p.isTTY {
		if !p.headerPrinted {
			var sb strings.Builder
			p.renderHeader(&sb)
			if p.output != nil {
				_, _ = fmt.Fprintln(p.output, sb.String())
			} else {
				fmt.Println(sb.String())
			}
			p.headerPrinted = true
		}
		p.mu.Unlock()
		return
	}

	p.program = tea.NewProgram(
		p.model,
		tea.WithOutput(p.output),
		tea.WithoutSignalHandler(),
	)
	p.mu.Unlock()

	go func() {
		defer p.signalAnimationDone()
		if p.program != nil {
			_, _ = p.program.Run()
		}
	}()
}

// Stop cleanly finishes the animation.
func (p *PacmanUI) Stop() {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	p.stopped = true
	p.paused = false
	p.completeAllAnimations()

	// In non-TTY mode, print the completed assets
	if !p.isTTY {
		var sb strings.Builder
		p.renderAssets(&sb)
		if sb.Len() > 0 {
			if p.output != nil {
				_, _ = fmt.Fprintln(p.output, sb.String())
			} else {
				fmt.Println(sb.String())
			}
		}
		p.mu.Unlock()
		p.signalAnimationDone()
		return
	}

	prog := p.program
	p.mu.Unlock()

	if prog != nil {
		prog.Quit()
	}
	p.signalAnimationDone()
}

// WaitForAnimation waits for the animation to glide to completion.
func (p *PacmanUI) WaitForAnimation() {
	p.mu.Lock()
	if !p.isTTY {
		p.mu.Unlock()
		return
	}
	if p.program != nil && p.started && !p.stopped {
		p.program.Send(PacmanCompleteMsg{Message: "Complete"})
	}
	p.mu.Unlock()

	select {
	case <-p.animationDone:
	case <-time.After(1500 * time.Millisecond):
		p.Stop()
	}
}

// Pause pauses the animation.
func (p *PacmanUI) Pause() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.paused = true
	if p.isTTY && p.output != nil {
		_, _ = fmt.Fprint(p.output, "\r\033[K")
	}
	if p.program != nil {
		p.program.Send(PacmanPauseMsg{Paused: true})
	} else if p.model != nil {
		p.model.Update(PacmanPauseMsg{Paused: true})
	}
}

// Resume resumes the animation.
func (p *PacmanUI) Resume() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.paused = false
	if p.program != nil {
		p.program.Send(PacmanPauseMsg{Paused: false})
	} else if p.model != nil {
		p.model.Update(PacmanPauseMsg{Paused: false})
	}
}

// Update updates installation stage and release info.
func (p *PacmanUI) Update(stage int, version, archive, asset, target, ghostType string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if version != "" {
		p.Version = version
	}
	if archive != "" {
		p.Archive = archive
	}
	if target != "" {
		p.Target = target
	}
	if asset != "" && len(p.Assets) == 0 {
		p.Assets = []AssetInfo{{Name: asset}}
	}

	msg := PacmanProgressMsg{
		Stage:     stage,
		Version:   version,
		Archive:   archive,
		Asset:     asset,
		Target:    target,
		GhostType: ghostType,
	}

	if p.program != nil && p.started && !p.stopped {
		p.program.Send(msg)
	} else if p.model != nil {
		p.model.Update(msg)
	}
}

// AddAsset adds an asset to be tracked.
func (p *PacmanUI) AddAsset(name, fullName, symlink, installCmd string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	asset := AssetInfo{
		Name:       name,
		FullName:   fullName,
		Symlink:    symlink,
		InstallCmd: installCmd,
		Completed:  false,
	}
	p.Assets = append(p.Assets, asset)

	msg := PacmanAddAssetMsg{Asset: asset}
	if p.program != nil && p.started && !p.stopped {
		p.program.Send(msg)
	} else if p.model != nil {
		p.model.Update(msg)
	}
}

// SetCurrentAsset sets the current asset being processed.
func (p *PacmanUI) SetCurrentAsset(index int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.CurrentAsset = index
	msg := PacmanSetAssetIndexMsg{Index: index}
	if p.program != nil && p.started && !p.stopped {
		p.program.Send(msg)
	} else if p.model != nil {
		p.model.Update(msg)
	}
}

// UpdateSymlink updates the symlink path for the current asset.
func (p *PacmanUI) UpdateSymlink(symlink string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.Assets) > 0 && p.CurrentAsset < len(p.Assets) {
		p.Assets[p.CurrentAsset].Symlink = symlink
	}

	msg := PacmanSymlinkMsg{Symlink: symlink}
	if p.program != nil && p.started && !p.stopped {
		p.program.Send(msg)
	} else if p.model != nil {
		p.model.Update(msg)
	}
}

// SetAssetInstallCmd sets the install command for the current asset.
func (p *PacmanUI) SetAssetInstallCmd(installCmd string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.Assets) > 0 && p.CurrentAsset < len(p.Assets) {
		p.Assets[p.CurrentAsset].InstallCmd = installCmd
	}

	msg := PacmanInstallCmdMsg{InstallCmd: installCmd}
	if p.program != nil && p.started && !p.stopped {
		p.program.Send(msg)
	} else if p.model != nil {
		p.model.Update(msg)
	}
}

// Success signals successful installation.
func (p *PacmanUI) Success(msg ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	status := "Done"
	if len(msg) > 0 {
		status = strings.Join(msg, " ")
	}

	p.completeAllAnimations()

	completeMsg := PacmanCompleteMsg{Message: status}
	if p.program != nil && p.started && !p.stopped {
		p.program.Send(completeMsg)
	} else if p.model != nil {
		p.model.Update(completeMsg)
	}
}

// Error signals an installation error.
func (p *PacmanUI) Error(args ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()

	errMsg := "Error"
	if len(args) > 0 {
		errMsg = fmt.Sprint(args...)
	}

	errMsgObj := PacmanErrorMsg{Error: errMsg}
	if p.program != nil && p.started && !p.stopped {
		p.program.Send(errMsgObj)
	} else if p.model != nil {
		p.model.Update(errMsgObj)
	}
}

// Finish finishes the progress bar (alias for Stop).
func (p *PacmanUI) Finish() {
	p.Stop()
}

// View returns the rendered string of the UI.
func (p *PacmanUI) View() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.model != nil {
		return p.model.View().Content
	}
	return ""
}

func (p *PacmanUI) completeAllAnimations() {
	for i := range p.Assets {
		p.Assets[i].Completed = true
	}
	p.CurrentAsset = len(p.Assets)
	if p.model != nil {
		for i := range p.model.Assets {
			p.model.Assets[i].Completed = true
		}
		p.model.CurrentAsset = len(p.model.Assets)
		p.model.ProgressTarget = 1.0
	}
}

func (p *PacmanUI) signalAnimationDone() {
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.animationDone:
	default:
		close(p.animationDone)
	}
}

func (p *PacmanUI) renderHeader(sb *strings.Builder) {
	if p.model != nil {
		sb.WriteString(p.model.renderHeader())
	}
}

func (p *PacmanUI) renderAssets(sb *strings.Builder) {
	if p.model != nil {
		sb.WriteString(p.model.renderAssets())
	}
}

// DumpDebugLog returns the debug log.
func (p *PacmanUI) DumpDebugLog() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]string, len(p.debugLog))
	copy(result, p.debugLog)
	return result
}

// StateSnapshot returns a string representation of the current state.
func (p *PacmanUI) StateSnapshot() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return fmt.Sprintf("PacmanUI: Repo=%s, Version=%s, Target=%.2f, Current=%.2f",
		p.Repo, p.Version, p.model.ProgressTarget, p.model.ProgressCurrent)
}

// --- Logging Interceptor ---

// PacmanLogWriter wraps an io.Writer to ensure log lines don't garble terminal output.
type PacmanLogWriter struct {
	io.Writer
}

func (w PacmanLogWriter) Write(p []byte) (int, error) {
	if GlobalPacman != nil {
		GlobalPacman.mu.Lock()
		defer GlobalPacman.mu.Unlock()
		if !GlobalPacman.paused && GlobalPacman.isTTY {
			if len(GlobalPacman.Assets) > 0 {
				fmt.Printf("\033[%dA\033[J", len(GlobalPacman.Assets)+2)
			} else {
				fmt.Print("\r\033[K")
			}
		}
	}
	n, err := w.Writer.Write(p)
	if GlobalPacman != nil && !GlobalPacman.paused && GlobalPacman.isTTY {
		view := GlobalPacman.View()
		if view != "" {
			fmt.Print(view)
		}
	}
	return n, err
}
