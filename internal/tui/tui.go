// Package tui provides a Bubble Tea-based TUI for interacting with an LLM agent.
// It combines agent streaming with a multi-session chat interface inspired by
// the go-chat TUI Spec example.
package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/DaviMGDev/thoth-agent/internal/agent"
	"github.com/DaviMGDev/thoth-agent/internal/llm"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

// Focus targets for Tab/Shift+Tab component cycling.
const (
	FocusSidebar  = 0
	FocusViewport = 1
	FocusTextarea = 2
)

const (
	footerHeight       = 1
	sessionHeaderLines = 3 // session name inlineblock (1) + spacing (2)
)

// ---------------------------------------------------------------------------
// Custom messages for streaming from goroutine to event loop
// ---------------------------------------------------------------------------

type streamChunkMsg struct {
	chunk agent.AgentChunk
}

type streamDoneMsg struct {
	err error
}

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

// Model is the root Bubble Tea model for the agent TUI.
// Create with NewModel(), then set Agent, ModelName, Tools, and Program
// before running the program.
type Model struct {
	// Agent wiring — set after construction
	Agent     *agent.FunctionCallingAgent
	ModelName string
	Tools     []llm.Tool
	Program   *tea.Program

	// Sessions
	Sessions  []*Session
	ActiveIdx int

	// UI components
	VP           viewport.Model
	TextArea     textarea.Model
	SpinnerModel spinner.Model

	// Terminal dimensions
	Width     int
	Height    int
	Ready     bool

	// Focus state (FocusSidebar / FocusViewport / FocusTextarea)
	FocusTarget int

	// Layout
	ShowSidebar      bool
	UserWantsSidebar bool

	// Streaming state
	Loading       bool
	StreamingText string // assistant text being accumulated (live preview)
	StreamingTool string // tool display being accumulated (live preview)
	Err           error
}

// NewModel creates a Model with default state and UI components.
// The caller must set Agent, ModelName, Tools, and Program before Run().
func NewModel() *Model {
	ta := textarea.New()
	ta.Placeholder = "Type a message..."
	ta.SetWidth(40)
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.BlurredStyle.CursorLine = lipgloss.NewStyle()
	ta.Focus()

	sp := spinner.New()
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("62"))
	sp.Spinner = spinner.Dot

	return &Model{
		Sessions:          []*Session{NewSession(0, "Session 1")},
		ActiveIdx:         0,
		TextArea:          ta,
		VP:                viewport.New(0, 0),
		SpinnerModel:      sp,
		FocusTarget:       FocusTextarea,
		ShowSidebar:       true,
		UserWantsSidebar:  true,
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, spinner.Tick)
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		m.SpinnerModel, cmd = m.SpinnerModel.Update(msg)
		return &m, cmd
	default:
		// Delegate to sub-models for non-key messages (e.g. paste)
		var cmds []tea.Cmd
		var cmd tea.Cmd
		m.VP, cmd = m.VP.Update(msg)
		cmds = append(cmds, cmd)
		m.TextArea, cmd = m.TextArea.Update(msg)
		cmds = append(cmds, cmd)
		return &m, tea.Batch(cmds...)
	}
}

// ---------------------------------------------------------------------------
// Window resize
// ---------------------------------------------------------------------------

