# ADR 003: TUI Rendering Primitives — Block & InlineBlock Models

**Status**: Accepted

**Date**: 2026-07-17

**Supersedes**: Initial draft of 2026-07-14 (replaced; see Revision Note below)

## Revision Note

This ADR replaces the initial proposal of 2026-07-14. The original proposed a single
`Block` struct with flat fields. Hands-on experience with the Bubble Tea TUI and user
feedback revealed the need for two distinct primitives — a multi-line `Block` with a
typed `Border` sub-struct and a single-line `InlineBlock` for compact UI elements. The
decision to accept this refined model was recorded on 2026-07-17.

## Context

The agent's Bubble Tea TUI (`cmd/my-agent/tui.go`) renders a vertical stream of events
from the agent loop (`internal/agent/agent.go`). These events fall into distinct visual
categories:

- User input (`llm.RoleUser`)
- Assistant text, final and streaming (`llm.RoleAssistant` + `AgentEventToken`)
- Tool call headers (`AgentEventToolCall`)
- Tool execution pending/start (`AgentEventToolStart`)
- Tool results — success and error (`AgentEventToolResult`)
- System messages (`llm.RoleSystem`)
- Iteration banners (`AgentEventIterationStart`)
- Done/final (`AgentEventDone`)
- Fatal errors, token usage, finish reason, connection status
- Status indicators (thinking spinner, connected/disconnected)

A naive approach would create a distinct component for each category. That leads to 10+
component types with duplicated border logic, color application, and width handling.

The observation is that every one of these is fundamentally **a block of content with
optional border, color, title, and state** — only the configuration differs. Additionally,
some elements (tool call headers, iteration markers, status lines) are intrinsically
single-line and benefit from compact powerline-style glyph borders rather than full
box-drawing frames.

## Decision

**Two primitives**: a multi-line `Block` for chat messages and content, and a
single-line `InlineBlock` for compact UI elements. Together they cover all visual
categories with a single rendering code path per primitive.

### The `Border` Type

```go
// Border defines the visual frame around a Block.
type Border struct {
    // Type controls the border style.
    // Supported values: "none", "round", "square".
    // By default, only single-line borders are supported. Custom characters
    // may be added in the future.
    Type string

    // Background is the fill color for the border area.
    // Empty string means transparent.
    Background string

    // Foreground is the color of the border lines and title text.
    // Empty string means terminal default.
    Foreground string
}
```

### The `Block` Type

```go
// Block is a multi-line rendering primitive for the agent TUI.
// Suitable for chat messages, code blocks, JSON responses, and any
// content that occupies multiple terminal lines.
type Block struct {
    // Content is the body text to display.
    Content string

    // Type controls how Content is interpreted and rendered.
    // Supported values: "markdown", "json", "yaml", "python",
    // "go", "text", or any other language identifier that can
    // appear in a Markdown code fence. "text" disables syntax
    // highlighting; anything else implies syntax-highlighted
    // code when inside a code block or inline context.
    Type string

    // Title appears in the top border line when Border.Type != "none".
    // When Border.Type == "none", it renders as a dimmed inline prefix
    // above Content.
    Title string

    // Border defines the visual frame. Zero value means no border.
    Border Border

    // Background is the background fill color for the entire block
    // interior. Empty string means transparent.
    Background string

    // Foreground is the default text color for Content.
    // Empty string means terminal default.
    Foreground string

    // Dimmed, when true, overrides Foreground with the theme's muted
    // or dim color. Ensures consistent dimming across theme changes
    // rather than baking in a specific hex value.
    Dimmed bool

    // Collapsed, when true, renders only the Summary line instead of
    // the full Content. Toggled by user interaction (enter/space).
    Collapsed bool

    // Summary is the label shown when Collapsed is true.
    // Example: "System prompt (301 chars) [enter to expand]".
    Summary string

    // Spinner indicates an animated state within the block.
    // Supported values: "" (none), "active" (spinning), "done" (checkmark).
    // When "active", the renderer draws a spinner character at the start
    // of the content area. When "done", it draws ✓.
    // May be removed in the future — Bubble Tea's spinner component may
    // handle this concern more cleanly. Needs investigation.
    Spinner string
}
```

