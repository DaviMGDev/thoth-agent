# TUI — Bubble Tea Chat Interface

> Notes on the TUI implementation, fixes applied, and design decisions.

## Overview

The TUI is a [Bubble Tea](https://github.com/charmbracelet/bubbletea) REPL for `my-agent`. It lives in `cmd/my-agent/` and provides an interactive chat experience with streaming responses, session management, and keyboard navigation.

## Component Structure

- `model` (`tui.go`) — top-level Bubble Tea model holding all state
- `Session` (`session.go`) — per-conversation state (messages, input history)
- Styles (`styles.go`) — lipgloss style definitions
- `main.go` — wiring, provider + tool setup, program start

### UI Layout

```
┌──────────────┬──────────────────────────────┐
│  Sessions    │  Chat Messages (viewport)     │
│              │                               │
│  ▶ Session 1 │                               │
│    Session 2 │                               │
│              │                               │
│              ├──────────────────────────────┤
│  Tab cycle   │  Text Input (textarea)        │
│  ↑↓ select   │                               │
│  Del delete  │                               │
│  Ctrl+N new  │                               │
│  Esc quit     │                               │
└──────────────┴──────────────────────────────┘
```

### Focus Zones

| Index | Component      | Tab/Shift+Tab cycles through |
|-------|----------------|------------------------------|
| 0     | Text input     | Default, pink border         |
| 1     | Chat viewport  | Scrollable via keys/mouse    |
| 2     | Side panel     | Session list navigation      |

## Fixes Applied

### 1. Panic on Scroll Up (Empty History)

**Issue**: Pressing Up arrow in a new session with no input history caused an index-out-of-range panic (`index out of range [-1]`). The code computed `sess.HistoryPos = len(sess.InputHistory) - 1` which equals `-1` for an empty slice, and then used that as a slice index.

**Fix** (`tui.go:handleInputKey`): Added an early return when `len(sess.InputHistory) == 0` before entering history navigation. A new session's first Up press now safely no-ops.

```go
if msg.Type == tea.KeyUp {
    if len(sess.InputHistory) == 0 {
        return &m, nil
    }
    // ...
}
```

### 2. Textarea Configuration

**Issues**:
- Textarea had a fixed height of 5 lines with no lower bound, and a black cursor-line background inherited from the default `textarea.FocusedStyle.CursorLine`.
- The viewport used a hardcoded `5 + 2` height calculation that didn't adapt when the textarea height changed.

**Fixes**:

- **Min/Max height**: The textarea now starts at `SetHeight(1)` with `MaxHeight = 5`. The internal `textarea.SetHeight` clamps between `minHeight` (1) and `MaxHeight` (5), giving the textarea a truly flexible 1–5 line range.

- **Black background removed**: Default `textarea.FocusedStyle.CursorLine` had `Background(lipgloss.AdaptiveColor{Light: "255", Dark: "0"})` which renders as black on dark terminals. Overridden with `lipgloss.NewStyle()` (no background), making the cursor line transparent and matching the terminal theme.

- **Dynamic height adjustment**: After each `textInput.Update()` in `handleInputKey`, the current `LineCount()` is checked. If it differs from the current `Height()`, `SetHeight` is called to match the content, clamped to [1, 5]. This causes the textarea to grow as the user types and shrink when content is deleted or submitted.

- **Proportional viewport shrinking**: The viewport height is recalculated whenever the textarea height changes, so it always fills the remaining vertical space.

### 3. Layout Height Fix

**Issue**: The main chat area overflowed the terminal vertically by 2 lines, causing the textarea's content and borders to be pushed off-screen at the bottom. On a 24-row terminal with a 1-line textarea, the textarea was almost entirely invisible. The side panel appeared to "respect" the textarea because `lipgloss.JoinHorizontal` padded the main area to match, creating visual coupling.

**Root cause**: `handleWindowSize` computed `vpHeight = m.height - textInput.Height() - 2`, but didn't account for the message box's border (2 additional lines). Total main area: `vpHeight + textInput.Height() + 4 = m.height + 2` — always 2 lines too tall.

**Fix**: Per ADR 006 §5, the viewport **reserves the maximum textarea height** plus all border overhead, keeping the viewport stable regardless of the textarea's current height:

```go
vpHeight := m.height - m.textInput.MaxHeight - 4
//                               ^^^             ^
//                               |               |
//                          max textarea    2 msgBox
//                          lines (5)      + 2 inputBox
```

The textarea grows from 1 to 5 lines **within its reserved box** — `SetHeight()` adjusts the textarea's visible height, but the viewport does NOT change. The side panel is independent (full `m.height`).

Total main area at max textarea: `(m.height - 9 + 2) + (5 + 2) = m.height` ✓
Total main area at min textarea: `(m.height - 9 + 2) + (1 + 2) = m.height - 4` (underflow is harmless — `JoinHorizontal` pads).

### 4. Block Component Removal — Investigation

**Finding**: `git log --all -p -- cmd/my-agent/` was searched for any use of a "block" component or model. No matches were found in the git history or current code. The `docs/notes/tui-feedback.md` mentions `InlineBlock` and `Block` as feature requests only — they were never implemented.

**Status**: No block components to remove. Zero references in code.

## Key Bindings

| Key               | Action                      |
|-------------------|-----------------------------|
| Enter             | Submit message              |
| Up / Down         | Input history navigation    |
| Tab / Shift+Tab   | Cycle focus (3 zones)       |
| Ctrl+B            | Toggle side panel           |
| Ctrl+N            | New session (side panel)    |
| Del / Backspace   | Delete session (side panel) |
| Esc / Ctrl+C      | Quit                        |

## Layout Calculation

The main chat area is composed of two bordered boxes stacked vertically. The viewport **reserves space for the maximum textarea height** (ADR 006 §5), so the viewport height is stable while the textarea grows from 1 to 5 lines within its reserved box.

```
┌──────────────────────┐  ← msgBox top border (1 line)
│ viewport content     │  ← vpHeight lines (stable)
│                      │
└──────────────────────┘  ← msgBox bottom border (1 line)
┌──────────────────────┐  ← inputBox top border (1 line)
│ textarea content     │  ← textInput.Height() lines (dynamic: 1–5)
│ (padding if < 5)     │
└──────────────────────┘  ← inputBox bottom border (1 line)
```

```
textInput.MaxHeight = 5                     (fixed maximum)
textInput.Height ∈ [1, 5]                   (dynamic, based on content lines)
borderOverhead     = 4                      (2 for msgBox + 2 for inputBox)
vpHeight           = windowHeight - MaxHeight - 4
vpHight            = max(vpHeight, 10)      (minimum viewport)
```

The side panel uses `Height(m.height - 2)` which, with its border (+2 lines), covers the full terminal height — independent of the textarea and viewport.

## Future Considerations

- **Enter vs newline**: The textarea currently processes Enter internally (adds a newline) and then the TUI handler also reacts to Enter (submits). For a chat application, Shift+Enter for newline and plain Enter for submit would be a better UX. This is outside the current scope.
- **Side panel width**: Currently a fixed percentage (25%). Could be made user-configurable.
