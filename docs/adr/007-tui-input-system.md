# ADR 007: TUI Input System

**Status**: Proposed

**Date**: 2026-07-17

## Context

The TUI uses `bubbles/textinput` for user input (`cmd/my-agent/tui.go` line 55).
This has three problems identified in user feedback:

1. **No multiline support** — `textinput` is a single-line input. Long messages
   scroll horizontally rather than wrapping to a new line. This is unfamiliar and
   error-prone for users typing multi-paragraph prompts or code snippets.

2. **Input loss on history scroll** — Since `textinput` has no concept of scrollable
   history within the input field, attempting to review earlier input content (via
   hypothetical Up Arrow) loses the in-progress text.

3. **No input history** — Previously submitted messages cannot be recalled and
   re-sent. Users who want to repeat or modify an earlier prompt must retype it.

The `bubbles` package (already a dependency) provides `textarea` as a multiline
alternative to `textinput`. It supports text wrapping, vertical growth, and content
scrolling within the input area.

## Decision

### 1. Replace `textinput` with `textarea`

Switch from `bubbles/textinput` to `bubbles/textarea` for the user input field.

| Property | `textinput` (current) | `textarea` (proposed) |
|----------|----------------------|----------------------|
| Lines | Single | Multiple, wraps at width |
| Scrolling | Inline horizontal | Vertical scroll within input |
| Max height | Fixed 1 line | Configurable (`MaxHeight: 5`) |
| Content access | `.Value()` | `.Value()` (compatible) |

**Key configuration:**

```go
ta := textarea.New()
ta.Placeholder = "Type a message…"
ta.Focus()
ta.CharLimit = 4096      // same as current
ta.MaxHeight = 5         // grows up to 5 lines, then scrolls internally
ta.ShowLineNumbers = false
```

The `MaxHeight: 5` setting means:
- Short messages occupy 1-2 lines.
- The input box grows vertically up to 5 lines as the user types.
- Beyond 5 lines, the textarea scrolls its content vertically, always keeping
  the cursor at the bottom of the visible area.

### 2. Input History Buffer

Add a per-session, in-memory input history buffer.

```go
type Session struct {
    ID       int
    Name     string
    Messages []llm.Message

    // Input history for this session
    InputHistory    []string // ring buffer, newest last
    HistoryPos      int      // -1 = not browsing history, 0..len-1 = browsing
    PendingInput    string   // current input saved when user starts browsing
}
```

**History behavior:**

| Action | Effect |
|--------|--------|
| User submits a message | Appended to `InputHistory` (up to 50 entries), `HistoryPos` reset to -1, `PendingInput` cleared |
| Up Arrow (input focused) | If at new input (`HistoryPos == -1`), save current text to `PendingInput`, move to last history entry. Else move to previous entry. |
| Down Arrow (input focused) | If browsing (`HistoryPos >= 0`), move to next entry. If at the newest entry, restore `PendingInput` and reset `HistoryPos` to -1. |
| Empty history, Up pressed | No-op (no crash, no state change) |

**History capacity:** 50 entries per session. Older entries are evicted (FIFO) when
the limit is reached.

**Edit semantics:** If the user recalls an old entry and edits it before pressing
Enter, the **edited version** is submitted and appended as a new history entry.
The original entry is preserved unchanged in its original position.

### 3. Scoping

Input history is **per-session** (each session has its own history buffer). History
persists in memory for the lifetime of the TUI session. No disk persistence.

## Consequences

### Positive

- **Multiline input** — Users can type paragraphs, code, and structured prompts
  naturally with word wrapping.
- **Visible input area** — The input box grows to show up to 5 lines of context;
  beyond that, inline scrolling keeps the cursor visible.
- **Shell-like history** — Up/Down arrow navigation matches muscle memory from
  bash, readline, and REPL tools.
- **Edit-friendly** — Recalling an entry and editing it creates a new entry,
  preserving the original.
- **No data loss** — In-progress input is saved in `PendingInput` when the user
  browses history, and restored when they return.
- **Backward-compatible API** — `.Value()` and `.SetValue()` work identically
  between `textinput` and `textarea`, so the streaming reply mechanism is
  unaffected.

### Negative

- **Visual change** — The input area now occupies 5 lines at maximum instead of 1.
  This reduces the viewport area by up to 4 lines. Mitigation: the viewport
  dynamically shrinks by the input area's actual height, not a fixed amount.
- **textarea has more dependencies** — It imports additional Bubble Tea internals
  for line wrapping. All are already transitive dependencies via `bubbles`.
- **History is per-session, not global** — If a user wants to reuse a message
  across sessions, they can't. Mitigation: global history could be added later;
  per-session is simpler and avoids cross-session contamination.

### Risks

- **textarea CharLimit vs line wrapping** — If `CharLimit` is reached mid-word,
  `textarea` may behave unexpectedly. Mitigation: 4096 is a high limit; most
  messages will wrap naturally before hitting it.
- **textarea vertical scroll on focus loss** — If the user switches focus to the
  viewport or side panel (ADR 005), the textarea's scroll position should be
  preserved. Mitigation: `textarea` preserves its internal state across
  focus/blur cycles automatically.
- **History buffer memory** — 50 entries × ~1KB average = ~50KB per session.
  Acceptable.

## Rejected Alternatives

1. **Keep `textinput` (status quo)** — Rejected because single-line input is
   inadequate for the expected use case (code snippets, multi-paragraph prompts,
   structured data). The workaround (typing on one very long line) is poor UX.

2. **Custom multiline input from scratch** — Rejected because `bubbles/textarea`
   exists, is maintained by the charmbracelet team, and is already a transitive
   dependency. Building a custom widget would duplicate effort.

3. **Global history (shared across sessions)** — Rejected because it creates
   confusion about which session a recalled message belongs to. Per-session
   history is clearer.

4. **Persist history to disk** — Rejected because the project has no persistence
   layer yet. In-memory history is sufficient for a TUI session. Disk persistence
   should follow the future session persistence feature (see ROADMAP.md).

5. **Ctrl+P/Ctrl+N for history** — Rejected in favor of Up/Down arrows, which
   match readline and shell conventions. Ctrl+P/Ctrl+N are less discoverable.