func (m Model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.Width = msg.Width
	m.Height = msg.Height
	if !m.Ready {
		m.Ready = true
	}

	// Auto-hide sidebar on narrow terminals
	const narrowThreshold = 50
	if msg.Width < narrowThreshold {
		m.ShowSidebar = false
	} else if m.UserWantsSidebar {
		m.ShowSidebar = true
	}

	// Layout calculation
	// Left sidebar: 25% width, right main area: remaining
	overhead := 4 // border + padding on one column
	var spTotal int
	if m.ShowSidebar {
		spTotal = m.Width * 25 / 100
		// Cap sidebar at 40% of screen width; minimum 10
		maxSp := m.Width * 40 / 100
		if spTotal > maxSp {
			spTotal = maxSp
		}
		if spTotal < 10 {
			spTotal = 10
		}
	}

	mainContent := m.Width - spTotal - overhead
	if !m.ShowSidebar {
		mainContent = m.Width - overhead
	}
	if mainContent < 16 {
		mainContent = 16
	}

	// Right panel: viewport on top, textarea at bottom
	internalTaHeight := m.TextArea.Height()
	// internal height = total - footer(1) - ta border(2) - ta content - vp border(2)
	internalVpHeight := m.Height - 1 - internalTaHeight - 4
	if internalVpHeight < 1 {
		internalVpHeight = 1
	}

	m.VP.Width = mainContent - 2
	m.VP.Height = internalVpHeight
	m.TextArea.SetWidth(mainContent - 2)

	// Refresh viewport
	m.VP.SetContent(m.renderMessages())
	m.VP.GotoBottom()

	return &m, nil
}

// ---------------------------------------------------------------------------
// Keyboard handling
// ---------------------------------------------------------------------------

func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global keys — handled regardless of focus
	switch msg.Type {
	case tea.KeyEscape:
		return &m, tea.Quit

	case tea.KeyCtrlC:
		return &m, tea.Quit

	case tea.KeyTab:
		m.cycleFocus(1)
		return &m, nil

	case tea.KeyShiftTab:
		m.cycleFocus(-1)
		return &m, nil

	case tea.KeyCtrlG:
		m.UserWantsSidebar = !m.UserWantsSidebar
		m.ShowSidebar = m.UserWantsSidebar
		m.refreshViewport()
		return &m, nil

	case tea.KeyCtrlO:
		m.exportChat()
		return &m, nil

	case tea.KeyEnter:
		switch m.FocusTarget {
		case FocusSidebar:
			// Enter in sidebar does nothing extra (navigation is via ↑↓)
			return &m, nil
		case FocusTextarea:
			if m.Loading {
				return &m, nil
			}
			value := m.TextArea.Value()
			if value == "" {
				return &m, nil
			}
			m.submitMessage(value)
			return &m, nil
		}
		return &m, nil

	case tea.KeyCtrlJ:
		// Ctrl+J inserts a newline when the textarea is focused
		if m.FocusTarget == FocusTextarea {
			m.TextArea.InsertString("\n")
			return &m, nil
		}
		return &m, nil
	}

	// Component dispatch
	switch m.FocusTarget {
	case FocusSidebar:
		return m.handleSidebarKey(msg)
	case FocusViewport:
		return m.handleViewportKey(msg)
	case FocusTextarea:
		return m.handleTextareaKey(msg)
	}

	return &m, nil
}

// handleSidebarKey processes keys when the session sidebar is focused.
func (m Model) handleSidebarKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyUp:
		if m.ActiveIdx > 0 {
			m.ActiveIdx--
			m.refreshViewport()
		}
	case tea.KeyDown:
		if m.ActiveIdx < len(m.Sessions)-1 {
			m.ActiveIdx++
			m.refreshViewport()
		}
	case tea.KeyEnter:
		m.refreshViewport()
	case tea.KeyCtrlN:
		id := len(m.Sessions)
		sess := NewSession(id, fmt.Sprintf("Session %d", id+1))
		m.Sessions = append(m.Sessions, sess)
		m.ActiveIdx = id
		m.refreshViewport()
	case tea.KeyDelete, tea.KeyBackspace:
		if len(m.Sessions) > 1 && m.ActiveIdx < len(m.Sessions) {
			m.Sessions = append(m.Sessions[:m.ActiveIdx], m.Sessions[m.ActiveIdx+1:]...)
			if m.ActiveIdx >= len(m.Sessions) {
				m.ActiveIdx = len(m.Sessions) - 1
			}
			m.refreshViewport()
		}
	}
	return &m, nil
}

