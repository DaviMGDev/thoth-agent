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
| `--provider-base-url` | — | Ollama base URL (default: `http://localhost:11434`) |
| `--version` | — | Print version and exit |

> **Note**: The TUI (interactive terminal REPL) has been moved to the `tui` branch for independent development.

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

## License

MIT — see [LICENSE](./LICENSE).
