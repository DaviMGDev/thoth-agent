# Plan 004: TUI Input System

**Status**: Planned

**Date**: 2026-07-17

**ADRs**: ADR 007

## Design Decisions

| # | Decision | Rationale |
|---|----------|-----------|
| 1 | Replace `textinput` with `textarea` for multiline support | `textarea` wraps text, grows vertically up to 5 lines, then scrolls internally |
| 2 | Input history per-session, 50 entries max | Bounded memory; per-session avoids cross-session confusion |
| 3 | Up/Down arrows navigate history | Matches bash/readline muscle memory |
| 4 | In-progress text saved in `PendingInput` when browsing history, restored on return | Prevents data loss |
| 5 | Edited recalled entries create new history entries | Preserves the original; natural shell semantics |

## Files to Modify

| File | Changes |
|------|---------|
| `cmd/thoth-agent/tui.go` | Replace `textinput.Model` with `textarea.Model`; add history fields; history key handling |
| `cmd/thoth-agent/session.go` | Add `InputHistory`, `HistoryPos`, `PendingInput` fields; history management methods |
| `cmd/thoth-agent/styles.go` | Add textarea-specific styles if needed |

## Files NOT Touched

| File | Reason |
|------|--------|
| `cmd/thoth-agent/main.go` | Entry point unchanged |
| `internal/agent/agent.go` | Agent logic unchanged |
| `internal/llm/llm.go` | Core types unchanged |
| `internal/bus/` | Bus unchanged |

## Implementation Details

### 1. Replace `textinput` with `textarea` in model

Current (line 38):
```go
textInput    textinput.Model
```

Change to:
```go
textInput    textarea.Model
```

In `initialModel()` (line 53), replace:

```go
ti := textinput.New()
ti.Placeholder = "Type a message…"
ti.Focus()
ti.CharLimit = 4096
ti.Width = 60
```

With:

```go
ti := textarea.New()
ti.Placeholder = "Type a message…"
ti.Focus()
ti.CharLimit = 4096
ti.MaxHeight = 5
ti.ShowLineNumbers = false
ti.SetWidth(60)
```

Remove the standalone `spinner` field — the spinner is already embedded in the
model's streaming state. (It can stay; no harm, but verify it's not redundant.)

### 2. Update Session struct

```go
type Session struct {
    ID       int
    Name     string
    Messages []llm.Message

    // Input history
    InputHistory []string // ring buffer, newest last
    HistoryPos   int      // -1 = not browsing, 0..len-1 = browsing
    PendingInput string   // saved input when user starts browsing
}

func NewSession(id int, name string) *Session {
    return &Session{
        ID:           id,
        Name:         name,
        Messages:     make([]llm.Message, 0),
        InputHistory: make([]string, 0, 51), // 50 + 1 before eviction
        HistoryPos:   -1,
    }
}

const maxHistoryEntries = 50

func (s *Session) AddToHistory(input string) {
    // Don't add duplicates of the last entry
    if len(s.InputHistory) > 0 && s.InputHistory[len(s.InputHistory)-1] == input {
        return
    }
    s.InputHistory = append(s.InputHistory, input)
    if len(s.InputHistory) > maxHistoryEntries {
        s.InputHistory = s.InputHistory[1:] // evict oldest
    }
    s.HistoryPos = -1
    s.PendingInput = ""
}
```

### 3. History key handling in `handleInputKey`

When input is focused and Up/Down is pressed, navigate history:

```go
func (m model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    if !m.ready {
        return &m, nil
    }

    sess := m.sessions[m.activeIdx]

    // History navigation
    if msg.Type == tea.KeyUp {
        if len(sess.InputHistory) == 0 {
            return &m, nil // no-op on empty history
        }
        if sess.HistoryPos == -1 {
            // Save current input before browsing
            sess.PendingInput = m.textInput.Value()
            sess.HistoryPos = len(sess.InputHistory) - 1
        } else if sess.HistoryPos > 0 {
            sess.HistoryPos--
        }
        m.textInput.SetValue(sess.InputHistory[sess.HistoryPos])
        m.textInput.SetCursor(len(sess.InputHistory[sess.HistoryPos]))
        return &m, nil
    }

    if msg.Type == tea.KeyDown {
        if sess.HistoryPos == -1 {
            return &m, nil // not browsing
        }
        sess.HistoryPos++
        if sess.HistoryPos >= len(sess.InputHistory) {
            // Return to pending input
            sess.HistoryPos = -1
            m.textInput.SetValue(sess.PendingInput)
            m.textInput.SetCursor(len(sess.PendingInput))
        } else {
            m.textInput.SetValue(sess.InputHistory[sess.HistoryPos])
            m.textInput.SetCursor(len(sess.InputHistory[sess.HistoryPos]))
        }
        return &m, nil
    }

    // Regular text input
    var cmd tea.Cmd
    m.textInput, cmd = m.textInput.Update(msg)

    if msg.Type == tea.KeyEnter {
        input := strings.TrimSpace(m.textInput.Value())
        if input != "" {
            m.textInput.SetValue("")
            sess.AddToHistory(input)
            m.startStream(input)
        }
    }

    return &m, cmd
}
```

### 4. Update `startStream` reference

`startStream` currently sets `m.textInput.SetValue("")` which works identically
for both `textinput` and `textarea`. No change needed.

### 5. Update `renderMain` for textarea height

In `renderMain`, the input box height was hardcoded to 3. With `textarea`, the
input area may be up to 5+2=7 lines. The layout calculation in `handleWindowSize`
(Plan 003) already accounts for this via `inputBoxHeight`.

The `inputAreaStyle` width must match the textarea width:

```go
m.textInput.SetWidth(contentWidth)
inputLine = m.textInput.View()
```

### 6. Update imports

Remove `"github.com/charmbracelet/bubbles/textinput"` and add
`"github.com/charmbracelet/bubbles/textarea"` in `tui.go`.

### 7. Wire `AddToHistory` into stream flow

In `handleStreamDone`, after a successful reply, the input has already been
saved to history (it was saved when Enter was pressed in `handleInputKey`).
If the stream fails, the input should remain in history (the user typed it;
failure doesn't undo the action). No additional wiring needed — `AddToHistory`
is called on Enter before `startStream`.

## Verification

- [ ] `go build ./cmd/thoth-agent/` — compiles successfully
- [ ] Text input wraps at the box boundary instead of scrolling horizontally
- [ ] Input grows vertically up to 5 lines as text is typed
- [ ] Beyond 5 lines, textarea scrolls its content (cursor stays at bottom)
- [ ] Up Arrow recalls last submitted message
- [ ] Up Arrow again recalls the one before that
- [ ] Down Arrow returns to more recent history entries
- [ ] Down Arrow at the newest entry restores the in-progress text
- [ ] Empty history: Up/Down do nothing (no crash)
- [ ] Editing a recalled entry and pressing Enter submits the edited version and adds it to history as a new entry
- [ ] Original entry is preserved unchanged
- [ ] History is per-session (switching sessions shows different history)
- [ ] 50-entry cap: 51st entry evicts the oldest
- [ ] Duplicate consecutive entries are not added
- [ ] Streaming still works after input system changes
