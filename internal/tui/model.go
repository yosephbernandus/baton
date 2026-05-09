package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
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

	bannerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("201")).
			Bold(true)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	focusedBorderStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("62"))

	unfocusedBorderStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("241"))

	focusedHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("62"))

	scrollInfoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	dividerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	dividerActiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("62")).
				Bold(true)
)

type focusPanel int

const (
	focusTaskList focusPanel = iota
	focusOutput
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
	Progress     string
	Stuck        bool
	viewport     viewport.Model
	vpReady      bool
	userScrolled bool
}

type Model struct {
	tasks      map[string]*taskState
	taskOrder  []string
	cursor     int
	taskScroll int     // first visible task row index
	splitRatio float64 // fraction of available height for task list (0.2–0.8)
	dragging   bool    // mouse dragging the divider
	dividerRow int     // screen row where divider was rendered
	width      int
	height     int
	eventCh    <-chan events.Event
	cancel     context.CancelFunc
	showOutput bool
	quitting   bool
	killCh     chan string
	focus      focusPanel
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
		tasks:      make(map[string]*taskState),
		eventCh:    ch,
		cancel:     cancel,
		splitRatio: 0.4,
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
		case "K":
			if m.cursor < len(m.taskOrder) {
				id := m.taskOrder[m.cursor]
				t := m.tasks[id]
				if t.Status == "running" && m.killCh != nil {
					select {
					case m.killCh <- id:
					default:
					}
				}
			}
		case "c":
			m.clearStale()
		case "tab":
			if !m.showOutput {
				m.showOutput = true
				m.focus = focusOutput
				m.onCursorChanged()
			} else {
				if m.focus == focusTaskList {
					m.focus = focusOutput
				} else {
					m.focus = focusTaskList
				}
				m.onCursorChanged()
			}
		case "enter":
			m.showOutput = !m.showOutput
			if !m.showOutput {
				m.focus = focusTaskList
			}
		case "+", "=":
			if m.showOutput && m.splitRatio < 0.8 {
				m.splitRatio += 0.05
				m.resizeAllViewports()
			}
		case "-", "_":
			if m.showOutput && m.splitRatio > 0.2 {
				m.splitRatio -= 0.05
				m.resizeAllViewports()
			}
		default:
			if m.focus == focusOutput && m.showOutput && m.cursor < len(m.taskOrder) {
				t := m.tasks[m.taskOrder[m.cursor]]
				m.ensureViewport(t)
				var cmd tea.Cmd
				t.viewport, cmd = t.viewport.Update(msg)
				t.userScrolled = !t.viewport.AtBottom()
				return m, cmd
			} else if m.focus == focusTaskList || !m.showOutput {
				switch msg.String() {
				case "up", "k":
					if m.cursor > 0 {
						m.cursor--
						m.scrollToCursor()
						m.onCursorChanged()
					}
				case "down", "j":
					if m.cursor < len(m.taskOrder)-1 {
						m.cursor++
						m.scrollToCursor()
						m.onCursorChanged()
					}
				}
			}
		}

	case tea.MouseMsg:
		if msg.Action == tea.MouseActionRelease {
			m.dragging = false
		} else if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && m.showOutput {
			// Wide hit area: ±2 rows around divider
			if msg.Y >= m.dividerRow-2 && msg.Y <= m.dividerRow+2 {
				m.dragging = true
			}
		}
		if msg.Action == tea.MouseActionMotion && m.dragging && m.showOutput {
			topChrome := 3
			available := m.height - topChrome - 1
			if available > 0 {
				ratio := float64(msg.Y-topChrome) / float64(available)
				if ratio < 0.2 {
					ratio = 0.2
				}
				if ratio > 0.8 {
					ratio = 0.8
				}
				m.splitRatio = ratio
				m.resizeAllViewports()
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeAllViewports()

	case eventMsg:
		m.processEvent(events.Event(msg))
		return m, waitForEvent(m.eventCh)

	case tickMsg:
		return m, tickCmd()
	}

	return m, nil
}

func (m *Model) ensureViewport(t *taskState) {
	if t.vpReady {
		return
	}
	vpHeight := m.outputViewportHeight()
	vpWidth := m.width - 4
	if vpWidth < 20 {
		vpWidth = 20
	}
	t.viewport = viewport.New(vpWidth, vpHeight)
	t.viewport.MouseWheelEnabled = true
	t.viewport.SetContent(strings.Join(t.Output, "\n"))
	t.viewport.GotoBottom()
	t.vpReady = true
	m.setViewportKeysEnabled(t, m.focus == focusOutput)
}

func (m *Model) maxVisibleTasks() int {
	if !m.showOutput {
		return m.height - 5
	}
	// Split available space by ratio
	available := m.height - 8 // title(2) + task header(1) + divider(1) + output header(1) + border(2) + help(1)
	taskRows := int(float64(available) * m.splitRatio)
	if taskRows < 3 {
		taskRows = 3
	}
	return taskRows
}