### The `InlineBlock` Type

```go
// InlineBlock is a single-line rendering primitive with powerline-style
// glyph borders (no vertical border lines — only top/bottom glyphs).
// Suitable for tool call headers, system banners, iteration markers,
// status lines, and other compact UI elements.
type InlineBlock struct {
    // Content is the single line of text to display.
    Content string

    // Border controls the powerline glyph style.
    // Supported values: "none", "square", "pointed", "slanted", "round".
    // All use powerline glyphs with no vertical borders.
    Border string

    // Padding is (horizontal, vertical). Vertical padding within a
    // single-line block is minimal (0-1).
    Padding [2]int // [horizontal, vertical]

    // Background fill color. Empty string means transparent.
    Background string

    // Foreground text color. Empty string means terminal default.
    Foreground string

    // Dimmed applies the theme's muted color.
    Dimmed bool

    // Wrap controls whether long content wraps (true) or truncates (false).
    Wrap bool

    // Weight controls sizing behavior:
    //   "none" — use padding normally
    //   "fill" — fill the entire parent width, ignores padding
    //   "wrap" — size respects content size, ignores padding
    Weight string

    // Position controls horizontal alignment:
    //   "none"   — respects padding
    //   "left"   — aligns content to the left, ignores padding
    //   "right"  — aligns content to the right, ignores padding
    //   "center" — centers content, ignores padding
    Position string
}
```

### Pre-configured Presets

Every message type maps to a `Block` or `InlineBlock` with specific property values.
These are constructed via factory functions, not sub-types:

| Message | Primitive | Border | Type | Key Properties |
|---------|-----------|--------|------|----------------|
| User input | `Block` | `none` | `text` | `Foreground: accent` |
| Assistant text | `Block` | `none` | `markdown` | — |
| Tool call+result | `Block` | `round` | `text` or `json` | `Border.Foreground` transitions on state |
| System message | `Block` | `none` | `text` | `Dimmed: true`, `Collapsed: true` |
| Error banner | `Block` | `square` | `text` | `Background: red`, `Foreground: white` |
| Thinking state | `Block` | `none` | `text` | `Spinner: "active"`, `Dimmed: true`, `Title: "Thinking..."` |
| Iteration header | `InlineBlock` | `pointed` | — | `Dimmed: true`, `Weight: "fill"` |
| Tool call header | `InlineBlock` | `square` | — | `Foreground: tool-color`, `Weight: "fill"` |
| Status bar | `InlineBlock` | `slanted` | — | `Position: "right"` |

### State Transitions via Mutation

A single `Block` mutates through states rather than being replaced by a different
component:

```
Tool call lifecycle:
  → Block{Spinner:"active", Border{Foreground:"muted"}, Title:"read_file(path=...)"}
  → Block{Spinner:"done",   Border{Foreground:"green"}, Title:"read_file(path=...)"}
  → Block{Spinner:"",       Border{Foreground:"green"}, Title:"read_file(path=...)"}

Streaming text lifecycle:
  → Block{Spinner:"active", Dimmed:true, Title:"Thinking..."}
  → Block{Spinner:"done",   Content:"It", ...}             // first token arrives
  → Block{Spinner:"",       Content:"It is 2:30 PM", ...}  // stream complete
```

### Renderer Interface

```go
// BlockRenderer converts a Block into styled terminal output at a given width.
type BlockRenderer interface {
    // Render returns the ANSI-styled string for the block.
    // The returned string must not exceed width characters per line.
    Render(block *Block, width int) string
}

// InlineBlockRenderer converts an InlineBlock into styled terminal output.
type InlineBlockRenderer interface {
    // Render returns a single ANSI-styled line for the block.
    Render(block *InlineBlock, width int) string
}
```