// handleViewportKey processes keys when the chat viewport is focused.
func (m Model) handleViewportKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !m.Ready {
		return &m, nil
	}
	m.VP, _ = m.VP.Update(msg)
	return &m, nil
}

// handleTextareaKey processes keys when the textarea is focused.
// Most keys are forwarded to the textarea widget; we only intercept
// specific overrides in the global handler above.
func (m Model) handleTextareaKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.TextArea, cmd = m.TextArea.Update(msg)
	return &m, cmd
}

// cycleFocus moves FocusTarget by delta (+1 forward, -1 backward),
// updating textarea focus state accordingly.
func (m *Model) cycleFocus(delta int) {
	count := 3
	if delta > 0 {
		m.FocusTarget = (m.FocusTarget + 1) % count
	} else {
		m.FocusTarget = (m.FocusTarget - 1 + count) % count
	}
	switch m.FocusTarget {
	case FocusTextarea:
		m.TextArea.Focus()
	default:
		m.TextArea.Blur()
	}
}

// submitMessage sends the user's input to the agent.
func (m *Model) submitMessage(input string) {
	m.Sessions[m.ActiveIdx].AddToHistory(input)
	m.Sessions[m.ActiveIdx].AddMessage(llm.Message{Role: llm.RoleUser, Content: input})
	m.TextArea.Reset()
	m.refreshViewport()
	m.startStream(input)
}

// refreshViewport refreshes the viewport content for the active session.
func (m *Model) refreshViewport() {
	if !m.Ready {
		return
	}
	m.VP.SetContent(m.renderMessages())
	m.VP.GotoBottom()
}

// ---------------------------------------------------------------------------
// Streaming
// ---------------------------------------------------------------------------

// startStream launches the agent stream in a background goroutine. Chunks are
// pushed into the event loop via program.Send so the model stays single-threaded.
func (m *Model) startStream(input string) {
	m.Loading = true
	m.StreamingText = ""
	m.StreamingTool = ""

	go func() {
		sess := m.Sessions[m.ActiveIdx]
		req := &agent.AgentRequest{
			Messages: sess.Messages,
			Model:    m.ModelName,
			Tools:    m.Tools,
		}

		stream, err := m.Agent.StreamRun(context.Background(), req)
		if err != nil {
			m.Program.Send(streamDoneMsg{err: err})
			return
		}
		defer stream.Close()

		for stream.Next() {
			m.Program.Send(streamChunkMsg{chunk: stream.Current()})
		}

		if err := stream.Err(); err != nil {
			m.Program.Send(streamDoneMsg{err: err})
			return
		}
		m.Program.Send(streamDoneMsg{})
	}()
}

// flushStreamingText commits accumulated assistant text as a message.
func (m *Model) flushStreamingText() {
	if m.StreamingText == "" {
		return
	}
	sess := m.Sessions[m.ActiveIdx]
	sess.AddMessage(llm.Message{Role: llm.RoleAssistant, Content: m.StreamingText})
	m.StreamingText = ""
}

func (m Model) handleStreamChunk(msg streamChunkMsg) (tea.Model, tea.Cmd) {
	chunk := msg.chunk

	switch chunk.Type {
	case agent.AgentEventToken:
		m.StreamingText += chunk.Content

	case agent.AgentEventToolCall:
		// Flush any assistant text accumulated so far as its own message
		m.flushStreamingText()
		// Start a tool display block
		if chunk.ToolCall != nil {
			m.StreamingTool = fmt.Sprintf("🔧 %s", chunk.ToolCall.Function.Name)
		}

	case agent.AgentEventToolResult:
		if chunk.ToolResult != nil {
			result := chunk.ToolResult.Result
			// Format the result — truncate if very long
			display := strings.TrimSpace(result)
			if len(display) > 500 {
				display = display[:500] + "…"
			}
			// Build a clean tool message
			var toolMsg strings.Builder
			toolMsg.WriteString(fmt.Sprintf("🔧 %s", chunk.ToolResult.Name))
			if display != "" {
				toolMsg.WriteString("\n")
				toolMsg.WriteString(display)
			}
			// Store as a tool message
			sess := m.Sessions[m.ActiveIdx]
			sess.AddMessage(llm.Message{Role: llm.RoleTool, Content: toolMsg.String()})
			m.StreamingTool = ""
		}

	case agent.AgentEventIterationStart:
		// Intentionally omitted — no iteration markers
	}

	m.refreshViewport()
	return &m, nil
}

