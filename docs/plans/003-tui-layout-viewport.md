# Plan 003: TUI Layout & Viewport

**Status**: Planned

**Date**: 2026-07-17

**ADRs**: ADR 006

## Design Decisions

| # | Decision | Rationale |
|---|----------|-----------|
| 1 | Ctrl+B toggles side panel | Unused keybinding, easy one-hand reach, matches editor convention (VS Code, Obsidian) |
| 2 | Side panel auto-hides below 50-width terminal | Prevents layout breakage on small terminals |
| 3 | Message history capped at 500 per session | Bounded memory; 500 is generous for a chat session |
| 4 | Prune 25% (125 messages) at once when limit exceeded | Avoids O(n) re-slice on every AddMessage near boundary |
| 5 | Mouse wheel enabled on viewport via `tea.WithMouseCellMotion()` | Standard UX for scrollable content; falls back to keyboard if unsupported |

## Files to Modify

| File | Changes |
|------|---------|
| `cmd/my-agent/tui.go` | Add `showSidePanel`/`userWantsSidePanel` fields; Ctrl+B handler; viewport key forwarding; layout reflow on toggle; resize guards |
| `cmd/my-agent/main.go` | Add `tea.WithMouseCellMotion()` to program options |
| `cmd/my-agent/session.go` | Add `AddMessage` with cap/prune logic |
| `cmd/my-agent/styles.go` | Add side panel hidden style if needed |

## Files NOT Touched

| File | Reason |
|------|--------|
| `internal/agent/agent.go` | Agent logic unchanged |
| `internal/llm/llm.go` | Core types unchanged |
| `internal/bus/` | Bus unchanged |
| `internal/providers/` | Providers unchanged |

## Implementation Details

### 1. Add layout state fields

Location: `cmd/my-agent/tui.go`, `model` struct

```go
type model struct {
    // ... existing fields ...

    showSidePanel     bool  // actual visibility (may be forced by resize)
    userWantsSidePanel bool // user's preference (restored after resize un-squishes)
}
```

Initialize both to `true` in `initialModel()`.

### 2. Ctrl+B toggle handler

In `handleKeyMsg`, add:

```go
case tea.KeyCtrlB:
    if !m.loading {
        m.userWantsSidePanel = !m.userWantsSidePanel
        m.showSidePanel = m.userWantsSidePanel
        m.handleWindowSize(tea.WindowSizeMsg{Width: m.width, Height: m.height})
    }
```

### 3. Update `handleWindowSize` with resize guards

Current code at lines 121-145. Restructure:

```go
func (m model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
    m.width = msg.Width
    m.height = msg.Height

    // Resize guards — auto-hide side panel on narrow terminals
    forcedHide := msg.Width < 50
    if forcedHide {
        m.showSidePanel = false
    } else if m.userWantsSidePanel {
        m.showSidePanel = true
    }

    // Compute side panel width
    var spTotal int
    if m.showSidePanel {
        overhead := 4
        spTotal = msg.Width * 25 / 100
        if msg.Width >= 80 && spTotal < 24 {
            spTotal = 24
        } else if msg.Width < 80 && spTotal < 20 {
            spTotal = 20
        }
    }

    // Main area width
    mainContent := msg.Width - spTotal - 4
    if mainContent < 20 {
        mainContent = 20
    }
    if !m.showSidePanel {
        mainContent = msg.Width - 4
    }

    // Vertical: viewport + input
    inputBoxHeight := 3 + 2 // 3 content rows + 2 border rows
    if m.inputIsTextarea {
        inputBoxHeight = 5 + 2 // max 5 content rows + 2 border rows
    }
    vpHeight := msg.Height - inputBoxHeight
    if vpHeight < 10 {
        vpHeight = 10
    }

    // Apply dimensions
    if !m.ready {
        m.vp = viewport.New(mainContent, vpHeight)
        m.ready = true
    } else {
        m.vp.Width = mainContent
        m.vp.Height = vpHeight
    }
    m.textInput.Width = mainContent // or textarea

    return &m, nil
}
```

### 4. Side panel height fix

In `renderSidepanel`, change from `m.height - 2` to use `m.height` minus borders
(consistent with the main area's actual height). The side panel content should
match the viewport + input area height, not the full terminal height.

```go
func (m model) renderSidepanel(width int) string {
    // ...
    panelHeight := m.height - 2 // subtract outer container borders
    // But also account for the input area row count to match main column
    return sidePanelStyle.Width(width).Height(panelHeight).Render(b.String())
}
```

### 5. Viewport key forwarding (when focused)

Moving keys to viewport is done via `handleViewportKey` in Plan 002. The viewport
model from `bubbles/viewport` natively handles `tea.KeyUp`, `tea.KeyDown`,
`tea.KeyPgUp`, `tea.KeyPgDown`, `tea.KeyHome`, `tea.KeyEnd`. It also handles `g`
and `G` for top/bottom via its `Update` method.

Simply forward the key event:

```go
func (m model) handleViewportKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    m.vp, _ = m.vp.Update(msg)
    return &m, nil
}
```

### 6. Enable mouse wheel in main.go

In `cmd/my-agent/main.go`, change:

```go
// Before:
p := tea.NewProgram(&m, tea.WithAltScreen())

// After:
p := tea.NewProgram(&m, tea.WithAltScreen(), tea.WithMouseCellMotion())
```

This enables mouse wheel and cell-level mouse tracking on the viewport.

### 7. Message history cap in session.go

```go
const MaxMessagesPerSession = 500

func (s *Session) AddMessage(msg llm.Message) {
    s.Messages = append(s.Messages, msg)
    if len(s.Messages) > MaxMessagesPerSession {
        prune := len(s.Messages) - MaxMessagesPerSession + (MaxMessagesPerSession / 4)
        s.Messages = s.Messages[prune:]
    }
}
```

### 8. Update refreshViewport for streaming reply

In `startStream`, the reply builder accumulates content into `m.replyBuf`. The
viewport already calls `GotoBottom()` after each chunk. No changes needed to
the streaming loop — it already scrolls to bottom on new content. The new
scrolling keys only apply when the user manually scrolls up.

## Verification

- [ ] `go test ./...` — all tests pass
- [ ] Ctrl+B toggles side panel on/off
- [ ] Side panel state persists through resize (unless forced by <50 width)
- [ ] Terminal <50 width auto-hides side panel
- [ ] Terminal ≥50 (and user wants it) re-shows side panel
- [ ] Viewport scrolls with Up/Down/PgUp/PgDown when focused
- [ ] `g` scrolls to top, `G` scrolls to bottom
- [ ] Mouse wheel scrolls the viewport
- [ ] Adding 501+ messages prunes oldest entries
- [ ] Layout recalculates correctly on toggle (no gaps, no overlap)
- [ ] Narrow terminal (<40) renders compact layout without breakage
- [ ] Side panel height matches main area height
