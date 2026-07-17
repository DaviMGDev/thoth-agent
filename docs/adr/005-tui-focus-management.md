# ADR 005: TUI Focus Management

**Status**: Proposed

**Date**: 2026-07-17

## Context

The Bubble Tea TUI (`cmd/my-agent/tui.go`) has three interactive regions:

1. **Text input** — where the user types messages
2. **Chat messages viewport** — the scrollable message history
3. **Side panel** — session list with create/switch/delete

Currently, focus is permanently locked to the text input. Tab/Shift+Tab cycles
sessions within the side panel (changing `activeIdx`) but never moves focus to the
viewport or to the side panel as a whole. This means:

- The viewport cannot be scrolled with keyboard keys (no component receives them).
- The side panel cannot be navigated independently (Tab currently cycles sessions,
  conflating focus movement with session selection).
- Users who want to scroll back through history, select a different session, or
  review past tool results have no keyboard path to do so — they're limited to
  whatever the text input allows.

Bubble Tea does not provide a built-in focus manager. Focus is implicit: whichever
component receives key messages in `Update()` has focus. To implement multi-component
focus, we need a model that tracks which component is active and routes key events
accordingly.

## Decision

Introduce an explicit focus management model with three focusable components cycling
in a fixed order.

### Focusable Components

| Index | Component | Purpose |
|-------|-----------|---------|
| 0 | text input (`textinput.Model`) | Typing and submitting messages |
| 1 | viewport (`viewport.Model`) | Scrolling through message history |
| 2 | side panel | Session list navigation (select, create, rename, delete) |

### Focus Cycle

- **Tab** advances focus forward: 0 → 1 → 2 → 0.
- **Shift+Tab** advances focus backward: 0 → 2 → 1 → 0.
- The cycle wraps around in both directions.
- Focus starts on the text input (index 0) when the TUI launches.

### Visual Focus Indicators

Each component changes its border color when focused:

| Component | Unfocused Border | Focused Border |
|-----------|-----------------|----------------|
| Text input | `"62"` (purple) | `"212"` (pink) |
| Viewport | `"62"` (purple) | `"212"` (pink) |
| Side panel | `"62"` (purple) | `"212"` (pink) |

The focused border color matches `activeSessionStyle`'s foreground (`"212"`), creating
a consistent visual language for "active/selected" across the UI.

### Interaction Model Per Focus State

| When focused… | Keys accepted |
|---------------|--------------|
| **Text input** | All text input keys, Enter (submit), Up/Down (history navigation, see ADR 007) |
| **Viewport** | Up/Down (scroll one line), PgUp/PgDown (scroll one page), g/G (top/bottom), mouse wheel |
| **Side panel** | Up/Down (select session), Enter (switch to selected), Ctrl+N (new session), Delete (delete selected), Tab/Shift+Tab (maintained for cycle) |

### Model Change

Add a `focusIndex int` field to the TUI `model` struct (values 0-2). The
`handleKeyMsg` method routes key events to the focused component before falling
through to global keybindings (Ctrl+C, Ctrl+Q).

```go
type model struct {
    // ... existing fields ...

    focusIndex int // 0=text input, 1=viewport, 2=side panel
}
```

Key events that are global (Ctrl+C to quit, Tab/Shift+Tab for focus cycle) are
handled before component dispatch. Component-specific keys are only processed when
that component has focus.

## Consequences

### Positive

- **Deterministic focus** — Users always know which component will respond to keys.
- **No ambiguous keybindings** — Each key does one thing depending on focus.
- **Bubble Tea idiomatic** — A single `focusIndex` int and conditional dispatch in
  `Update()` is the standard pattern in multi-component Bubble Tea apps.
- **Accessible** — Keyboard-only navigation across all parts of the TUI.
- **Non-breaking** — Existing session-switching on Tab is repurposed into side-panel
  focus, which makes more semantic sense.

### Negative

- **One more state field** — The model gains `focusIndex` which must be preserved
  across renders and resets.
- **Tab overload** — Tab is now overloaded: focus cycle + (when side panel focused)
  it's also the key to switch sessions. Mitigation: when the side panel is focused,
  Tab still cycles sessions; Enter commits the selection. This is intuitive — Tab
  moves selection, Enter confirms.
- **Shift+Tab wasn't used before** — No migration burden.

### Risks

- **Terminal emulator intercepts Tab** — Some terminals consume Tab for GUI focus
  cycling. Mitigation: Bubble Tea runs in raw mode (`tea.WithAltScreen()`), so Tab
  is delivered as a key event. Confirmed in current code: `tea.KeyTab` is handled
  in `handleKeyMsg`.
- **Shift+Tab may conflict with terminal** — Likewise delivered in raw mode.
- **Users expect Tab to complete/indent** — In a chat TUI, Tab for indentation is
  rare. If needed later, Ctrl+I can serve as an alias.

## Rejected Alternatives

1. **Vim-style focus movement** (Ctrl+J/K or Ctrl+W+J/K) — Rejected because it
   adds a learning curve. Tab/Shift+Tab is the universal "next/previous" convention
   in terminal UIs and matches browser/system dialog behavior.

2. **Mouse click to focus** — Rejected because not all terminals support mouse events
   reliably, and the TUI should be fully keyboard-accessible. Mouse scroll on the
   viewport is supported when the viewport is focused (via Bubble Tea's mouse
   tracking), but click-to-focus is not the primary mechanism.

3. **No focus management (status quo)** — Rejected because it permanently locks the
   user to the text input, making the viewport and side panel inaccessible via
   keyboard.

4. **Focus on hover** — Rejected because terminal emulators don't reliably report
   hover position in a way that Bubble Tea can consume.