func (m Model) handleStreamDone(msg streamDoneMsg) (tea.Model, tea.Cmd) {
	m.Loading = false
	m.FocusTarget = FocusViewport // return focus to viewport
	m.StreamingTool = ""

	if msg.err != nil {
		m.Err = msg.err
		m.StreamingText = ""
		m.refreshViewport()
		return &m, nil
	}

	// Flush any remaining assistant text
	m.flushStreamingText()

	m.refreshViewport()
	return &m, nil
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

// View implements tea.Model.
func (m Model) View() string {
	if !m.Ready {
		return "\n  Initializing…"
	}

	// Guard against tiny terminals
	if m.Width < 40 || m.Height < 6 {
		return m.renderTooSmall()
	}

	var body string
	if m.ShowSidebar {
		left := m.renderSidebar()
		leftWidth := lipgloss.Width(left)
		rightTotal := m.Width - leftWidth + 2
		if rightTotal < 20 {
			rightTotal = 20
		}
		right := m.renderMain(rightTotal)
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	} else {
		body = m.renderMain(m.Width)
	}

	footer := m.renderFooter(m.Width)
	return lipgloss.JoinVertical(lipgloss.Top, body, footer)
}

// ---------------------------------------------------------------------------
// Sidebar
// ---------------------------------------------------------------------------

func (m Model) renderSidebar() string {
	// Calculate sidebar width — mirrors the logic in handleWindowSize
	spTotal := m.Width * 25 / 100
	maxSp := m.Width * 40 / 100
	if spTotal > maxSp {
		spTotal = maxSp
	}
	if spTotal < 10 {
		spTotal = 10
	}

	width := spTotal
	innerW := width - 2

	// Header
	header := RenderInLineBlock(" Sessions ", innerW)

	// Session list
	var items []string
	for i, s := range m.Sessions {
		content := s.Name
		if i == m.ActiveIdx {
			// Active session: bold with Powerline borders
			st := sessionStyle(true)
			items = append(items, RenderInLineBlockStyled(
				st, " "+content+" ", innerW-2, blue,
			))
		} else {
			// Inactive session: plain text
			st := sessionStyle(false)
			items = append(items, st.Copy().Padding(0, 0).MaxWidth(innerW-2).Render("  "+content+"  "))
		}
	}
	if len(items) == 0 {
		items = append(items, infoStyle.Render("  no sessions"))
	}
	list := strings.Join(items, "\n")

	body := lipgloss.JoinVertical(lipgloss.Top, header, "", list)

	// Border color indicates focus
	borderClr := gray
	if m.FocusTarget == FocusSidebar {
		borderClr = white
	}

	return leftPanelBg.
		BorderForeground(borderClr).
		Width(width - 2).
		Height(m.Height - 2 - footerHeight).
		Render(body)
}

// ---------------------------------------------------------------------------
// Main panel (viewport + textarea)
// ---------------------------------------------------------------------------

func (m Model) renderMain(totalWidth int) string {
	contentWidth := totalWidth - 4
	if contentWidth < 16 {
		contentWidth = 16
	}

	m.VP.Width = contentWidth

	// Viewport content includes the session header (set in renderMessages)
	vpContent := m.VP.View()

	// Spinner when loading
	if m.Loading {
		vpContent = vpContent + "\n" + m.SpinnerModel.View() + " Thinking…"
	}

	// Viewport border
	vpClr := gray
	if m.FocusTarget == FocusViewport {
		vpClr = white
	}
	vpBordered := vpBorderStyle.
		BorderForeground(vpClr).
		Width(contentWidth).
		Render(vpContent)

	// Textarea border
	taClr := gray
	if m.FocusTarget == FocusTextarea {
		taClr = white
	}
	taBordered := taBorderStyle.
		BorderForeground(taClr).
		Width(contentWidth).
		Render(m.TextArea.View())

	return lipgloss.JoinVertical(lipgloss.Top, vpBordered, taBordered)
}

// ---------------------------------------------------------------------------
// Message rendering
// ---------------------------------------------------------------------------

func (m Model) renderMessages() string {
	if len(m.Sessions) == 0 {
		return ""
	}

	active := m.Sessions[m.ActiveIdx]
	vpW := m.VP.Width
	if vpW < 20 {
		vpW = 20
	}

	var b strings.Builder

	// Session header inside the viewport (matching go-chat pattern)
	header := RenderInLineBlock(" "+active.Name+" ", vpW)
	b.WriteString(header)
	b.WriteString("\n\n")

	hasMessages := len(active.Messages) > 0
	hasStreamingText := m.StreamingText != ""
	hasStreamingTool := m.StreamingTool != ""
	hasLiveContent := hasStreamingText || hasStreamingTool

	if !hasMessages && !hasLiveContent {
		b.WriteString(infoStyle.Render("  No messages yet. Type below to start!"))
		return b.String()
	}

	// Calculate message block width
	msgW := vpW - 4
	if msgW < 10 {
		msgW = 10
	}

	// Create markdown renderer for the message content width
	mdWidth := msgW - 2
	if mdWidth < 8 {
		mdWidth = 8
	}

	chatMarkdownStyle := func() ansi.StyleConfig {
		cfg := styles.DarkStyleConfig
		zero := uint(0)
		cfg.Document.Margin = &zero
		cfg.Document.BlockPrefix = ""
		cfg.Document.BlockSuffix = ""
		return cfg
	}()

	mdRenderer, mdErr := glamour.NewTermRenderer(
		glamour.WithStyles(chatMarkdownStyle),
		glamour.WithWordWrap(mdWidth),
	)

	// Render each committed message
	for _, msg := range active.Messages {
		switch msg.Role {
		case llm.RoleUser:
			m.renderUserMessage(&b, msg, msgW, mdRenderer, mdErr)
		case llm.RoleAssistant:
			m.renderAssistantMessage(&b, msg, msgW, mdRenderer, mdErr)
		case llm.RoleTool:
			m.renderToolMessage(&b, msg, msgW)
		}
	}

	// Append live streaming content
	if hasStreamingTool {
		m.renderToolDisplay(&b, m.StreamingTool, msgW)
	}
	if hasStreamingText {
		m.renderStreamingContent(&b, m.StreamingText, msgW, mdRenderer, mdErr)
	}

	return b.String()
}

func (m Model) renderUserMessage(b *strings.Builder, msg llm.Message, msgW int, mdRenderer *glamour.TermRenderer, mdErr error) {
	label := senderLabelStyle(userClr).Render("You:")
	content := msg.Content
	if mdErr == nil && content != "" {
		rendered, err := mdRenderer.Render(content)
		if err == nil {
			content = strings.TrimRight(rendered, "\n")
		}
	}
	block := messageBlockStyle(bubbleClr, msgW).Render(content)
	fmt.Fprintf(b, "%s\n%s\n\n", label, block)
}

func (m Model) renderAssistantMessage(b *strings.Builder, msg llm.Message, msgW int, mdRenderer *glamour.TermRenderer, mdErr error) {
	label := senderLabelStyle(lipgloss.Color("214")).Render("Assistant:")
	content := msg.Content
	if mdErr == nil && content != "" {
		rendered, err := mdRenderer.Render(content)
		if err == nil {
			content = strings.TrimRight(rendered, "\n")
		}
	}
	block := messageBlockStyle(lipgloss.Color("214"), msgW).Render(content)
	fmt.Fprintf(b, "%s\n%s\n\n", label, block)
}

func (m Model) renderToolMessage(b *strings.Builder, msg llm.Message, msgW int) {
	// Render a committed tool message as a distinct block
	// Format: first line is "🔧 name", rest is the result content
	lines := strings.SplitN(msg.Content, "\n", 2)
	name := lines[0]
	result := ""
	if len(lines) > 1 {
		result = strings.TrimSpace(lines[1])
	}

	label := senderLabelStyle(lipgloss.Color("243")).Render(name)
	if result != "" {
		block := messageBlockStyle(lipgloss.Color("243"), msgW).Render(result)
		fmt.Fprintf(b, "%s\n%s\n\n", label, block)
	} else {
		b.WriteString(toolMsgStyle.Render(name))
		b.WriteString("\n\n")
	}
}

func (m Model) renderToolDisplay(b *strings.Builder, tool string, msgW int) {
	// Render a live (in-progress) tool display
	label := senderLabelStyle(lipgloss.Color("243")).Render(tool)
	b.WriteString(label)
	b.WriteString("\n\n")
}

func (m Model) renderStreamingContent(b *strings.Builder, streaming string, msgW int, mdRenderer *glamour.TermRenderer, mdErr error) {
	label := senderLabelStyle(lipgloss.Color("214")).Render("Assistant:")
	content := streaming
	if mdErr == nil && content != "" {
		rendered, err := mdRenderer.Render(content)
		if err == nil {
			content = strings.TrimRight(rendered, "\n")
		}
	}
	block := messageBlockStyle(lipgloss.Color("214"), msgW).Render(content)
	fmt.Fprintf(b, "%s\n%s\n\n", label, block)
}

// ---------------------------------------------------------------------------
// Footer
// ---------------------------------------------------------------------------

func (m Model) renderFooter(width int) string {
	helpText := "Tab cycle · ↑↓ select · Ctrl+N new · Del delete · Ctrl+O export · Ctrl+G sidebar · Esc quit"
	return RenderInLineBlock(" "+helpText+" ", width)
}

// ---------------------------------------------------------------------------
// Too-small terminal warning
// ---------------------------------------------------------------------------

func (m Model) renderTooSmall() string {
	warn := lipgloss.NewStyle().
		Foreground(lipgloss.Color("196")).
		Bold(true).
		Align(lipgloss.Center)
	return warn.Render("Terminal too small\nPlease resize to at least 40x6")
}

// ---------------------------------------------------------------------------
// Chat export
// ---------------------------------------------------------------------------

// exportChat writes the active session's messages to a timestamped text file.
func (m Model) exportChat() {
	if len(m.Sessions) == 0 {
		return
	}
	active := m.Sessions[m.ActiveIdx]

	var b strings.Builder
	b.WriteString("=================================\n")
	fmt.Fprintf(&b, "  Chat Export: %s\n", active.Name)
	fmt.Fprintf(&b, "  Exported:    %s\n", time.Now().Format("2006-01-02 15:04:05"))
	b.WriteString("=================================\n\n")

	if len(active.Messages) == 0 {
		b.WriteString("(no messages)\n")
	} else {
		for _, msg := range active.Messages {
			var sender string
			switch msg.Role {
			case llm.RoleUser:
				sender = "You"
			case llm.RoleAssistant:
				sender = "Assistant"
			case llm.RoleTool:
				sender = "Tool"
			default:
				sender = string(msg.Role)
			}
			fmt.Fprintf(&b, "%s:\n", sender)
			for _, line := range strings.Split(msg.Content, "\n") {
				fmt.Fprintf(&b, "  %s\n", line)
			}
			b.WriteString("\n")
		}
	}

	// Sanitize session name for filename
	safeName := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, active.Name)

	path := fmt.Sprintf("chat-%s-%s.txt", safeName, time.Now().Format("20060102-150405"))
	os.WriteFile(path, []byte(b.String()), 0644) // best-effort
}
