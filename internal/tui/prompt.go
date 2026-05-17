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
	if msg, ok := msg.(tea.KeyMsg); ok {
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

// SelectOrInput shows options with a final "Other" choice that enables text input.
// Returns the selected option's Value, or the typed text if "Other" was chosen.
func SelectOrInput(title string, options []SelectOption) (string, error) {
	opts := make([]SelectOption, len(options))
	copy(opts, options)
	opts = append(opts, SelectOption{Label: "Other", Description: "type custom value"})

	m := newSelectInputModel(title, opts)
	p := tea.NewProgram(m)
	result, err := p.Run()
	if err != nil {
		return "", err
	}
	final := result.(selectInputModel)
	if final.cancelled {
		return "", nil
	}
	return final.result, nil
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

// selectInputModel combines selection with text input for the "Other" option.
type selectInputModel struct {
	title     string
	options   []SelectOption
	cursor    int
	inputMode bool
	input     string
	result    string
	cancelled bool
	done      bool
}

func newSelectInputModel(title string, options []SelectOption) selectInputModel {
	return selectInputModel{
		title:   title,
		options: options,
	}
}

func (m selectInputModel) Init() tea.Cmd {
	return nil
}

func (m selectInputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		if m.inputMode {
			switch msg.String() {
			case "enter":
				if m.input != "" {
					m.result = m.input
					m.done = true
					return m, tea.Quit
				}
			case "esc":
				m.inputMode = false
				m.input = ""
			case "backspace":
				if len(m.input) > 0 {
					m.input = m.input[:len(m.input)-1]
				}
			case "ctrl+c":
				m.cancelled = true
				m.done = true
				return m, tea.Quit
			default:
				if len(msg.String()) == 1 {
					m.input += msg.String()
				}
			}
			return m, nil
		}

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
			if m.cursor == len(m.options)-1 {
				m.inputMode = true
			} else {
				opt := m.options[m.cursor]
				if opt.Value != "" {
					m.result = opt.Value
				} else {
					m.result = opt.Label
				}
				m.done = true
				return m, tea.Quit
			}
		case "ctrl+c", "q":
			m.cancelled = true
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m selectInputModel) View() string {
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

	if m.inputMode {
		b.WriteString("\n")
		b.WriteString(promptCursorStyle.Render("  Type model: "))
		b.WriteString(m.input)
		b.WriteString(promptCursorStyle.Render("█"))
		b.WriteString("\n")
		b.WriteString(promptDimStyle.Render("  enter confirm • esc back"))
	} else {
		b.WriteString("\n")
		b.WriteString(promptDimStyle.Render("↑/↓ navigate • enter select • q quit"))
	}

	return b.String()
}
