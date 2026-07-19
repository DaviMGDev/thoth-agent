# ADR 004: Keybinding to Copy the Last Agent Response

**Status**: Proposed

**Date**: 2026-07-16

## Context

The `thoth-agent` Bubble Tea TUI (`cmd/thoth-agent/tui.go`) renders agent responses inside a styled viewport. Users frequently want to copy the last assistant response — code blocks, structured data, file paths returned by tools — either to paste elsewhere or to feed back into the conversation.

The current workflow requires users to:

1. Mouse-select the response text in the terminal.
2. Manually copy (Ctrl+Shift+C, Cmd+C, or right-click).
3. The selection must be precise — excess whitespace or omitted lines break the copy.

For long responses (multi-paragraph explanations, code snippets, tool results), this is error-prone and tedious. A dedicated keybinding or slash-command would improve UX significantly.

Existing constraints inform the solution:

- **Zero external dependencies** (stdlib only — the project's design intent).
- **ADR 003** defines a `Block`/`InlineBlock`-based TUI rendering model with a viewport that holds `[]Block`. A keybinding would integrate naturally with that model once implemented.
- **Bubble Tea's event loop** supports individual keypress detection — a `/copy` command or keybinding can be handled directly in `handleKeyMsg()` without raw terminal mode changes.

## Decision

Implement **two copy mechanisms**, each covering a different fidelity level, using only stdlib:

### 1. `/copy` Slash Command (Immediate, Independent of TUI)

Add a `/copy` command that prints the last assistant response to stdout in a copy-friendly format and, on supported platforms, writes it to the system clipboard via the platform's clipboard utility.

**Clipboard mechanism**: Shell out to the OS-native clipboard utility, detected at startup:

| Platform | Command |
|----------|---------|
| Linux (X11) | `xclip -selection clipboard` |
| Linux (Wayland) | `wl-copy` |
| macOS | `pbcopy` |
| Windows | `clip` |

Detection is a one-time probe at startup (`exec.LookPath`) — the first found tool is cached. If none is found, `/copy` falls back to printing the response with a clear delimited boundary and instructions to select+copy manually.

**Response buffer**: The REPL already maintains a `reply strings.Builder` that accumulates the assistant response. Stash the completed builder content in a `lastAssistantReply string` variable that persists across iterations.

**False-trigger guard**: If there is no assistant response yet (first turn or after `/clear`), print a message explaining that there is nothing to copy.

### 2. `copy-key.py` Helper Script (Optional Companion)

Ship an optional Python script at `scripts/copy-key.py` that users can bind to a keyboard shortcut in their terminal emulator or window manager. The script reads from stdin and writes to the clipboard (using `pyperclip` or platform commands). This is not a REPL feature — it is a user-convenience for those who want a system-level keybind.

### 3. Future: Native TUI Keybinding (When ADR 003 Primitives Are Implemented)

When the `Block`-based TUI viewport from ADR 003 is implemented, add a **Ctrl+Y** keybinding that copies the last rendered block's content via **OSC 52** (terminal clipboard escape sequence). OSC 52 requires no external dependencies — it writes a control sequence to stdout that the terminal emulator interprets as a clipboard write.

```go
// OSC 52 clipboard write — zero deps, works over SSH.
// Supported by: kitty, iTerm2, tmux, GNOME Terminal, Windows Terminal, etc.
func copyToClipboardOSC52(text string) {
    // Base64-encode the text to avoid escape sequence issues.
    encoded := base64.StdEncoding.EncodeToString([]byte(text))
    fmt.Printf("\033]52;c;%s\007", encoded)
}
```

This approach:
- Works over SSH and tmux (where desktop clipboard tools do not).
- Requires no external binary.
- Is immediately available once the TUI has raw terminal input.
- Falls back gracefully (terminals that don't support OSC 52 simply ignore the sequence).

**Keybinding choice**: **Ctrl+Y** is chosen because:
- Ctrl+Shift+C is the terminal's native copy shortcut (terminal emulators intercept it before the application).
- Ctrl+Y is rarely used in REPL contexts and is not a common paste/suspend key.
- It is easy to type with one hand.
- It matches Emacs's "yank" — for users familiar with readline bindings.

## Consequences

### Positive

- **Immediate usability** — `/copy` works today with no infrastructure changes beyond the code change itself.
- **Zero new dependencies** — Both `/copy` (shelling out) and future OSC 52 (control sequences) use only stdlib.
- **Cross-platform clipboard** — Detection logic handles Linux, macOS, and Windows gracefully.
- **Graceful degradation** — If no clipboard tool is found, `/copy` becomes a guided print-to-terminal instruction.
- **Future-proofing** — The OSC 52 approach aligns with the TUI primitives in ADR 003 and avoids re-designing the feature later.
- **Testable** — The clipboard dispatch can be tested by injecting a `ClipboardWriter` interface:

```go
type ClipboardWriter interface {
    Write(text string) error
}
```

This allows test doubles that verify the write without actually touching the system clipboard.

### Negative

- **Shelling out is not instant** — `exec.Command` for `xclip`/`pbcopy` adds ~10-50ms latency. Acceptable for an explicit `/copy` command.
- **Platform dependency** — Clipboard commands must be installed. `/copy` degrades gracefully but users on systems without them get a less seamless experience.
- **OSC 52 not universal** — Some terminal emulators (older xterm without `allowWindowOps`, some mobile SSH clients) ignore OSC 52. Mitigation: the TUI can detect support via `DA3` (Device Attributes) query and fall back to printing.

### Risks

- **`/copy` could be confused with `/clip` or `/export`** — Mitigation: `copy` is the most intuitive verb. `/clip` is accepted as an alias.
- **Response buffer not persisted** — If the user changes a `Block`'s collapsed state or the viewport scrolls, the _rendered_ response may differ from the raw `lastAssistantReply`. Mitigation: `/copy` copies the last response's canonical text (the `Content` field of the last assistant `Block`), not the rendered/truncated viewport version.
- **Security: clipboard poisoning** — A malicious LLM response could contain content that, when pasted into a shell, executes unintended commands. Mitigation: The clipboard write is the user's explicit action (they type `/copy` or press Ctrl+Y). This is not different from manually selecting and copying.
- **`exec.LookPath` at startup is a side effect** — Mitigation: probe lazily on first `/copy` invocation, cache the result.

## Rejected Alternatives

1. **Raw terminal mode with `golang.org/x/term`** — Rejected because it introduces an external dependency (`golang.org/x/term` plus potentially `golang.org/x/sys`). This is a worthy goal but belongs in the TUI push (ADR 003) where a full raw-mode terminal abstraction is justified. For the immediate copy-keybind, `/copy` provides the functionality with zero deps.

2. **Always-on clipboard write (every response auto-copied)** — Rejected because it is surprising and potentially destructive. The user should explicitly request a copy.

3. **Select/copy via ANSI selection escape sequences** — There is no widely-supported ANSI sequence for programmatic text selection (as opposed to clipboard write). OSC 52 is the only portable clipboard write mechanism.

4. **Temp file write** (`/tmp/thoth-agent-last-response.txt`) — Rejected because it requires the user to open a file and manually copy. This is better than nothing but not meaningfully better than mouse selection. Could be added as a secondary `/export` command if users request it.

5. **Clipboard via cgo** (e.g., linking `Xlib` or `CFramework`) — Rejected because it introduces platform-specific build tags and breaks cross-compilation. The pure-Go shell-out approach is simpler and more portable.

6. **Dedicated GUI clipboard history widget** — Rejected as over-engineering for the current stage of the project. A `/copy` command and future Ctrl+Y are sufficient.
