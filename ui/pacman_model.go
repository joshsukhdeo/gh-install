package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/harmonica"
)

// Phase represents the current animation phase
type Phase int

const (
	PhaseHeader Phase = iota
	PhaseHorizontal
	PhaseAssets
)

// Model implements bubbletea.Model for the pacman animation
type Model struct {
	// Configuration
	Repo    string
	Version string
	Archive string
	Target  string
	Assets  []AssetInfo

	// Animation state
	phase         Phase
	headerEaten   int
	horizontalPos float64
	currentAsset  int
	dotsEaten     int

	// Physics
	spring harmonica.Spring

	// Terminal
	width  int
	height int
	isTTY  bool

	// Styling
	pacmanStyle lipgloss.Style
	dotStyle    lipgloss.Style
	headerStyle lipgloss.Style
}

// NewModel creates a new pacman animation model
func NewModel(repo string) *Model {
	return &Model{
		Repo:    repo,
		Version: "?",
		Archive: "?",
		Target:  "?",
		Assets:  []AssetInfo{},
		phase:   PhaseHeader,
		spring:  harmonica.NewSpring(harmonica.FPS(60), 6.0, 0.5),
		width:   80,
		height:  24,
		isTTY:   true,
		pacmanStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("226")). // yellow
			Bold(true),
		dotStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("255")),
		headerStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")). // blue
			Bold(true),
	}
}

// Init implements tea.Model
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case TickMsg:
		return m.updateAnimation()

	case AssetResolvedMsg:
		if len(m.Assets) > 0 && m.currentAsset < len(m.Assets) {
			m.Assets[m.currentAsset].Name = msg.Name
			m.Assets[m.currentAsset].FullName = msg.FullName
			m.Assets[m.currentAsset].Symlink = msg.Symlink
			m.Assets[m.currentAsset].InstallCmd = msg.InstallCmd
		}
		return m, nil

	case AssetCompletedMsg:
		if m.currentAsset < len(m.Assets) {
			m.Assets[m.currentAsset].Completed = true
			m.currentAsset++
			m.dotsEaten = 0
		}
		return m, nil
	}

	return m, nil
}

// updateAnimation advances the animation state
func (m *Model) updateAnimation() (tea.Model, tea.Cmd) {
	switch m.phase {
	case PhaseHeader:
		if m.headerEaten < 4 {
			m.headerEaten++
		} else {
			m.phase = PhaseHorizontal
			m.horizontalPos = 0
		}

	case PhaseHorizontal:
		m.horizontalPos++
		if m.horizontalPos >= float64(m.width) {
			m.horizontalPos = 0
		}
		// Check if assets are resolved
		if len(m.Assets) > 0 && m.Assets[0].Name != "?" {
			m.phase = PhaseAssets
			m.currentAsset = 0
			m.dotsEaten = 0
		}

	case PhaseAssets:
		if m.currentAsset < len(m.Assets) {
			if !m.Assets[m.currentAsset].Completed {
				m.dotsEaten++
				if m.dotsEaten >= 3 {
					m.Assets[m.currentAsset].Completed = true
					m.currentAsset++
					m.dotsEaten = 0
				}
			}
		}
	}

	return m, Tick()
}

// View implements tea.Model
func (m *Model) View() tea.View {
	var sb strings.Builder

	// Header
	header := m.renderHeader()
	sb.WriteString(header)
	sb.WriteString("\n")

	// Assets or animation
	if m.phase == PhaseAssets {
		sb.WriteString(m.renderAssets())
	} else {
		sb.WriteString(m.renderAnimation())
	}

	return tea.NewView(sb.String())
}

// renderHeader renders the header line
func (m *Model) renderHeader() string {
	return m.headerStyle.Render(fmt.Sprintf("🐙 %s      🏷️ %s      📦 %s",
		m.Repo, m.Version, m.Archive))
}

// renderAnimation renders the current animation phase
func (m *Model) renderAnimation() string {
	switch m.phase {
	case PhaseHeader:
		return m.renderHeaderAnimation()
	case PhaseHorizontal:
		return m.renderHorizontalAnimation()
	default:
		return ""
	}
}