A single implementation per interface handles all configurations. Border drawing,
content wrapping (plain or syntax-aware), color application, and spinner animation
are orthogonal concerns applied within `Render`.

## Consequences

### Positive

- **One rendering code path per primitive** — No duplicated border logic, color
  application, or width handling across visual categories.
- **Additive** — New visual variants require only a new factory function, not a new
  component class or renderer dispatch branch.
- **State mutations are simple assignments** — Tool lifecycle (pending→success→error)
  and streaming (thinking→receiving→done) are property changes, not type swaps.
- **Serializable** — `[]Block` and `[]InlineBlock` slices can be serialized to JSON
  for session persistence or debugging snapshots.
- **Testable** — Renderer methods return plain strings. Golden-file tests compare
  output directly.
- **Bubble Tea compatible** — A `tea.Model` holds slices of primitives in a viewport.
  `Update` handles key events (expand/collapse, scroll). `View` iterates through
  renderers.
- **Single-line vs multi-line separation** — Compact UI elements (tool call headers,
  iteration markers) don't waste vertical space with full box-drawing frames.

### Negative

- **No type-safe differentiation** — The compiler cannot prevent a "user input" block
  from being accidentally given a border. Mitigation: factory functions enforce
  conventions; direct struct construction is for edge cases.
- **All-or-nothing properties** — Every block carries all fields, even when irrelevant
  (e.g., `Border` on a plain text block). Mitigation: zero values are no-ops; the
  structs are small (~10 fields each).
- **Two renderers instead of one** — Slight duplication between `BlockRenderer` and
  `InlineBlockRenderer`. Mitigation: shared utility functions for color application,
  content wrapping, and truncation.
- **No per-block interactive behavior** — Collapse/expand and scrolling are handled by
  the viewport model, not by individual blocks. Mitigation: this is by design — blocks
  are data, the viewport is the interactor.

### Risks

- **Property explosion** — Future requirements (padding, alignment, max-height, inline
  images) could bloat the structs. Mitigation: add properties only when needed; group
  related properties into sub-structs if top-level field count exceeds ~15.
- **Markdown rendering complexity** — `Type: "markdown"` implies the renderer must
  wrap markdown-aware (don't break inside code fences, handle headings, etc.).
  Mitigation: start with `Type: "text"` only; add markdown as a separate iteration
  after the primitives are stable.
- **Powerline glyphs not universally available** — `InlineBlock` depends on a
  patched font. Mitigation: fall back to ASCII alternatives (arrows, dashes) when
  powerline glyphs are not available; detect via terminal capabilities query.

## Rejected Alternatives

1. **Single flat `Block` struct with string fields for border/style** (initial proposal
   of 2026-07-14) — Replaced because the flat model conflated border type with border
   color and lacked a separation between multi-line and single-line primitives. The
   `Border` sub-struct and separate `InlineBlock` are cleaner.

2. **One component type per message category** — Rejected because it creates ~10
   component implementations with duplicated border, color, and width logic. Adding
   a new variant (e.g., "info panel") requires a new component.

3. **Inheritance / interface hierarchy** (`MessageWidget`, `ToolWidget`, `ErrorWidget`)
   — Rejected because Go lacks classical inheritance, and interface-based dispatch
   adds indirection without benefit when the rendering operation is the same for all
   variants.

4. **No primitives — raw ANSI strings in the agent loop** — Rejected because it
   couples rendering to the agent core, prevents theming, breaks on resize, and
   duplicates formatting code.

5. **Separate "container" and "leaf" primitives** — Rejected because nesting is
   unnecessary for a linear chat scrollback. Future needs (side panels, dialogs) may
   require composition, but that can be added as a viewport-level concern without
   changing the primitive models.
