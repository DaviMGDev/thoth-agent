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

> Tests are in `llm_test.go`. The `MockLLM` implementation in `llm.go` echoes the user's input and is designed to simplify unit testing of code that depends on the `LLM` interface.

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

- **Pattern**: Interface-based design — the `LLM` interface abstracts provider-specific implementations
- **Structure**: Flat package (`package main`) — suitable for early-stage prototyping
- **Key files**:
  - `llm.go` — `LLM` interface definition, request/response types, message roles, usage stats, streaming types (`ChatStream`, `ChatChunk`, `ToolCallDelta`), `MockLLM` implementation, and `MockChatStream` implementation
  - `ollama.go` — `OllamaLLM` provider implementation for local Ollama instances
  - `main.go` — entry point demonstrating usage of the mock implementation

## Dependencies

- **Current state**: Zero external dependencies (stdlib only: `context`, `fmt`)
- **Design intent**: Providers can be added as new types implementing the `LLM` interface, keeping the core lightweight

## Notes for AI Agents

- This is an early-stage project with a clean, minimal surface. The `LLM` interface is the primary abstraction point — any provider (OpenAI, Anthropic, Ollama, etc.) should implement `Chat(ctx, *ChatRequest)`, `Complete(ctx, prompt)`, and `StreamChat(ctx, *ChatRequest)`.
- The `FinishReason` enum and `UsageStats` struct align with common LLM API patterns, making integration straightforward.
- **Streaming**: The `StreamChat` method uses the iterator pattern (`ChatStream` with `Next()`, `Current()`, `Err()`, `Close()`), matching the approach used by the OpenAI Go SDK. The caller **must** call `Close()` when done. The `ChatChunk` type carries incremental `Content`, `ToolCalls` (for tool call streaming), `FinishReason`, and `Usage`.
- **Tool call streaming**: `ToolCallDelta` supports incremental tool call fragments by index. Providers should emit deltas with `Index` to identify which tool call the fragment belongs to, and the agent should accumulate partial JSON arguments.
- When adding a new provider, add a new file (e.g., `openai.go`, `anthropic.go`) with a struct that implements the `LLM` interface.
- The project includes a built-in `OllamaLLM` provider (`ollama.go`) that connects to Ollama's `/api/chat` endpoint. It uses only stdlib (`net/http`, `encoding/json`, `bufio`). The zero value is usable with sensible defaults. Tests use `httptest.NewServer` to mock Ollama without requiring a running instance.
