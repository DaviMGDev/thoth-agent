# Ouroboros

> A Go-based LLM agent framework defining a generic chat completion interface with typed requests/responses and a mock implementation for testing.

## Build & Development

- **Run the CLI**: `go run ./cmd/ouroboros/ -- -p "Hello"` (or build first: `go build -o oro ./cmd/ouroboros/ && ./oro -p "Hello"`)
- **Build binary**: `go build -o oro ./cmd/ouroboros/`
- **Install dependencies**: `make deps` (or `go mod tidy && go mod verify`)
- **Lint**: `make lint` (or `go vet ./internal/... ./cmd/...`)
- **Format**: `make fmt` (or `gofmt -s -w ./internal/ ./cmd/`)

## Testing

- **Run all tests**: `go test ./internal/... ./cmd/...`
- **Run tests verbosely**: `go test -v ./internal/...`
- **Run with coverage**: `go test -coverprofile=coverage.out ./internal/... ./cmd/...`

> Tests live alongside their packages: `internal/llm/llm_test.go`, `internal/providers/ollama/ollama_test.go`, `internal/agent/agent_test.go`, `internal/agent/hook_test.go`, `internal/tools/bash_test.go`, and `cmd/ouroboros/viper_test.go`. The `MockLLM` implementation in `internal/llm/` echoes the user's input and is designed to simplify unit testing of code that depends on the `LLM` interface.

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
ouroboros/
├── internal/
│   ├── llm/                  # LLM interface + shared types + MockLLM
│   ├── agent/                # Agent interface + FunctionCallingAgent + hooks + MockAgent
│   ├── tools/                # Built-in tools (bash, file, time)
│   └── providers/
│       └── ollama/           # OllamaLLM provider implementation
├── cmd/
│   └── ouroboros/          # main() — CLI entry point
├── docs/
│   ├── adr/                  # Architecture Decision Records
│   ├── discussions/          # Architecture discussions (e.g., 001-rename)
│   └── plans/                # Implementation plans (e.g., 001-hooks)
├── .github/
│   └── workflows/ci.yml      # GitHub Actions CI
├── .golangci.yml             # Linter configuration
├── AGENTS.md                 # This file — agent project instructions
├── CHANGELOG.md              # Version history
├── LICENSE                   # MIT license
├── Makefile                  # Build/test/lint/format targets
├── README.md                 # Project overview
└── ROADMAP.md                # Planned features
```

- **Key files**:
  - `internal/llm/llm.go` — `LLM` interface, shared types, `Tool` interface, `MockLLM`, `MockChatStream`
  - `internal/providers/ollama/ollama.go` — `OllamaLLM` provider (stdlib only, connects to Ollama's `/api/chat`)
  - `internal/agent/agent.go` — `Agent` interface, `FunctionCallingAgent` (tool-calling loop, parallel execution), `AgentStream`, `MockAgent`, `MockTool`
  - `internal/agent/hook.go` — `Hook` interface and `BaseHook` for intercepting agent lifecycle events
  - `internal/agent/hook_chain.go` — Hook chaining helpers (`applyBefore*` / `applyAfter*`)
  - `internal/tools/bash.go` — `BashTool` for executing shell commands
  - `internal/tools/file.go` — `ReadFileTool` for reading files
  - `internal/tools/time.go` — `GetTimeTool` for current date/time
  - `cmd/ouroboros/main.go` — CLI entry point (`main()` function)
  - `cmd/ouroboros/root.go` — Cobra root command (flags, Viper config, agent execution pipeline)

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

## CLI

The CLI is built with [Cobra](https://github.com/spf13/cobra) and [Viper](https://github.com/spf13/viper).

### Configuration

Optional config file (YAML). Viper searches `./ouroboros.yaml` then `~/.config/ouroboros/config.yaml`.
Use `--config <path>` to specify a custom location.

```yaml
# ouroboros.yaml
model: gemma4:31b-cloud
provider:
  base_url: http://localhost:11434
