package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yosephbernandus/baton/internal/events"
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("62")).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("252"))

	statusCompleted = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	statusRunning   = lipgloss.NewStyle().Foreground(lipgloss.Color("226"))
	statusFailed    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	statusTimeout   = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	statusClarify   = lipgloss.NewStyle().Foreground(lipgloss.Color("51"))
	statusHuman     = lipgloss.NewStyle().Foreground(lipgloss.Color("201"))

	selectedStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("236"))

	outputStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	bannerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("201")).
			Bold(true)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))
)

type taskState struct {
	ID           string
	Runtime      string
	Model        string
	Status       string
	Duration     string
	StartedAt    time.Time
	Output       []string
	Clarify      string
	DispatchedBy string
}

type Model struct {
	tasks      map[string]*taskState
	taskOrder  []string
	cursor     int
	width      int
	height     int
	eventCh    <-chan events.Event
	cancel     context.CancelFunc
	showOutput bool
	quitting   bool
}

type eventMsg events.Event
type tickMsg time.Time

func NewModel(eventPath string) (*Model, error) {
	ctx, cancel := context.WithCancel(context.Background())

	tailer := events.NewTailer(eventPath)
	ch, err := tailer.Tail(ctx)
	if err != nil {
		cancel()
		return nil, err
	}

	return &Model{
		tasks:   make(map[string]*taskState),
		eventCh: ch,
		cancel:  cancel,
	}, nil
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(waitForEvent(m.eventCh), tickCmd())
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			m.cancel()
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.taskOrder)-1 {
				m.cursor++
			}
		case "enter":
			m.showOutput = !m.showOutput
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case eventMsg:
		m.processEvent(events.Event(msg))
		return m, waitForEvent(m.eventCh)

	case tickMsg:
		return m, tickCmd()
	}

	return m, nil
}

func (m *Model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	title := titleStyle.Render(" Baton Monitor ")
	b.WriteString(title)
	b.WriteString("\n\n")

	b.WriteString(m.renderTaskTable())
	b.WriteString("\n")

	if m.showOutput && len(m.taskOrder) > 0 {
		b.WriteString(m.renderOutput())
		b.WriteString("\n")
	}

	if banner := m.renderEscalationBanner(); banner != "" {
		b.WriteString(banner)
		b.WriteString("\n")
	}

	help := helpStyle.Render("[↑/↓] select  [enter] output  [q] quit")
	b.WriteString(help)

	return b.String()
}

func (m *Model) processEvent(ev events.Event) {
	id := ev.TaskID
	if id == "" {
		return
	}

	t, exists := m.tasks[id]
	if !exists {
		t = &taskState{ID: id}
		m.tasks[id] = t
		m.taskOrder = append(m.taskOrder, id)
	}

	if ev.Runtime != "" {
		t.Runtime = ev.Runtime
	}
	if ev.Model != "" {
		t.Model = ev.Model
	}
	if ev.DispatchedBy != "" {
		t.DispatchedBy = ev.DispatchedBy
	}

	switch ev.EventType {
	case "task_created":
		t.Status = "pending"
	case "task_started":
		t.Status = "running"
		t.StartedAt = ev.Timestamp
	case "output":
		if line, ok := ev.Data["line"].(string); ok {
			t.Output = append(t.Output, line)
			if len(t.Output) > 200 {
				t.Output = t.Output[len(t.Output)-200:]
			}
		}
	case "task_completed":
		t.Status = "completed"
		if d, ok := ev.Data["duration"].(string); ok {
			t.Duration = d
		}
	case "task_failed":
		t.Status = "failed"
		if d, ok := ev.Data["duration"].(string); ok {
			t.Duration = d
		}
	case "task_timeout":
		t.Status = "timeout"
		if d, ok := ev.Data["duration"].(string); ok {
			t.Duration = d
		}
	case "needs_clarification":
		t.Status = "needs_clarification"
		if c, ok := ev.Data["clarification"].(string); ok {
			t.Clarify = c
		}
	case "needs_human":
		t.Status = "needs_human"
	}
}

func (m *Model) renderTaskTable() string {
	var b strings.Builder

	header := fmt.Sprintf("  %-16s %-12s %-12s %-22s %-10s",
		"TASK ID", "RUNTIME", "MODEL", "STATUS", "DURATION")
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	sorted := make([]string, len(m.taskOrder))
	copy(sorted, m.taskOrder)
	sort.Strings(sorted)

	for i, id := range m.taskOrder {
		t := m.tasks[id]

		status := m.styledStatus(t)
		duration := t.Duration
		if duration == "" && t.Status == "running" && !t.StartedAt.IsZero() {
			duration = time.Since(t.StartedAt).Round(time.Second).String()
		}
		if duration == "" {
			duration = "-"
		}

		cursor := "  "
		if i == m.cursor {
			cursor = "▸ "
		}

		line := fmt.Sprintf("%s%-16s %-12s %-12s %-22s %-10s",
			cursor, truncate(t.ID, 16), truncate(t.Runtime, 12), truncate(t.Model, 12), status, duration)

		if i == m.cursor {
			line = selectedStyle.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

func (m *Model) styledStatus(t *taskState) string {
	switch t.Status {
	case "completed":
		return statusCompleted.Render("● completed")
	case "running":
		return statusRunning.Render("◉ running")
	case "failed":
		return statusFailed.Render("✗ failed")
	case "timeout":
		return statusTimeout.Render("⏱ timeout")
	case "needs_clarification":
		return statusClarify.Render("? needs clarify")
	case "needs_human":
		return statusHuman.Render("! needs human")
	case "pending":
		return "○ pending"
	default:
		return t.Status
	}
}

func (m *Model) renderOutput() string {
	if m.cursor >= len(m.taskOrder) {
		return ""
	}

	id := m.taskOrder[m.cursor]
	t := m.tasks[id]

	var b strings.Builder
	header := fmt.Sprintf("OUTPUT: %s (%s/%s)", t.ID, t.Runtime, t.Model)
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	maxLines := 10
	if m.height > 30 {
		maxLines = 15
	}

	start := 0
	if len(t.Output) > maxLines {
		start = len(t.Output) - maxLines
	}

	for _, line := range t.Output[start:] {
		b.WriteString(outputStyle.Render("  │ " + truncate(line, m.width-6)))
		b.WriteString("\n")
	}

	if len(t.Output) == 0 {
		b.WriteString(outputStyle.Render("  │ (no output)"))
		b.WriteString("\n")
	}

	return b.String()
}

func (m *Model) renderEscalationBanner() string {
	for _, id := range m.taskOrder {
		t := m.tasks[id]
		if t.Status == "needs_human" || (t.Status == "needs_clarification" && t.Clarify != "") {
			msg := fmt.Sprintf("! %s blocked — %s", t.ID, t.Clarify)
			return bannerStyle.Render(msg)
		}
	}
	return ""
}

func waitForEvent(ch <-chan events.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return eventMsg(ev)
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func truncate(s string, n int) string {
	if n <= 0 {
		return s
	}
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
