# my-agent

A Go-based LLM agent framework with a generic chat completion interface and built-in mock implementation.

## Overview

`my-agent` defines a minimal `LLM` interface that abstracts provider-specific LLM interactions behind three methods:

- **`Chat(ctx, *ChatRequest)`** — Conversational chat with message history, model selection, and generation parameters.
- **`Complete(ctx, prompt)`** — Single-turn text completion.
- **`StreamChat(ctx, *ChatRequest)`** — Streaming chat via an iterator pattern (`ChatStream`).

The project ships with a `MockLLM` implementation that echoes back the user's input, making it easy to write unit tests and prototype agent logic without an API key. The mock also supports streaming via `MockChatStream`.

## Types

| Type | Description |
|------|-------------|
| `Message` | A single chat message with `role` (system/user/assistant) and `content` |
| `ChatRequest` | Input to `Chat()`: messages, model, temperature, max tokens, stop sequences |
| `ChatResponse` | Output from `Chat()`: response message, model name, token usage, finish reason |
| `UsageStats` | Token counts for prompt, completion, and total |
| `FinishReason` | Why generation stopped (`stop`, `length`, `error`, `content_filter`) |
| `ChatStream` | Iterator interface (`Next()`, `Current()`, `Err()`, `Close()`) for streaming chunks |
| `ChatChunk` | One incremental delta: `Content`, `Role`, `ToolCalls`, `FinishReason`, `Usage` |
| `ToolCallDelta` | Incremental tool call fragment for streaming: `Index`, `ID`, `Function` |

## Getting Started

### Blocking Chat

```go
package main

import (
	"context"
	"fmt"
)

func main() {
	mock := &MockLLM{}
	req := &ChatRequest{
		Messages: []Message{
			{Role: RoleUser, Content: "Hello, how are you?"},
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

```bash
go run .
# Output: Chat Response: Hello, how are you?
#         Hello from streaming!
#         [stop] tokens: 42
#         Streamed complete: Hello from streaming!
```

### Streaming Chat

```go
package main

import (
	"context"
	"fmt"
	"strings"
)

func main() {
	mock := &MockLLM{}
	req := &ChatRequest{
		Messages: []Message{
			{Role: RoleUser, Content: "Hello from streaming!"},
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

Add a new provider by creating a file (e.g., `openai.go`) with a struct that implements the `LLM` interface:

```go
type OpenAILLM struct {
	apiKey string
}

func (o *OpenAILLM) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	// Real API call
}

func (o *OpenAILLM) Complete(ctx context.Context, prompt string) (string, error) {
	// Real completion call
}

func (o *OpenAILLM) StreamChat(ctx context.Context, req *ChatRequest) (ChatStream, error) {
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
)

func main() {
	ollama := &OllamaLLM{
		BaseURL: "http://localhost:11434", // default
	}

	resp, err := ollama.Chat(context.Background(), &ChatRequest{
		Messages: []Message{
			{Role: RoleUser, Content: "Why is the sky blue?"},
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
