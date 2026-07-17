package main

import (
	"context"
	"fmt"
	"strings"

	"my-agent/internal/agent"
	"my-agent/internal/llm"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- Custom messages for streaming from goroutine to event loop ---

type streamChunkMsg struct {
	chunk agent.AgentChunk
}

type streamDoneMsg struct {
	err error
}

// --- Model ---

type model struct {
	// Agent wiring (set from main.go after construction)
	agent     *agent.FunctionCallingAgent
	modelName string
	tools     []llm.Tool

	// Program reference — set after tea.NewProgram so that streaming
	// goroutines can push messages back into the event loop.
	program *tea.Program

	// Sessions
	sessions  []*Session
	activeIdx int

	// UI dimensions
	width     int
	height    int
	ready     bool
	spContent int // cached sidepanel content width (inside border+padding)

	// Components
	vp           viewport.Model
	spinnerModel spinner.Model

	// Focus state
	focusIndex int // 0 = viewport, 1 = side panel

	// Layout state
	showSidePanel      bool // actual visibility (may be forced by resize)
	userWantsSidePanel bool // user's preference (restored after resize)

	// Streaming state
	loading  bool
	replyBuf *strings.Builder // pointer so value-receiver Update doesn't copy a used Builder
	err      error
}

func initialModel() model {
	sp := spinner.New()
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("62"))
	sp.Spinner = spinner.Dot

	return model{
		sessions:           []*Session{NewSession(0, "Session 1")},
		activeIdx:          0,
		spinnerModel:       sp,
		focusIndex:          0,
		showSidePanel:      true,
		userWantsSidePanel: true,
		replyBuf:           &strings.Builder{},
	}
}

// --- Init ----------------------------------------------------------------

func (m model) Init() tea.Cmd {
	return spinner.Tick
}

// --- Update ---------------------------------------------------------------

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case streamChunkMsg:
		return m.handleStreamChunk(msg)

	case streamDoneMsg:
		return m.handleStreamDone(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinnerModel, cmd = m.spinnerModel.Update(msg)
		return &m, cmd

	default:
		return &m, nil
	}
}

// --- Window resize --------------------------------------------------------

func (m model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height

	// Resize guards — auto-hide the side panel on narrow terminals.
	const narrowThreshold = 50
	if msg.Width < narrowThreshold {
		m.showSidePanel = false
	} else if m.userWantsSidePanel {
		m.showSidePanel = true
	}

	// Compute side panel and main area widths.
	overhead := 4 // border + padding on one column
	var spTotal int
	if m.showSidePanel {
		spTotal = m.width * 25 / 100
		if m.width >= 80 && spTotal < 24 {
			spTotal = 24
		} else if m.width < 80 && spTotal < 20 {
			spTotal = 20
		}
	}

	// Main area content width fills the rest.
	mainContent := m.width - spTotal - overhead
	if !m.showSidePanel {
		mainContent = m.width - overhead // reclaim side panel width
	}
	if mainContent < 20 {
		mainContent = 20
	}

	m.spContent = spTotal - overhead

	// Viewport fills the full height minus the message box border (2 lines).
	vpHeight := m.height - 2
	if vpHeight < 5 {
		vpHeight = 5
	}

	if !m.ready {
		m.vp = viewport.New(mainContent, vpHeight)
		m.ready = true
	} else {
		m.vp.Width = mainContent
		m.vp.Height = vpHeight
	}

	return &m, nil
}

// --- Key handling ---------------------------------------------------------

func (m model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global keys — handled regardless of focus
	switch msg.Type {
	case tea.KeyCtrlC, tea.KeyEsc:
		return &m, tea.Quit

	case tea.KeyTab:
		if m.showSidePanel {
			m.focusIndex = (m.focusIndex + 1) % 2
		}
		return &m, nil

	case tea.KeyShiftTab:
		if m.showSidePanel {
			m.focusIndex = (m.focusIndex - 1 + 2) % 2
		}
		return &m, nil

	case tea.KeyCtrlB:
		m.userWantsSidePanel = !m.userWantsSidePanel
		m.showSidePanel = m.userWantsSidePanel
		m.refreshViewport()
		return &m, nil
	}

	// Component dispatch
	switch m.focusIndex {
	case 0:
		return m.handleViewportKey(msg)
	case 1:
		return m.handleSidepanelKey(msg)
	}

	return &m, nil
}

// handleViewportKey processes keys when the chat viewport is focused.
func (m model) handleViewportKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !m.ready {
		return &m, nil
	}
	m.vp, _ = m.vp.Update(msg)
	return &m, nil
}

// handleSidepanelKey processes keys when the session side panel is focused.
func (m model) handleSidepanelKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyUp:
		if m.activeIdx > 0 {
			m.activeIdx--
			m.refreshViewport()
		}
	case tea.KeyDown:
		if m.activeIdx < len(m.sessions)-1 {
			m.activeIdx++
			m.refreshViewport()
		}
	case tea.KeyEnter:
		m.refreshViewport()
	case tea.KeyCtrlN:
		id := len(m.sessions)
		sess := NewSession(id, fmt.Sprintf("Session %d", id+1))
		m.sessions = append(m.sessions, sess)
		m.activeIdx = id
		m.refreshViewport()
	case tea.KeyDelete, tea.KeyBackspace:
		if len(m.sessions) > 1 {
			m.sessions = append(m.sessions[:m.activeIdx], m.sessions[m.activeIdx+1:]...)
			if m.activeIdx >= len(m.sessions) {
				m.activeIdx = len(m.sessions) - 1
			}
			m.refreshViewport()
		}
	}
	return &m, nil
}