func (m *Model) scrollIndicatorLines() int {
	maxVisible := m.maxVisibleTasks()
	total := len(m.taskOrder)
	extra := 0
	if m.taskScroll > 0 {
		extra++
	}
	if m.taskScroll+maxVisible < total {
		extra++
	}
	return extra
}

func (m *Model) outputViewportHeight() int {
	maxTasks := m.maxVisibleTasks()
	visibleTasks := maxTasks
	if len(m.taskOrder) < visibleTasks {
		visibleTasks = len(m.taskOrder)
	}
	if visibleTasks == 0 {
		visibleTasks = 1
	}
	indicators := m.scrollIndicatorLines()
	// title(2) + task header(1) + visible tasks + scroll indicators + divider(1) + output header(1) + border(2) + help(1)
	chrome := 2 + 1 + visibleTasks + indicators + 1 + 1 + 2 + 1
	vpHeight := m.height - chrome
	if vpHeight < 5 {
		vpHeight = 5
	}
	return vpHeight
}

func (m *Model) scrollToCursor() {
	maxVisible := m.maxVisibleTasks()
	if m.cursor < m.taskScroll {
		m.taskScroll = m.cursor
	}
	if m.cursor >= m.taskScroll+maxVisible {
		m.taskScroll = m.cursor - maxVisible + 1
	}
}

func (m *Model) resizeAllViewports() {
	m.scrollToCursor()
	vpHeight := m.outputViewportHeight()
	vpWidth := m.width - 4
	if vpWidth < 20 {
		vpWidth = 20
	}
	for _, id := range m.taskOrder {
		t := m.tasks[id]
		if t.vpReady {
			t.viewport.Width = vpWidth
			t.viewport.Height = vpHeight
		}
	}
}

func (m *Model) renderDivider() string {
	style := dividerStyle
	label := "─── ↕ drag ───"
	if m.dragging {
		style = dividerActiveStyle
		label = "─── ↕ drag ───"
	}
	w := m.width
	if w <= 0 {
		w = 80
	}
	labelLen := 14
	side := (w - labelLen) / 2
	if side < 0 {
		side = 0
	}
	line := strings.Repeat("─", side) + label + strings.Repeat("─", w-side-labelLen)
	return style.Render(line)
}

func (m *Model) setViewportKeysEnabled(t *taskState, enabled bool) {
	t.viewport.KeyMap.Up.SetEnabled(enabled)
	t.viewport.KeyMap.Down.SetEnabled(enabled)
	t.viewport.KeyMap.PageUp.SetEnabled(enabled)
	t.viewport.KeyMap.PageDown.SetEnabled(enabled)
	t.viewport.KeyMap.HalfPageUp.SetEnabled(enabled)
	t.viewport.KeyMap.HalfPageDown.SetEnabled(enabled)
}

func (m *Model) onCursorChanged() {
	for _, id := range m.taskOrder {
		t := m.tasks[id]
		if t.vpReady {
			m.setViewportKeysEnabled(t, false)
		}
	}
	if m.cursor < len(m.taskOrder) && m.showOutput {
		t := m.tasks[m.taskOrder[m.cursor]]
		m.ensureViewport(t)
		m.setViewportKeysEnabled(t, m.focus == focusOutput)
	}
}

