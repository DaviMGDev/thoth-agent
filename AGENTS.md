# Thoth Agent

> A Go-based LLM agent framework defining a generic chat completion interface with typed requests/responses and a mock implementation for testing.

## Build & Development

- **Run the app**: `go run ./cmd/thoth-agent/`
- **Build binary**: `go build -o thoth-agent ./cmd/thoth-agent/`
- **Install dependencies**: `go mod tidy`
- **Lint**: `go vet ./internal/... ./cmd/...`
- **Format**: `gofmt -s -w ./internal/ ./cmd/`

## Testing

- **Run all tests**: `go test ./internal/... ./cmd/...`
- **Run tests verbosely**: `go test -v ./internal/...`
- **Run with coverage**: `go test -coverprofile=coverage.out ./internal/... ./cmd/...`

> Tests live alongside their packages: `internal/llm/llm_test.go`, `internal/providers/ollama/ollama_test.go`, and `internal/agent/agent_test.go`. The `MockLLM` implementation in `internal/llm/` echoes the user's input and is designed to simplify unit testing of code that depends on the `LLM` interface.

## Code Style

- **Language**: Go 1.24
- **Formatted with**: `gofmt` (standard Go tooling)
- **Linted with**: `go vet` (plus `golangci-lint` when installed — see `.golangci.yml`)
- **Naming conventions**:
  - Exported types and functions: `PascalCase` (e.g., `ChatRequest`, `MockLLM`)
  - Unexported: `camelCase`
  - Constants: `PascalCase` with descriptive names (e.g., `RoleSystem`, `FinishReasonStop`)
- **JSON tags**: Used on all serializable struct fields (e.g., `json:"role"`)
- **Error handling**: idiomatic Go — functions return errors as last return value
- **Interface design**: Small, focused interfaces (`LLM` with `Chat`, `Complete`, and `StreamChat` methods)

## Architecture

- **Pattern**: Interface-based design — the `LLM` interface abstracts provider-specific implementations; the `Agent` interface orchestrates tool-using conversations
- **Structure**: Module layout with `internal/` subpackages for encapsulation:

```
thoth-agent/
├── internal/
│   ├── llm/                  # LLM interface + shared types + MockLLM
│   ├── agent/                # Agent interface + FunctionCallingAgent + MockAgent + MockTool
│   └── providers/
│       └── ollama/           # OllamaLLM provider implementation
├── cmd/
│   └── thoth-agent/             # main() — REPL entry point
├── docs/
│   └── adr/                  # Architecture Decision Records
├── .golangci.yml             # Linter configuration
├── Makefile                  # Build/test/lint/format targets
└── .github/workflows/ci.yml  # GitHub Actions CI
```

- **Key files**:
  - `internal/llm/llm.go` — `LLM` interface definition, shared types (`Message`, `ChatRequest`, `ChatResponse`, `UsageStats`, `FinishReason`, `ChatStream`, `ChatChunk`, `ToolCallDelta`, `ToolCall`, `ToolDef`, `ToolFunction`, `Tool` interface), `MockLLM` implementation, and `MockChatStream` implementation
  - `internal/providers/ollama/ollama.go` — `OllamaLLM` provider implementation for local Ollama instances
  - `internal/agent/agent.go` — `Agent` interface, `FunctionCallingAgent` (concrete agent with tool-calling loop and parallel tool execution), `AgentStream`, `MockAgent`, `MockTool`
  - `cmd/thoth-agent/main.go` — entry point (REPL interactive chat with Ollama)

## Architecture Decision Records

Record significant architectural and structural decisions in `docs/adr/` as
Markdown files following the convention established in
`docs/adr/001-flat-to-module-layout.md`.

### When to Write

Write an ADR when a decision has lasting impact on the project — module
layout, dependency choices, interface design, provider architecture, test
strategy, or any structurally significant change.

### Naming Convention

```
docs/adr/NNN-title-with-dashes.md
```

- `NNN` is the next available zero-padded sequence number (e.g., `002`).
- Title uses kebab-case, matching the decision summary.

### Template

