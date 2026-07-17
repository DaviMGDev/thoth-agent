# TUI Feedback — Feature Requests & Issues

> Captured on 2026-07-17. Last updated 2026-07-17. No implementation or planning intended — just a record of ideas and problems encountered while using the TUI.

## Issues

### 1. Viewport Not Scrollable

The main chat viewport does not respond to scroll input (mouse wheel, scroll keys, etc.). Long conversations or multi-turn responses are clipped to the viewport height with no way to scroll back through earlier messages.

### 2. Text Input Scrolls Horizontally Instead of Wrapping

When typing a long message, the text input scrolls to the right (inline horizontal scroll) rather than wrapping text to the next line. Expected behavior:

- Text wraps at the input box boundary.
- Input grows vertically (max ~5 lines), then scrolls its content.
- The view scrolls to **bottom** so the user always sees what they're typing.

### 3. Unintentional Input Loss When Scrolling Text History

When scrolling the text input area to review previously entered text, the current in-progress input is lost. The current input should be **preserved** when the user scrolls up to view earlier input lines.

## Feature Requests

### 4. Focus Navigation Between TUI Components

Ability to change keyboard focus between the major TUI components:

- Text input area
- Chat messages viewport
- Side panel (session list)

Currently focus is locked to the text input. Expected: Tab/Shift+Tab (or another keybinding) cycles focus across components.

### 5. Toggle Side Panel

A keybinding to show/hide the session side panel. When hidden, the main chat area should reclaim the full terminal width.

### 6. Text Input History

A history of previously submitted user inputs, navigable via **Up Arrow / Down Arrow** while the text input is focused. This mirrors shell behavior (like readline's `history-search-backward`). Expectations:

- Up Arrow shows the most recent input, Up again shows the one before that, etc.
- Empty history does nothing (no crash or weird state).
- If a history entry is selected and then edited, the edited version is what gets submitted (not the original).
- The history persists across sessions for the current TUI session (in-memory is fine).

## New TUI Models to Add

### 7. `InlineBlock` Model

A **single-line** rendering primitive. What makes it a "block" is the presence of a powerline glyph border (no vertical borders — just the top/bottom glyphs). Suitable for tool call headers, system banners, iteration markers, status lines, etc.

```codify
model InlineBlock {
  content: string
  border: string   // none, square, pointed, slanted, round
                   // all powerline glyphs, no vertical borders
  padding: (int, int)   // (horizontal, vertical)
  background: color
  foreground: color
  dimmed: boolean
  wrap: boolean    // if false, truncate
  weight: string   // none | fill | wrap
                   // none: use padding normally
                   // fill: fill the whole parent width, ignores padding
                   // wrap: size respects content size, ignores padding
  position: string // none | left | right | center
                   // none: respects padding
                   // left/right/center: aligns content accordingly, ignores padding
}
```

### 8. `Block` Model

A **multi-line** text block. Uses normal ASCII/Unicode box-drawing borders (all four sides). Suitable for chat messages, code blocks, JSON responses, etc.

Updated model definition (replaces the one in ADR 003):

```codify
model Border {
  type: string // none, round, square. by default, for now, only single line is supported. might add custom chars support someday.
  background: color
  foreground: color
}

model Block {
  content:    string
  type:       string   // markdown | json | <any DSL or GPL languages that can be used in a md codeblock, like yaml or python> | text
  title:      string
  border:     Border   // {background, foreground} — color pair for the border
  background: color
  foreground: color
  dimmed:     boolean
  collapsed:  boolean
  summary:    string
  spinner:    string   // may be removed later — spinner probably belongs as its own
                       // model or is already handled by Bubble Tea's spinner component.
                       // Needs investigation.
}
```

> Note: Attributes of this model can be updated later as needed.