```

### Environment Variables

All flags support env var overrides with the `ORO_` prefix:

| Flag | Env Var |
|------|---------|
| `--model` / `-m` | `ORO_MODEL` |
| `--provider-base-url` | `ORO_PROVIDER_BASE_URL` |
| `--prompt` / `-p` | `ORO_PROMPT` |
| `--session` / `-s` | `ORO_SESSION` |
| `--verbose` / `-v` | `ORO_VERBOSE` |
| `--quiet` / `-q` | `ORO_QUIET` |
| `--config` | — |
| `--version` | — |

Precedence: **CLI flags** > **env vars** > **config file** > **defaults**.

### Shell Completions

```bash
source <(oro completion bash)   # bash
source <(oro completion zsh)    # zsh
oro completion fish | source    # fish
```

## Dependencies

- **Runtime**: [Cobra](https://github.com/spf13/cobra) (CLI framework), [Viper](https://github.com/spf13/viper) (config/env management)
- **Core `internal/` packages**: zero direct dependencies (stdlib only)
- **Design intent**: Providers can be added as new types implementing the `LLM` interface, keeping the core framework lightweight

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
| `AgentStream` | Streaming iterator (`Next()`, `Current()`, `Err()`, `Close()`) for agent execution events |
| `AgentChunk` | One event from streaming agent execution: `Type`, `Content`, `ToolCall`, `ToolResult`, `Usage` |

## Messages & Roles

- `RoleSystem` — system-level instruction
- `RoleUser` — end-user message
- `RoleAssistant` — AI assistant message (may carry `ToolCalls`)
- `RoleTool` — tool execution result

## Streaming

- `ChatStream` — iterator pattern (`Next()`, `Current()`, `Err()`, `Close()`) matching OpenAI Go SDK. The caller **must** call `Close()` when done.
- `ChatChunk` — carries incremental `Content`, `ToolCalls` (as `ToolCallDelta`), `FinishReason`, and `Usage`.
- `AgentStream` — same iterator pattern as `ChatStream`, yields `AgentChunk` events (`token`, `tool_call`, `tool_start`, `tool_result`, `iteration_start`, `done`).

## Tool Calling

- Define tools by implementing the `Tool` interface (`Name()`, `Description()`, `Schema()`, `Execute()`).
- Pass tools via `ChatRequest.Tools` — providers serialize them automatically via `ToolDefs()`.
- The LLM can respond with tool calls in `Message.ToolCalls` (non-streaming) or `ChatChunk.ToolCalls` (streaming).
- `FunctionCallingAgent` orchestrates the full loop: call LLM → if tool calls → execute tools in parallel (`sync.WaitGroup`) → feed results back → repeat until the LLM responds with content.

## Hooks

The [`Hook`](/internal/agent/hook.go) interface lets you intercept agent execution at six lifecycle points. Embed [`BaseHook`](/internal/agent/hook.go) and override only the methods you need.

| # | Hook Method | When It Fires |
|---|-------------|---------------|
| P1 | `BeforeAgent` | Once when `Run`/`StreamRun` starts |
| P2 | `BeforeLLM` | Before each LLM call inside the iteration loop |
| P3 | `AfterLLM` | After each LLM call returns |
| P4 | `BeforeTool` | Before each `tool.Execute()` call |
| P5 | `AfterTool` | After each `tool.Execute()` completes |
| P6 | `AfterAgent` | On every exit path (success, error, cancel, max-iterations) |

- **Before** methods fire in forward order (hook[0] → hook[1] → ...).
- **After** methods fire in reverse order (... → hook[1] → hook[0]).
- Returning an error from any hook aborts the agent.

The `FunctionCallingAgent.Hooks` field accepts `nil` or empty (no overhead).

## Built-in Tools

Three tools are registered by default in the CLI and ready to use:

### `BashTool` (`internal/tools/bash.go`)

Executes a shell command and returns stdout+stderr. Context-aware; supports output truncation (default 10,000 bytes).

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `MaxOutput` | `int` | `10000` | Max combined output bytes (0 = use default) |

### `ReadFileTool` (`internal/tools/file.go`)

Reads a file from disk. Paths are restricted to the current working directory to prevent directory traversal.

### `GetTimeTool` (`internal/tools/time.go`)

Returns the current date and time, optionally in a specified IANA timezone (e.g. `America/Sao_Paulo`, `Europe/London`, `UTC`).

## Key Files

- `internal/llm/llm.go` — `LLM` interface, all shared types, `Tool` interface, `MockLLM`, `MockChatStream`
- `internal/providers/ollama/ollama.go` — `OllamaLLM` provider (stdlib only, connects to Ollama's `/api/chat`)
- `internal/agent/agent.go` — `Agent` interface, `FunctionCallingAgent`, `AgentStream`, `MockAgent`, `MockTool`
- `internal/agent/hook.go` — `Hook` interface and `BaseHook` for intercepting agent lifecycle events
- `internal/agent/hook_chain.go` — Hook chaining helpers (`applyBefore*` / `applyAfter*`)
- `internal/tools/bash.go` — `BashTool` for executing shell commands
- `internal/tools/file.go` — `ReadFileTool` for reading files
- `internal/tools/time.go` — `GetTimeTool` for current date/time
- `cmd/ouroboros/main.go` — CLI entry point (`main()` function)
- `cmd/ouroboros/root.go` — Cobra root command (flags, Viper config, agent execution pipeline, session persistence)

## Providers

Add a new provider by creating a file under `internal/providers/` (e.g., `internal/providers/openai/openai.go`) with a struct that implements the `LLM` interface:

```go
package openai

import "github.com/DaviMGDev/ouroboros/internal/llm"

type OpenAILLM struct {
	apiKey string
}

func (o *OpenAILLM) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) { ... }
func (o *OpenAILLM) Complete(ctx context.Context, prompt string) (string, error) { ... }
func (o *OpenAILLM) StreamChat(ctx context.Context, req *llm.ChatRequest) (llm.ChatStream, error) { ... }
```

The built-in `OllamaLLM` uses only stdlib (`net/http`, `encoding/json`, `bufio`). The zero value is usable (defaults to `http://localhost:11434` and `http.DefaultClient`). Tests use `httptest.NewServer` to mock Ollama without a running instance.