func (m *Model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder
	lineCount := 0

	title := titleStyle.Render(" Baton Monitor ")
	b.WriteString(title)
	b.WriteString("\n\n")
	lineCount += 2

	taskTable := m.renderTaskTable()
	b.WriteString(taskTable)
	lineCount += strings.Count(taskTable, "\n")

	if m.showOutput && len(m.taskOrder) > 0 {
		m.dividerRow = lineCount
		b.WriteString(m.renderDivider())
		b.WriteString("\n")

		b.WriteString(m.renderOutput())
		b.WriteString("\n")
	}

	if banner := m.renderEscalationBanner(); banner != "" {
		b.WriteString(banner)
		b.WriteString("\n")
	}

	var helpText string
	if m.showOutput && m.focus == focusOutput {
		helpText = "[↑/↓/j/k] scroll  [pgup/pgdn] page  [tab] task list  [+/-] resize  [enter] close  [q] quit"
	} else {
		helpText = "[↑/↓] select  [enter] output  [tab] focus output  [+/-] resize  [K] kill  [c] clear  [q] quit"
	}
	help := helpStyle.Render(helpText)
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
	switch ev.EventType {
	case "task_created":
		t.Status = "pending"
	case "task_started":
		t.Status = "running"
		t.StartedAt = ev.Timestamp
	case "output":
		if line, ok := ev.Data["line"].(string); ok {
			t.Output = append(t.Output, line)
			if len(t.Output) > 500 {
				t.Output = t.Output[len(t.Output)-500:]
			}
			if t.vpReady {
				wasAtBottom := t.viewport.AtBottom()
				t.viewport.SetContent(strings.Join(t.Output, "\n"))
				if wasAtBottom || !t.userScrolled {
					t.viewport.GotoBottom()
				}
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
	case "task_killed":
		t.Status = "killed"
	case "task_deferred":
		t.Status = "deferred"
	case "task_responded":
		t.Status = "pending"
	case "task_redispatched":
		t.Status = "running"
	case "worker_heartbeat", "worker_progress", "worker_milestone":
		if msg, ok := ev.Data["msg"].(string); ok {
			t.Progress = msg
		}
		t.Stuck = false
	case "worker_stuck", "worker_error":
		if msg, ok := ev.Data["msg"].(string); ok {
			t.Progress = msg
		}
		t.Stuck = true
	case "guidance_sent":
		t.Stuck = false
	}
}

func (m *Model) renderTaskTable() string {
	var b strings.Builder

	focusMarker := " "
	hStyle := headerStyle
	if m.focus == focusTaskList || !m.showOutput {
		focusMarker = "▸"
		hStyle = focusedHeaderStyle
	}

	totalTasks := len(m.taskOrder)
	maxVisible := m.maxVisibleTasks()

	scrollHint := ""
	if totalTasks > maxVisible {
		scrollHint = fmt.Sprintf(" [%d/%d]", m.cursor+1, totalTasks)
	}

	header := fmt.Sprintf("%s %-16s %-12s %-12s %-22s %-10s%s",
		focusMarker, "TASK ID", "RUNTIME", "MODEL", "STATUS", "DURATION", scrollHint)
	b.WriteString(hStyle.Render(header))
	b.WriteString("\n")

	// Show scroll-up indicator
	if m.taskScroll > 0 {
		b.WriteString(scrollInfoStyle.Render("  ↑ more tasks above"))
		b.WriteString("\n")
	}

	end := m.taskScroll + maxVisible
	if end > totalTasks {
		end = totalTasks
	}

	for i := m.taskScroll; i < end; i++ {
		id := m.taskOrder[i]
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

	// Show scroll-down indicator
	if end < totalTasks {
		b.WriteString(scrollInfoStyle.Render("  ↓ more tasks below"))
		b.WriteString("\n")
	}

	return b.String()
}

func (m *Model) styledStatus(t *taskState) string {
	switch t.Status {
	case "completed":
		return statusCompleted.Render("● completed")
	case "running":
		if t.Stuck {
			return statusClarify.Render("⚠ stuck")
		}
		if t.Progress != "" {
			msg := t.Progress
			if len(msg) > 18 {
				msg = msg[:18]
			}
			return statusRunning.Render("◉ " + msg)
		}
		return statusRunning.Render("◉ running")
	case "failed":
		return statusFailed.Render("✗ failed")
	case "timeout":
		return statusTimeout.Render("⏱ timeout")
	case "needs_clarification":
		return statusClarify.Render("? needs clarify")
	case "needs_human":
		return statusHuman.Render("! needs human")
	case "killed":
		return statusFailed.Render("☠ killed")
	case "deferred":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("⏸ deferred")
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
	m.ensureViewport(t)

	var b strings.Builder
	focusMarker := " "
	hStyle := headerStyle
	if m.focus == focusOutput {
		focusMarker = "▸"
		hStyle = focusedHeaderStyle
	}
	header := fmt.Sprintf("%s OUTPUT: %s (%s/%s)", focusMarker, t.ID, t.Runtime, t.Model)
	b.WriteString(hStyle.Render(header))

	totalLines := t.viewport.TotalLineCount()
	scrollPct := t.viewport.ScrollPercent()
	scrollInfo := fmt.Sprintf("  [%d lines · %.0f%%]", totalLines, scrollPct*100)
	b.WriteString(scrollInfoStyle.Render(scrollInfo))
	b.WriteString("\n")

	vpContent := t.viewport.View()
	borderStyle := unfocusedBorderStyle
	if m.focus == focusOutput {
		borderStyle = focusedBorderStyle
	}
	b.WriteString(borderStyle.Render(vpContent))
	b.WriteString("\n")
	return b.String()
}

func (m *Model) renderEscalationBanner() string {
	for _, id := range m.taskOrder {
		t := m.tasks[id]
		if t.Status == "needs_human" || (t.Status == "needs_clarification" && t.Clarify != "") {
			msg := fmt.Sprintf("! %s blocked -- %s", t.ID, t.Clarify)
			return bannerStyle.Render(msg)
		}
	}
	return ""
}

func (m *Model) clearStale() {
	var newOrder []string
	for _, id := range m.taskOrder {
		t := m.tasks[id]
		if t.Status == "running" || t.Status == "pending" {
			newOrder = append(newOrder, id)
		} else {
			delete(m.tasks, id)
		}
	}
	m.taskOrder = newOrder
	if m.cursor >= len(m.taskOrder) && m.cursor > 0 {
		m.cursor = len(m.taskOrder) - 1
	}
	m.onCursorChanged()
}

func (m *Model) SetKillChannel(ch chan string) {
	m.killCh = ch
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
