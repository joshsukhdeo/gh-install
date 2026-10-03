package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/log"
	"github.com/mattn/go-runewidth"
	"github.com/pterm/pterm"
	"golang.org/x/term"
)

// tableWrapWidths are the per-cell wrap widths tried (widest first) when
// fitting a table into the terminal. 0 means no wrapping.
var tableWrapWidths = []int{0, 40, 30, 20}

// renderState prints state rows (data[0] is the header) in the requested format.
func renderState(data pterm.TableData, format string) error {
	width := terminalWidth()
	switch format {
	case "table":
		fmt.Println(renderTable(data, 40))
		return nil
	case "list":
		fmt.Print(renderList(data, width))
		return nil
	case "tui":
		if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
			log.Warn("--format tui requires an interactive terminal; falling back to auto")
			break
		}
		return runStateTUI(data)
	}

	// auto
	fmt.Print(autoRender(data, width))
	return nil
}

// autoRender returns the widest table that fits within width, or a list if
// no table fits. width <= 0 (unknown, e.g. piped output) always yields a table.
func autoRender(data pterm.TableData, width int) string {
	if width <= 0 {
		return renderTable(data, 40) + "\n"
	}
	for _, wrap := range tableWrapWidths {
		if s := renderTable(data, wrap); renderedWidth(s) <= width {
			return s + "\n"
		}
	}
	return renderList(data, width)
}

// terminalWidth returns the stdout terminal width, falling back to $COLUMNS,
// or 0 when unknown.
func terminalWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		return w
	}
	if w, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && w > 0 {
		return w
	}
	return 0
}

// renderedWidth is the display width of the widest line in s, ignoring ANSI codes.
func renderedWidth(s string) int {
	maxW := 0
	for _, line := range strings.Split(s, "\n") {
		if w := runewidth.StringWidth(pterm.RemoveColorFromString(line)); w > maxW {
			maxW = w
		}
	}
	return maxW
}

func renderTable(data pterm.TableData, wrap int) string {
	rows := make(pterm.TableData, len(data))
	for i, row := range data {
		rows[i] = make([]string, len(row))
		for j, cell := range row {
			if i > 0 && j > 0 {
				cell = wrapCell(cell, wrap)
			}
			rows[i][j] = cell
		}
	}
	s, err := pterm.DefaultTable.WithHasHeader().WithBoxed().WithData(fixEmojiPadding(rows)).Srender()
	if err != nil {
		log.Warn("failed to render table", "error", err)
	}
	return s
}

// renderList prints one block per row: the first column as a title, then
// "Header: value" lines, skipping empty values. Values wrap to fit width.
func renderList(data pterm.TableData, width int) string {
	if len(data) < 2 {
		return ""
	}
	headers := data[0]
	labelW := 0
	for _, h := range headers[1:] {
		labelW = max(labelW, runewidth.StringWidth(h))
	}
	valueW := 0
	if width > 0 {
		valueW = max(width-labelW-4, 20)
	}

	var sb strings.Builder
	for i, row := range data[1:] {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(pterm.Bold.Sprint(pterm.LightCyan(row[0])) + "\n")
		for j := 1; j < len(row) && j < len(headers); j++ {
			v := row[j]
			if v == "" || v == "-" || v == "N/A" {
				continue
			}
			pad := strings.Repeat(" ", labelW-runewidth.StringWidth(headers[j]))
			indent := strings.Repeat(" ", labelW+4)
			lines := strings.Split(wrapCell(v, valueW), "\n")
			fmt.Fprintf(&sb, "  %s%s  %s\n", pterm.Gray(headers[j]), pad, lines[0])
			for _, l := range lines[1:] {
				sb.WriteString(indent + l + "\n")
			}
		}
	}
	return sb.String()
}

// stateTUI is an interactive table with a detail pane for the selected row.
type stateTUI struct {
	table   table.Model
	headers []string
	rows    []table.Row
	width   int
}

func runStateTUI(data pterm.TableData) error {
	if len(data) < 2 {
		pterm.Info.Println("Nothing to display.")
		return nil
	}
	headers := data[0]
	cols := make([]table.Column, len(headers))
	for j, h := range headers {
		w := runewidth.StringWidth(h)
		for _, row := range data[1:] {
			w = max(w, runewidth.StringWidth(row[j]))
		}
		cols[j] = table.Column{Title: h, Width: min(w, 40)}
	}
	rows := make([]table.Row, len(data)-1)
	for i, row := range data[1:] {
		rows[i] = table.Row(row)
	}

	t := table.New(table.WithColumns(cols), table.WithRows(rows), table.WithFocused(true), table.WithHeight(min(len(rows), 15)))
	styles := table.DefaultStyles()
	styles.Header = styles.Header.Bold(true).BorderStyle(lipgloss.NormalBorder()).BorderBottom(true)
	styles.Selected = styles.Selected.Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57")).Bold(true)
	t.SetStyles(styles)

	_, err := tea.NewProgram(stateTUI{table: t, headers: headers, rows: rows}).Run()
	return err
}

func (m stateTUI) Init() tea.Cmd { return nil }

func (m stateTUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.table.SetWidth(msg.Width)
		m.table.SetHeight(max(min(len(m.rows), msg.Height-len(m.headers)-6), 3))
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m stateTUI) View() tea.View {
	var detail pterm.TableData
	if sel := m.table.SelectedRow(); sel != nil {
		detail = pterm.TableData{m.headers, sel}
	}
	v := tea.NewView(m.table.View() + "\n\n" + renderList(detail, m.width) + "\n" + pterm.Gray("↑/↓ navigate • q quit"))
	v.AltScreen = true
	return v
}