// renderHeaderAnimation renders the header eating animation
func (m *Model) renderHeaderAnimation() string {
	var sb strings.Builder
	pacman := m.pacmanStyle.Render("ᗧ")
	dot := m.dotStyle.Render("•")

	sb.WriteString(pacman)

	elements := []struct {
		icon string
		text string
	}{
		{"🐙", m.Repo},
		{"🏷️", m.Version},
		{"📦", m.Archive},
		{"🍒", "?"},
	}

	for i, elem := range elements {
		if i > 0 {
			fmt.Fprintf(&sb, " %s %s %s ", dot, dot, dot)
		}

		if i < m.headerEaten {
			fmt.Fprintf(&sb, "%s %s", elem.icon, elem.text)
		} else if i == m.headerEaten {
			eaten := m.dotsEaten
			if eaten > 3 {
				eaten = 3
			}
			sb.WriteString(strings.Repeat("  ", eaten))
			sb.WriteString(pacman)
			rem := 3 - eaten
			for j := 0; j < rem; j++ {
				sb.WriteString(dot)
			}
			fmt.Fprintf(&sb, " %s %s", elem.icon, elem.text)
			break
		} else {
			fmt.Fprintf(&sb, "%s %s %s ", dot, dot, dot)
			fmt.Fprintf(&sb, "%s %s", elem.icon, elem.text)
		}
	}

	return sb.String()
}

// renderHorizontalAnimation renders the horizontal movement animation
func (m *Model) renderHorizontalAnimation() string {
	var sb strings.Builder
	pacman := m.pacmanStyle.Render("ᗧ")
	dot := m.dotStyle.Render("•")

	pos := int(m.horizontalPos)
	for i := 0; i < pos; i++ {
		if i%2 == 0 {
			sb.WriteString(" ")
		} else {
			sb.WriteString(dot)
		}
	}

	sb.WriteString(pacman)

	remaining := m.width - pos - 2
	for i := 0; i < remaining && i < 20; i++ {
		if i%2 == 0 {
			sb.WriteString(dot)
		} else {
			sb.WriteString(" ")
		}
	}

	sb.WriteString(" 🍒 ?")

	return sb.String()
}

// renderAssets renders the asset list
func (m *Model) renderAssets() string {
	var sb strings.Builder
	pacman := m.pacmanStyle.Render("ᗧ")
	dot := m.dotStyle.Render("•")
	cherry := "🍒"
	flag := "🏁"
	link := "🔗"

	for i, asset := range m.Assets {
		if i > 0 {
			sb.WriteString("\n")
		}

		assetPart := fmt.Sprintf("%s %s", cherry, asset.Name)

		finishPart := ""
		if asset.Completed {
			if asset.FullName != "" {
				finishPart = fmt.Sprintf("%s %s", flag, asset.FullName)
			} else if asset.InstallCmd != "" {
				finishPart = fmt.Sprintf("%s %s", flag, asset.InstallCmd)
			}

			if asset.Symlink != "" {
				finishPart += fmt.Sprintf(" <--%s %s", link, asset.Symlink)
			}
		}

		prefix := ""
		if i < m.currentAsset {
			prefix = "    "
		} else if i == m.currentAsset {
			prefix = ""
		} else {
			prefix = "    "
		}

		sb.WriteString(prefix)

		if i < m.currentAsset {
			sb.WriteString("       ")
		} else if i == m.currentAsset {
			eaten := m.dotsEaten
			if eaten > 3 {
				eaten = 3
			}
			sb.WriteString(strings.Repeat("  ", eaten))
			sb.WriteString(pacman)
			rem := 3 - eaten
			for j := 0; j < rem; j++ {
				sb.WriteString(dot)
			}
			sb.WriteString(" ")
		} else {
			fmt.Fprintf(&sb, "%s%s%s ", dot, dot, dot)
		}

		sb.WriteString(assetPart)

		if asset.Completed && finishPart != "" {
			sb.WriteString(" " + finishPart)
		}
	}

	return sb.String()
}

// Messages

// TickMsg signals an animation tick
type TickMsg struct{}

// Tick returns a command that sends a TickMsg after a delay
func Tick() tea.Cmd {
	return tea.Tick(time.Millisecond*250, func(_ time.Time) tea.Msg {
		return TickMsg{}
	})
}

// AssetResolvedMsg signals that an asset has been resolved
type AssetResolvedMsg struct {
	Name       string
	FullName   string
	Symlink    string
	InstallCmd string
}

// AssetCompletedMsg signals that an asset has been completed
type AssetCompletedMsg struct{}
