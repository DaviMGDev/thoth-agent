# my-agent

> A Go-based LLM agent framework defining a generic chat completion interface with typed requests/responses and a mock implementation for testing.

## Build & Development

- **Run the app**: `go run .`
- **Build binary**: `go build -o my-agent .`
- **Install dependencies**: `go mod tidy`
- **Lint**: `go vet ./...`
- **Format**: `gofmt -s -w .`

## Testing

- **Run all tests**: `go test ./...`
- **Run tests verbosely**: `go test -v ./...`
- **Run tests with coverage**: `go test -cover ./...`

> Tests are in `llm_test.go`, `ollama_test.go`, and `agent_test.go`. The `MockLLM` implementation in `llm.go` echoes the user's input and is designed to simplify unit testing of code that depends on the `LLM` interface.

## Code Style

- **Language**: Go 1.24
- **Formatted with**: `gofmt` (standard Go tooling)
- **Linted with**: `go vet`
- **Naming conventions**:
  - Exported types and functions: `PascalCase` (e.g., `ChatRequest`, `MockLLM`)
  - Unexported: `camelCase`
  - Constants: `PascalCase` with descriptive names (e.g., `RoleSystem`, `FinishReasonStop`)
- **JSON tags**: Used on all serializable struct fields (e.g., `json:"role"`)
- **Error handling**: idiomatic Go — functions return errors as last return value
- **Interface design**: Small, focused interfaces (`LLM` with `Chat` and `Complete` methods)

## Architecture

- **Pattern**: Interface-based design — the `LLM` interface abstracts provider-specific implementations; the `Agent` interface orchestrates tool-using conversations
- **Structure**: Flat package (`package main`) — suitable for early-stage prototyping
- **Key files**:
  - `llm.go` — `LLM` interface definition, shared types (`Message`, `ChatRequest`, `ChatResponse`, `UsageStats`, `FinishReason`, `ChatStream`, `ChatChunk`, `ToolCallDelta`, `ToolCall`, `ToolDef`, `ToolFunction`, `Tool` interface), `MockLLM` implementation, and `MockChatStream` implementation
  - `ollama.go` — `OllamaLLM` provider implementation for local Ollama instances
  - `agent.go` — `Agent` interface, `FunctionCallingAgent` (concrete agent with tool-calling loop and parallel tool execution), `AgentStream`, `MockAgent`, `MockTool`
  - `main.go` — entry point (REPL interactive chat with Ollama)

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

- `llm.go` — `LLM` interface, all shared types, `Tool` interface, `MockLLM`, `MockChatStream`
- `ollama.go` — `OllamaLLM` provider (stdlib only, connects to Ollama's `/api/chat`)
- `agent.go` — `Agent` interface, `FunctionCallingAgent`, `AgentStream`, `MockAgent`, `MockTool`
- `main.go` — REPL entry point (interactive Ollama chat)

## Providers

Add a new provider by creating a file (e.g., `openai.go`, `anthropic.go`) with a struct that implements the `LLM` interface:

```go
type OpenAILLM struct {
	apiKey string
}

func (o *OpenAILLM) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) { ... }
func (o *OpenAILLM) Complete(ctx context.Context, prompt string) (string, error) { ... }
func (o *OpenAILLM) StreamChat(ctx context.Context, req *ChatRequest) (ChatStream, error) { ... }
```

The built-in `OllamaLLM` uses only stdlib (`net/http`, `encoding/json`, `bufio`). The zero value is usable (defaults to `http://localhost:11434` and `http.DefaultClient`). Tests use `httptest.NewServer` to mock Ollama without a running instance.