// --- Streaming ------------------------------------------------------------

// startStream launches the agent stream in a background goroutine.  Chunks are
// pushed into the event loop via program.Send so the model stays single-threaded.
func (m *model) startStream(input string) {
	m.loading = true
	m.replyBuf.Reset()

	sess := m.sessions[m.activeIdx]
	sess.AddMessage(llm.Message{Role: llm.RoleUser, Content: input})
	m.refreshViewport()

	go func() {
		req := &agent.AgentRequest{
			Messages: sess.Messages,
			Model:    m.modelName,
			Tools:    m.tools,
		}

		stream, err := m.agent.StreamRun(context.Background(), req)
		if err != nil {
			m.program.Send(streamDoneMsg{err: err})
			return
		}
		defer stream.Close()

		for stream.Next() {
			m.program.Send(streamChunkMsg{chunk: stream.Current()})
		}

		if err := stream.Err(); err != nil {
			m.program.Send(streamDoneMsg{err: err})
			return
		}
		m.program.Send(streamDoneMsg{})
	}()
}

func (m model) handleStreamChunk(msg streamChunkMsg) (tea.Model, tea.Cmd) {
	chunk := msg.chunk

	switch chunk.Type {
	case agent.AgentEventToken:
		m.replyBuf.WriteString(chunk.Content)

	case agent.AgentEventToolCall:
		if chunk.ToolCall != nil {
			m.replyBuf.WriteString(fmt.Sprintf(
				"\n🔧 calling %s(%s)\n",
				chunk.ToolCall.Function.Name,
				chunk.ToolCall.Function.Arguments,
			))
		}

	case agent.AgentEventToolResult:
		if chunk.ToolResult != nil {
			result := chunk.ToolResult.Result
			if len(result) > 200 {
				result = result[:200] + "..."
			}
			m.replyBuf.WriteString(fmt.Sprintf(
				"✅ %s → %s\n",
				chunk.ToolResult.Name,
				strings.TrimSpace(result),
			))
		}

	case agent.AgentEventIterationStart:
		if chunk.MaxIter > 0 {
			m.replyBuf.WriteString(fmt.Sprintf(
				"\n── iter %d/%d ──\n",
				chunk.Iteration+1,
				chunk.MaxIter,
			))
		}
	}

	sess := m.sessions[m.activeIdx]
	m.vp.SetContent(m.renderMessages(sess, m.replyBuf.String()))
	m.vp.GotoBottom()

	return &m, nil
}

func (m model) handleStreamDone(msg streamDoneMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	m.focusIndex = 0 // return focus to viewport

	if msg.err != nil {
		m.err = msg.err
		return &m, nil
	}

	sess := m.sessions[m.activeIdx]
	reply := m.replyBuf.String()
	m.replyBuf.Reset()

	if reply != "" {
		sess.AddMessage(llm.Message{Role: llm.RoleAssistant, Content: reply})
	}

	m.vp.SetContent(m.renderMessages(sess, ""))
	m.vp.GotoBottom()

	return &m, nil
}

func (m model) refreshViewport() {
	if !m.ready {
		return
	}
	sess := m.sessions[m.activeIdx]
	m.vp.SetContent(m.renderMessages(sess, ""))
	m.vp.GotoBottom()
}

// --- View ----------------------------------------------------------------

func (m model) View() string {
	if !m.ready {
		return "Initializing…"
	}

	if m.showSidePanel {
		left := m.renderSidepanel(m.spContent)
		leftWidth := lipgloss.Width(left)

		rightTotal := m.width - leftWidth + 2
		if rightTotal < 20 {
			rightTotal = 20
		}
		right := m.renderMain(rightTotal)

		return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}

	// Side panel hidden — main area takes full width.
	return m.renderMain(m.width)
}

func (m model) renderSidepanel(width int) string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("Sessions"))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", width-2))
	b.WriteString("\n\n")

	for i, sess := range m.sessions {
		if i == m.activeIdx {
			b.WriteString(activeSessionStyle.Render("▶ " + sess.Name))
		} else {
			b.WriteString(inactiveSessionStyle.Render("  " + sess.Name))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Tab cycle"))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑↓ select"))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Del delete"))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Ctrl+N new"))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Esc quit"))

	panelStyle := sidePanelStyle
	if m.focusIndex == 1 {
		panelStyle = focusedBorderStyle
	}
	return panelStyle.Width(width).Height(m.height - 2).Render(b.String())
}

func (m model) renderMain(totalWidth int) string {
	contentWidth := totalWidth - 4
	if contentWidth < 16 {
		contentWidth = 16
	}

	m.vp.Width = contentWidth
	vpContent := m.vp.View()

	if m.loading {
		vpContent += "\n" + m.spinnerModel.View() + " Thinking…"
	}

	msgStyle := messagesStyle
	if m.focusIndex == 0 {
		msgStyle = focusedBorderStyle
	}

	return msgStyle.Width(contentWidth).Render(vpContent)
}

func (m model) renderMessages(sess *Session, streaming string) string {
	var b strings.Builder

	for _, msg := range sess.Messages {
		switch msg.Role {
		case llm.RoleUser:
			b.WriteString(userMsgStyle.Render("┃ " + msg.Content))
			b.WriteString("\n\n")
		case llm.RoleAssistant:
			b.WriteString(assistantMsgStyle.Render(msg.Content))
			b.WriteString("\n\n")
		case llm.RoleTool:
			b.WriteString(toolMsgStyle.Render("⚙ " + msg.Content))
			b.WriteString("\n\n")
		}
	}

	if streaming != "" {
		b.WriteString(streaming)
		b.WriteString("\n")
	}

	return b.String()
}
