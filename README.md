# Ouroboros

<p align="center"><img src="ouroboros.png" alt="Ouroboros" width="200"></p>

[![CI](https://github.com/DaviMGDev/ouroboros/actions/workflows/ci.yml/badge.svg)](https://github.com/DaviMGDev/ouroboros/actions/workflows/ci.yml)

A Go-based LLM agent framework with a generic chat completion interface, tool-calling agent loop, and built-in mock implementation.

> **Version**: Run `oro --version` or see [CHANGELOG](./CHANGELOG.md).

## Quick Start (CLI)

```bash
# Build
go build -o oro ./cmd/ouroboros/

# Single prompt
./oro -p "What is 2+2?"

# Pipe from stdin
echo "Hello" | ./oro -q

# Persistent multi-turn conversation
./oro -s ./chat.json -p "My name is Davi"
./oro -s ./chat.json -p "What's my name?"

# Verbose mode (see tool calls)
./oro -v -p "List files in /tmp"
```

| Flag | Shorthand | Description |
|------|-----------|-------------|
| `--prompt` | `-p` | User prompt (reads from stdin if omitted) |
| `--model` | `-m` | Model name (default: `gemma4:31b-cloud`) |
| `--session` | `-s` | Session file for persistent conversation context |
| `--verbose` | `-v` | Show tool calls and iteration info on stderr |
| `--quiet` | `-q` | Only print final assistant response |
| `--config` | — | Config file path (default: `./ouroboros.yaml`, `~/.config/ouroboros/config.yaml`) |
| `--provider-base-url` | — | Ollama base URL (default: `http://localhost:11434`) |
| `--version` | — | Print version and exit |

## Overview

`ouroboros` defines a minimal `LLM` interface that abstracts provider-specific LLM interactions behind three methods:

- **`Chat(ctx, *ChatRequest)`** — Conversational chat with message history, model selection, and generation parameters.
- **`Complete(ctx, prompt)`** — Single-turn text completion.
- **`StreamChat(ctx, *ChatRequest)`** — Streaming chat via an iterator pattern (`ChatStream`).

The project ships with a `MockLLM` implementation that echoes back the user's input, making it easy to write unit tests and prototype agent logic without an API key. The mock also supports streaming via `MockChatStream`.

## Types

| Type | Description |
|------|-------------|
| `Message` | A single chat message with `role` (system/user/assistant/tool), `content`, and optional `ToolCalls` |
| `ChatRequest` | Input to `Chat()`: messages, model, temperature, max tokens, stop sequences, tools |
| `ChatResponse` | Output from `Chat()`: response message, model name, token usage, finish reason |
| `UsageStats` | Token counts for prompt, completion, and total |
| `FinishReason` | Why generation stopped (`stop`, `length`, `error`, `content_filter`) |
| `ChatStream` | Iterator interface (`Next()`, `Current()`, `Err()`, `Close()`) for streaming chunks |
| `ChatChunk` | One incremental delta: `Content`, `Role`, `ToolCalls`, `FinishReason`, `Usage` |
| `ToolCall` | A tool call made by the LLM (non-streaming): `ID`, `Function` (name + arguments) |
| `ToolCallDelta` | Incremental tool call fragment for streaming: `Index`, `ID`, `Function` |
| `Tool` | Interface for defining callable tools: `Name()`, `Description()`, `Schema()`, `Execute()` |
| `ToolDef` | Serializable tool definition for provider API requests: `Type`, `Function` |
| `ToolFunction` | Describes the schema of a callable function: `Name`, `Description`, `Parameters` |

## Getting Started

### Blocking Chat

```go
package main

import (
	"context"
	"fmt"

	"github.com/DaviMGDev/ouroboros/internal/llm"
)

func main() {
	mock := &llm.MockLLM{}
	req := &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "Hello, how are you?"},
		},
		Model:       "mock-model",
		Temperature: 0.7,
		MaxTokens:   100,
	}

	resp, err := mock.Chat(context.Background(), req)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Println("Response:", resp.Message.Content)
}
```



### Streaming Chat

```go
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/DaviMGDev/ouroboros/internal/llm"
)

func main() {
	mock := &llm.MockLLM{}
	req := &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "Hello from streaming!"},
		},
		Model: "mock-model",
	}

	stream, err := mock.StreamChat(context.Background(), req)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer stream.Close()

	var full strings.Builder
	for stream.Next() {
		chunk := stream.Current()
		fmt.Print(chunk.Content)
		full.WriteString(chunk.Content)
		if chunk.FinishReason != "" {
			fmt.Printf("\n[%s] tokens: %d\n", chunk.FinishReason, chunk.Usage.TotalTokens)
		}
	}
	if err := stream.Err(); err != nil {
		fmt.Println("\nStream error:", err)
		return
	}
}
```

## Agent

The [`Agent`](/internal/agent/agent.go) interface orchestrates a tool-using conversation with an LLM. It provides two methods:

- **`Run(ctx, *AgentRequest)`** — Synchronous execution with the full tool-calling loop.
- **`StreamRun(ctx, *AgentRequest)`** — Streaming execution via `AgentStream`, yielding fine-grained events.

### FunctionCallingAgent

The built-in [`FunctionCallingAgent`](/internal/agent/agent.go) implements the tool-calling loop:

1. Call the LLM with the current message history.
2. If the LLM responds with tool calls, execute all tools in parallel (using `sync.WaitGroup`).
3. Feed tool results back into the conversation history.
4. Repeat until the LLM responds with content.

```go
ag := &agent.FunctionCallingAgent{
    LLM:   llmProvider,
    Hooks: myHooks, // optional: see Hooks section
}

resp, err := ag.Run(ctx, &agent.AgentRequest{
    Messages: []llm.Message{{Role: llm.RoleUser, Content: "What time is it?"}},
    Model:    "gemma4:31b-cloud",
    Tools:    []llm.Tool{&tools.GetTimeTool{}},
})
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `LLM` | `llm.LLM` | — | The LLM provider (required) |
| `Hooks` | `[]Hook` | `nil` | Ordered hook chain for lifecycle interception |
| `ChunkDelay` | `time.Duration` | `0` | Simulated delay between stream chunks (testing) |

### AgentStream

`StreamRun` returns an `AgentStream` that yields `AgentChunk` events:

| Type | Description |
|------|-------------|
| `token` | LLM streaming token |
| `tool_call` | LLM requested a tool |
| `tool_start` | Tool execution started |
| `tool_result` | Tool returned a result |
| `iteration_start` | New iteration of the tool loop |
| `done` | Agent finished (with optional error) |

## Hooks

The [`Hook`](/internal/agent/hook.go) interface lets you intercept the agent's execution at six lifecycle points. Embed [`BaseHook`](/internal/agent/hook.go) and override only the methods you need.

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

```go
type LoggingHook struct {
    agent.BaseHook
}

func (h *LoggingHook) BeforeLLM(ctx context.Context, req *llm.ChatRequest) (*llm.ChatRequest, error) {
    log.Printf("LLM call with %d messages", len(req.Messages))
    return req, nil
}
```

## Extending

Add a new provider by creating a file under `internal/providers/` (e.g., `internal/providers/openai/openai.go`) with a struct that implements the `LLM` interface:

```go
package openai

import (
	"context"

	"github.com/DaviMGDev/ouroboros/internal/llm"
)

type OpenAILLM struct {
	apiKey string
}

func (o *OpenAILLM) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	// Real API call
}

func (o *OpenAILLM) Complete(ctx context.Context, prompt string) (string, error) {
	// Real completion call
}

func (o *OpenAILLM) StreamChat(ctx context.Context, req *llm.ChatRequest) (llm.ChatStream, error) {
	// Real streaming call
}
```

## Providers

### Ollama

The project ships with a built-in `OllamaLLM` provider that connects to a local [Ollama](https://ollama.com/) instance.

```go
package main

import (
	"context"
	"fmt"

	"github.com/DaviMGDev/ouroboros/internal/llm"
	"github.com/DaviMGDev/ouroboros/internal/providers/ollama"
)

func main() {
	o := &ollama.OllamaLLM{
		BaseURL: "http://localhost:11434", // default
	}

	resp, err := o.Chat(context.Background(), &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "Why is the sky blue?"},
		},
		Model: "llama3.2",
	})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Println("Response:", resp.Message.Content)
}
```

The `OllamaLLM` zero value is usable (defaults to `http://localhost:11434` and `http.DefaultClient`).

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `BaseURL` | `string` | `http://localhost:11434` | Ollama server URL |
| `HTTPClient` | `*http.Client` | `http.DefaultClient` | HTTP client for API calls |

