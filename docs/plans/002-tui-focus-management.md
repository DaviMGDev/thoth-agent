# Plan 002: TUI Focus Management

**Status**: Planned

**Date**: 2026-07-17

**ADRs**: ADR 005

## Design Decisions

| # | Decision | Rationale |
|---|----------|-----------|
| 1 | `focusIndex int` tracks active component (0=input, 1=viewport, 2=sidepanel) | Single integer is the simplest model; Bubble Tea idiom for multi-component apps |
| 2 | Tab advances focus forward, Shift+Tab backward | Universal convention across browsers, dialog boxes, and terminal UIs |
| 3 | Focused component gets pink border `"212"`, unfocused gets purple `"62"` | Pink is already used for active sessions (activeSessionStyle); consistent visual language |
| 4 | Global keys (Ctrl+C, Tab/Shift+Tab cycle) handled before component dispatch | Prevents a focused component from hijacking navigation keys |
| 5 | Component-specific keys only processed when that component is focused | Clean separation of concerns; no ambiguous keybindings |

## Files to Modify

| File | Changes |
|------|---------|
| `cmd/thoth-agent/tui.go` | Add `focusIndex` field; rewrite `handleKeyMsg` to dispatch per focus; component-specific handlers |
| `cmd/thoth-agent/styles.go` | Add focused/unfocused border color variants for each panel |

## Files NOT Touched

| File | Reason |
|------|--------|
| `cmd/thoth-agent/main.go` | Entry point unchanged |
| `cmd/thoth-agent/session.go` | Session model unchanged |
| `internal/agent/agent.go` | Agent logic unchanged |
| `internal/llm/llm.go` | Core types unchanged |

## Implementation Details

### 1. Add `focusIndex` to the model struct

Location: `cmd/thoth-agent/tui.go`, `model` struct (around line 37)

```go
type model struct {
    // ... existing fields ...

    focusIndex int // 0 = text input, 1 = viewport, 2 = side panel
}
```

Initialize to 0 in `initialModel()`.

### 2. Rewrite `handleKeyMsg` for focus dispatch

Current `handleKeyMsg` (lines 160-195) handles all keys inline. Restructure into
a dispatch pattern:

```
handleKeyMsg(msg):
    // 1. Global keys (handled regardless of focus)
    if msg.Type == CtrlC or Esc → quit
    if msg.Type == Tab → advance focusIndex forward (0→1→2→0), return
    if msg.Type == ShiftTab → advance focusIndex backward (0→2→1→0), return

    // 2. Component dispatch
    switch focusIndex:
        case 0 (input): return handleInputKey(msg)
        case 1 (viewport): return handleViewportKey(msg)
        case 2 (sidepanel): return handleSidepanelKey(msg)
```

Each handler method returns `(tea.Model, tea.Cmd)` and only processes
component-relevant keys.

### 3. Component-specific handlers

**handleInputKey** — current textinput logic (Enter to submit, text input), plus
Up/Down for input history (see Plan 004).

```go
func (m model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    if !m.ready {
        return &m, nil
    }
    var cmd tea.Cmd
    m.textInput, cmd = m.textInput.Update(msg)
    if msg.Type == tea.KeyEnter {
        input := strings.TrimSpace(m.textInput.Value())
        if input != "" {
            m.textInput.SetValue("")
            m.startStream(input)
        }
    }
    return &m, cmd
}
```

**handleViewportKey** — forward scroll keys to `m.vp`.

```go
func (m model) handleViewportKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    m.vp, _ = m.vp.Update(msg) // viewport handles Up/Down/PgUp/PgDown/g/G natively
    return &m, nil
}
```

**handleSidepanelKey** — session navigation (Up/Down to select, Enter to switch,
Ctrl+N for new, Delete for removal).

```go
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
        // Switch to selected session (already active; no-op unless
        // we add a "go to session" vs "preview" distinction later)
    case tea.KeyCtrlN:
        // New session
        id := len(m.sessions)
        sess := NewSession(id, fmt.Sprintf("Session %d", id+1))
        m.sessions = append(m.sessions, sess)
        m.activeIdx = id
        m.refreshViewport()
    case tea.KeyDelete, tea.KeyBackspace:
        // Delete session (minimum 1 kept)
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
```

### 4. Update `renderSidepanel` and `renderMain` for focus styling

In `styles.go`, add:

```go
var (
    // Focused border variants
    focusedBorderStyle = lipgloss.NewStyle().
                Border(lipgloss.RoundedBorder()).
                BorderForeground(lipgloss.Color("212"))

    unfocusedBorderStyle = lipgloss.NewStyle().
                Border(lipgloss.RoundedBorder()).
                BorderForeground(lipgloss.Color("62"))
)
```

In `renderSidepanel`, apply focused/unfocused border based on `m.focusIndex == 2`.
In `renderMain`, apply focused/unfocused border on the messages box based on
`m.focusIndex == 1`, and on the input box based on `m.focusIndex == 0`.

### 5. Remove old session-switching Tab logic

Delete the current Tab/Shift+Tab handling in `handleKeyMsg` that cycles
`sessions[activeIdx]`. This is replaced by side-panel focus handling where
Tab cycles focus, and within the side panel, Up/Down selects sessions.

## Verification

- [ ] `go test ./cmd/thoth-agent/` — passes
- [ ] Tab cycles focus: input → viewport → side panel → input
- [ ] Shift+Tab cycles focus in reverse
- [ ] Focused component has pink border, unfocused has purple
- [ ] When input focused: typing works, Enter submits, history works
- [ ] When viewport focused: Up/Down/PgUp/PgDown scroll, g/G go to top/bottom
- [ ] When side panel focused: Up/Down selects session, Ctrl+N creates, Delete removes
- [ ] Ctrl+C quits from any focus state
- [ ] Minimum one session always exists (delete guard)
- [ ] Focus resets to input after stream completes