```markdown
# ADR NNN: Short Title in Title Case

**Status**: [Proposed | Accepted | Deprecated | Superseded]

**Date**: YYYY-MM-DD

## Context

What prompted the decision? What constraints were at play? What alternatives
were considered? Describe the problem space.

## Decision

What was decided? Be specific about the chosen approach, its shape, and
its boundaries.

## Consequences

### Positive

- Benefit 1
- Benefit 2

### Negative

- Trade-off 1
- Trade-off 2

### Risks

- Risk 1 (mitigation: ...)
- Risk 2 (mitigation: ...)

## Rejected Alternatives

1. **Alternative A** — Why it was not chosen.
2. **Alternative B** — Why it was not chosen.
```

## Dependencies

- **Current state**: Zero external dependencies (stdlib only: `context`, `fmt`)
- **Design intent**: Providers can be added as new types implementing the `LLM` interface, keeping the core lightweight

## Shared Types

| Type | Description |
|------|-------------|
| `Message` | Chat message with `Role`, `Content`, and `ToolCalls` (for assistant tool-call messages) |
| `ChatRequest` | Input to `Chat()`: messages, model, temperature, max tokens, stop sequences, tools |
| `ChatResponse` | Output from `Chat()`: response message, model, token usage, finish reason |
| `UsageStats` | Token counts for prompt, completion, and total |
| `FinishReason` | Why generation stopped (`stop`, `length`, `error`, `content_filter`) |
| `Tool` | Interface for defining callable tools: `Name()`, `Description()`, `Schema()`, `Execute()` |
| `ToolCall` | A tool call made by the LLM (non-streaming) |
| `ToolCallDelta` | Incremental tool call fragment for streaming |
| `ToolDef` / `ToolFunction` | Serializable tool definition for provider API requests |

## Messages & Roles

- `RoleSystem` — system-level instruction
- `RoleUser` — end-user message
- `RoleAssistant` — AI assistant message (may carry `ToolCalls`)
- `RoleTool` — tool execution result

## Streaming

- `ChatStream` — iterator pattern (`Next()`, `Current()`, `Err()`, `Close()`) matching OpenAI Go SDK. The caller **must** call `Close()` when done.
- `ChatChunk` — carries incremental `Content`, `ToolCalls` (as `ToolCallDelta`), `FinishReason`, and `Usage`.
- `AgentStream` — same iterator pattern as `ChatStream`, yields `AgentChunk` events (`token`, `tool_call`, `tool_result`, `done`).

## Tool Calling

- Define tools by implementing the `Tool` interface (`Name()`, `Description()`, `Schema()`, `Execute()`).
- Pass tools via `ChatRequest.Tools` — providers serialize them automatically via `ToolDefs()`.
- The LLM can respond with tool calls in `Message.ToolCalls` (non-streaming) or `ChatChunk.ToolCalls` (streaming).
- `FunctionCallingAgent` orchestrates the full loop: call LLM → if tool calls → execute tools in parallel (`sync.WaitGroup`) → feed results back → repeat until the LLM responds with content.

## Key Files

- `internal/llm/llm.go` — `LLM` interface, all shared types, `Tool` interface, `MockLLM`, `MockChatStream`
- `internal/providers/ollama/ollama.go` — `OllamaLLM` provider (stdlib only, connects to Ollama's `/api/chat`)
- `internal/agent/agent.go` — `Agent` interface, `FunctionCallingAgent`, `AgentStream`, `MockAgent`, `MockTool`
- `cmd/thoth-agent/main.go` — REPL entry point (interactive Ollama chat)

## Providers

Add a new provider by creating a file under `internal/providers/` (e.g., `internal/providers/openai/openai.go`) with a struct that implements the `LLM` interface:

```go
package openai

import "github.com/DaviMGDev/thoth-agent/internal/llm"

type OpenAILLM struct {
	apiKey string
}

func (o *OpenAILLM) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) { ... }
func (o *OpenAILLM) Complete(ctx context.Context, prompt string) (string, error) { ... }
func (o *OpenAILLM) StreamChat(ctx context.Context, req *llm.ChatRequest) (llm.ChatStream, error) { ... }
```

The built-in `OllamaLLM` uses only stdlib (`net/http`, `encoding/json`, `bufio`). The zero value is usable (defaults to `http://localhost:11434` and `http.DefaultClient`). Tests use `httptest.NewServer` to mock Ollama without a running instance.