## Built-in Tools

The project ships with three tools ready to use in the agent loop:

### `BashTool` (`internal/tools/bash.go`)

Executes a shell command and returns stdout+stderr. Supports context cancellation and output truncation.

```go
tools := []llm.Tool{
    &tools.BashTool{MaxOutput: 10_000},
}
```

### `ReadFileTool` (`internal/tools/file.go`)

Reads a file from disk. Paths are restricted to the current working directory to prevent traversal.

```go
tools := []llm.Tool{
    &tools.ReadFileTool{},
}
```

### `GetTimeTool` (`internal/tools/time.go`)

Returns the current date and time, optionally in a specified IANA timezone.

```go
tools := []llm.Tool{
    &tools.GetTimeTool{},
}
```

## Configuration

### Config File

Optional YAML config file. Viper searches `./ouroboros.yaml` then `~/.config/ouroboros/config.yaml`.
Use `--config <path>` to specify a custom location.

```yaml
model: gemma4:31b-cloud
provider:
  base_url: http://localhost:11434
```

### Environment Variables

All flags support `ORO_`-prefixed environment variables:

| Flag | Env Var |
|------|---------|
| `--model` / `-m` | `ORO_MODEL` |
| `--provider-base-url` | `ORO_PROVIDER_BASE_URL` |
| `--prompt` / `-p` | `ORO_PROMPT` |
| `--session` / `-s` | `ORO_SESSION` |
| `--verbose` / `-v` | `ORO_VERBOSE` |
| `--quiet` / `-q` | `ORO_QUIET` |

Precedence: **CLI flags** > **env vars** > **config file** > **defaults**.

### Session Persistence

The `--session` / `-s` flag enables persistent multi-turn conversations. Session data is stored as a human-readable JSON file:

```json
{
  "model": "gemma4:31b-cloud",
  "messages": [
    {"role": "user", "content": "My name is Davi"},
    {"role": "assistant", "content": "Nice to meet you, Davi!"}
  ]
}
```

## License

MIT — see [LICENSE](./LICENSE).
