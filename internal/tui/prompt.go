package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	promptTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("62"))

	promptItemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	promptCursorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("42")).
				Bold(true)

	promptDimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))
)

type SelectOption struct {
	Label       string
	Description string
	Value       string
}

type selectModel struct {
	title    string
	options  []SelectOption
	cursor   int
	selected int
	done     bool
}

func newSelectModel(title string, options []SelectOption) selectModel {
	return selectModel{
		title:    title,
		options:  options,
		cursor:   0,
		selected: -1,
	}
}

func (m selectModel) Init() tea.Cmd {
	return nil
}

func (m selectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.options)-1 {
				m.cursor++
			}
		case "enter":
			m.selected = m.cursor
			m.done = true
			return m, tea.Quit
		case "ctrl+c", "q":
			m.selected = -1
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m selectModel) View() string {
	if m.done {
		return ""
	}

	var b strings.Builder
	b.WriteString(promptTitleStyle.Render(m.title))
	b.WriteString("\n\n")

	for i, opt := range m.options {
		cursor := "  "
		style := promptItemStyle
		if i == m.cursor {
			cursor = promptCursorStyle.Render("→ ")
			style = promptCursorStyle
		}

		label := style.Render(opt.Label)
		b.WriteString(fmt.Sprintf("%s%s", cursor, label))

		if opt.Description != "" {
			b.WriteString(promptDimStyle.Render("  " + opt.Description))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(promptDimStyle.Render("↑/↓ navigate • enter select • q quit"))

	return b.String()
}

// Select shows an interactive selection prompt and returns the chosen index.
// Returns -1 if user cancelled.
func Select(title string, options []SelectOption) (int, error) {
	m := newSelectModel(title, options)
	p := tea.NewProgram(m)
	result, err := p.Run()
	if err != nil {
		return -1, err
	}
	final := result.(selectModel)
	return final.selected, nil
}

// Confirm shows a yes/no prompt. Returns true for yes.
func Confirm(question string) (bool, error) {
	idx, err := Select(question, []SelectOption{
		{Label: "Yes"},
		{Label: "No"},
	})
	if err != nil {
		return false, err
	}
	return idx == 0, nil
}
