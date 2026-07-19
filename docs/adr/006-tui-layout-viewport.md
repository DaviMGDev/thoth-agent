# ADR 006: TUI Layout & Viewport Architecture

**Status**: Proposed

**Date**: 2026-07-17

## Context

The TUI (`cmd/thoth-agent/tui.go`) currently has a two-column layout: a side panel
(25% width) for session management and a main area for the chat viewport + text
input. Several structural issues exist:

1. **Side panel is always visible** — It consumes 25% of the terminal width even on
   narrow terminals, leaving limited space for the chat. There is no way to reclaim
   that space.

2. **Viewport does not scroll** — Key events (Up, Down, PgUp, PgDown, g, G) are never
   forwarded to the viewport model. Long conversations clip to the visible area with
   no way to scroll back.

3. **No scrollback limit** — Messages are appended to the session's `Messages` slice
   indefinitely. In a long session, this grows unbounded in memory and rendering
   cost.

4. **No resize boundaries** — When the terminal is very narrow, the hardcoded
   `spTotal = 24` minimum for the side panel and `mainContent = 20` minimum for the
   main area can conflict or produce unusable layouts.

5. **Side panel height is fixed** — It uses `m.height - 2` unconditionally, which
   doesn't account for the input area at the bottom of the main column.

These issues are structural — they affect how the model manages geometry, scrolling,
and message history — and benefit from a documented architecture rather than ad-hoc
fixes.

## Decision

### 1. Side Panel Toggle

Add a keybinding (**Ctrl+B**) to toggle the side panel on and off.

- When visible, the side panel takes 25% of terminal width (minimum 24, as today).
- When hidden, the main area reclaims the full terminal width.
- The toggled state is stored as a `showSidePanel bool` field on the model.
- Toggle is instantaneous — no animation.
- The side panel state persists across resizes.
- Ctrl+B is chosen because: no existing binding, easy to reach with one hand,
  conventional "sidebar toggle" binding in many editors (VS Code, Obsidian).

### 2. Viewport Scrolling

Forward the following key events to `m.vp` (the `viewport.Model`) whenever the
viewport has focus (see ADR 005):

| Key | Action |
|-----|--------|
| Up | Scroll up one line |
| Down | Scroll down one line |
| PgUp | Scroll up one page |
| PgDown | Scroll down one page |
| g | Scroll to top |
| G (Shift+g) | Scroll to bottom |
| Home | Scroll to top |
| End | Scroll to bottom |

Additionally, enable Bubble Tea's mouse wheel support on the viewport:
`tea.WithMouseCellMotion()` in `main.go`'s `tea.NewProgram` call, so the mouse
wheel works naturally.

### 3. Message History Limit

Cap the number of messages retained in a session's `Messages` slice to **500**.
When the limit is reached, the oldest messages are pruned:

```go
const maxMessagesPerSession = 500

func (s *Session) AddMessage(msg llm.Message) {
    s.Messages = append(s.Messages, msg)
    if len(s.Messages) > maxMessagesPerSession {
        // Prune oldest 25% to avoid pruning on every append
        prune := len(s.Messages) - maxMessagesPerSession + (maxMessagesPerSession / 4)
        s.Messages = s.Messages[prune:]
    }
}
```

Pruning removes 125 messages (25% of 500) at once when the limit is exceeded,
avoiding a re-slice on every single `AddMessage` call near the boundary.

The viewport scroll position is reset to bottom after pruning to avoid pointing
into removed history.

### 4. Minimum Width & Layout Guard

| Threshold | Behavior |
|-----------|----------|
| Width ≥ 80 | Normal two-column layout (side panel 25%, main area rest) |
| Width 50-79 | Two-column layout but side panel shrinks to minimum 20, main area gets priority |
| Width 40-49 | Side panel auto-hidden, main area takes full width (Ctrl+B still works to force-show) |
| Width < 40 | Layout falls back to a minimal single-column mode: no side panel, no borders, compact input |

These thresholds are checked in `handleWindowSize`. If the terminal shrinks below
the auto-hide threshold, `showSidePanel` is forced to false (but restored when
width recovers above the threshold).

### 5. Layout Height Fix

The side panel height should match the available height, not the full terminal
height minus 2. Current code uses `m.height - 2` which is correct for the outer
container but the side panel's content area should account for the same vertical
space division as the main area. Fix: compute a consistent `contentHeight` that
both columns share, subtracting the input box height and border overhead.

## Consequences

### Positive

- **Flexible layout** — Side panel toggle lets users reclaim space for long
  conversations.
- **Usable on small terminals** — Auto-hide and minimum thresholds prevent layout
  breakage.
- **Bounded memory** — Message history cap prevents unbounded growth.
- **Keyboard-accessible history** — Viewport scrolling works with standard keys.
- **Consistent height** — Both columns share the same vertical space calculation.

### Negative

- **Loss of old messages** — The 500-message cap means very old messages are
  dropped. Mitigation: 500 is generous for most chat sessions; persistence (future
  roadmap) would provide a full archive.
- **Side panel state management** — The `showSidePanel` flag must interact correctly
  with resize auto-hide. When the terminal shrinks below 50, the flag is forced
  false; expanding back above 50 restores the user's previous choice. A
  `userWantsSidePanel bool` separate from `showSidePanel` tracks user preference.

### Risks

- **Animation expectations** — Users might expect a smooth slide animation on
  toggle. Mitigation: animations in terminal TUIs are complex and depend on
  framerate control. Start with instant toggle; animation can be added later with
  Bubble Tea's animation model if demand exists.
- **Mouse wheel support adds dependency** — `tea.WithMouseCellMotion()` enables
  mouse tracking, which some terminal emulators handle poorly over SSH.
  Mitigation: mouse support is optional; keyboard scrolling is always available.
  If mouse tracking causes issues, users can disable it by removing the program
  option.

## Rejected Alternatives

1. **Always-on side panel (status quo)** — Rejected because it wastes width on
   narrow terminals and users may not need constant session visibility during a
   single-session conversation.

2. **Virtualized message list** — Render only visible messages rather than storing
   all rendered content. Rejected because Bubble Tea's `viewport.Model` already
   handles this — it only renders the visible portion of the content string.
   The `Messages` slice stores canonical data; the viewport manages display.

3. **Infinite scrollback** — No message cap. Rejected because memory is bounded;
   a cap protects against runaway sessions. 500 is a generous limit (at ~1KB per
   message, ~500KB per session).

4. **Animated side panel transition** — Rejected for v1 (see risk above).
