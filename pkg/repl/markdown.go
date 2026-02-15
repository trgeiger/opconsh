package repl

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"golang.org/x/term"
)

type pagerModel struct {
	viewport viewport.Model
	content  string
}

func (m pagerModel) Init() tea.Cmd { return nil }

func (m pagerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" || msg.String() == "esc" {
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height
		m.viewport.SetContent(renderContent(m.content, msg.Width))
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m pagerModel) View() string {
	view := m.viewport.View()

	symbol := " :"
	if m.viewport.AtBottom() {
		symbol = " (END)"
	}

	percent := int(m.viewport.ScrollPercent() * 100)
	info := fmt.Sprintf(" %d%% (press q to exit) ", percent)

	gapSize := m.viewport.Width - len(symbol) - len(info)
	if gapSize < 0 {
		gapSize = 0
	}
	gap := strings.Repeat(" ", gapSize)

	footer := fmt.Sprintf("\x1b[7m%s%s%s\x1b[0m", symbol, gap, info)

	return fmt.Sprintf("%s\n%s", view, footer)
}

func renderContent(raw string, width int) string {
	// Glamour handles its own wrapping internally
	r, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width-4), // Padding for readability
	)
	out, _ := r.Render(raw)
	return out
}

func RunMarkdownPager(markdown string) error {
	fd := int(os.Stdout.Fd())
	width, height, err := term.GetSize(fd)
	if err != nil {
		width, height = 80, 24
	}

	m := pagerModel{
		content:  markdown,
		viewport: viewport.New(width, height),
	}

	m.viewport.SetContent(renderContent(markdown, width))

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}
