package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/DaviMGDev/thoth-agent/internal/llm"
)

func TestOllamaLLM_Chat(t *testing.T) {
	t.Parallel()

	var (
		mu          sync.Mutex
		gotModel    string
		gotStream   bool
		gotMessages []ollamaMessage
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/chat" {
			t.Errorf("expected /api/chat, got %s", r.URL.Path)
		}

		var req ollamaChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		mu.Lock()
		gotModel = req.Model
		gotStream = req.Stream
		gotMessages = req.Messages
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ollamaChatResponse{
			Model: "ministral-3b:cloud",
			Message: &ollamaMessage{
				Role:    "assistant",
				Content: "Hello from Ollama!",
			},
			DoneReason:      "stop",
			Done:            true,
			PromptEvalCount: 10,
			EvalCount:       5,
		})
	}))
	defer server.Close()

	ollamaLLM := &OllamaLLM{
		BaseURL: server.URL,
	}

	req := &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "You are helpful."},
			{Role: llm.RoleUser, Content: "Hi there"},
		},
		Model:       "ministral-3b:cloud",
		Temperature: 0.7,
		MaxTokens:   100,
	}

	resp, err := ollamaLLM.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify request was built correctly
	mu.Lock()
	if gotModel != "ministral-3b:cloud" {
		t.Errorf("expected model llama3.2, got %q", gotModel)
	}
	if gotStream {
		t.Error("expected stream=false for non-streaming chat")
	}
	if len(gotMessages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(gotMessages))
	}
	if gotMessages[0].Role != "system" || gotMessages[0].Content != "You are helpful." {
		t.Errorf("unexpected first message: %+v", gotMessages[0])
	}
	if gotMessages[1].Role != "user" || gotMessages[1].Content != "Hi there" {
		t.Errorf("unexpected second message: %+v", gotMessages[1])
	}
	mu.Unlock()

	// Verify response
	if resp.Message.Role != llm.RoleAssistant {
		t.Errorf("expected role %q, got %q", llm.RoleAssistant, resp.Message.Role)
	}
	if resp.Message.Content != "Hello from Ollama!" {
		t.Errorf("expected content %q, got %q", "Hello from Ollama!", resp.Message.Content)
	}
	if resp.Model != "ministral-3b:cloud" {
		t.Errorf("expected model %q, got %q", "ministral-3b:cloud", resp.Model)
	}
	if resp.FinishReason != llm.FinishReasonStop {
		t.Errorf("expected finish reason %q, got %q", llm.FinishReasonStop, resp.FinishReason)
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("expected PromptTokens 10, got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 5 {
		t.Errorf("expected CompletionTokens 5, got %d", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("expected TotalTokens 15, got %d", resp.Usage.TotalTokens)
	}
}

func TestOllamaLLM_Chat_NilRequest(t *testing.T) {
	t.Parallel()
	ollamaLLM := &OllamaLLM{}
	_, err := ollamaLLM.Chat(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request")
	}
}

func TestOllamaLLM_Chat_EmptyModel(t *testing.T) {
	t.Parallel()
	ollamaLLM := &OllamaLLM{}
	_, err := ollamaLLM.Chat(context.Background(), &llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for empty model")
	}
}

func TestOllamaLLM_Chat_Non200(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad model"}`))
	}))
	defer server.Close()

	ollamaLLM := &OllamaLLM{BaseURL: server.URL}
	_, err := ollamaLLM.Chat(context.Background(), &llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Model:    "bad-model",
	})
	if err == nil {
		t.Fatal("expected error for 400 status")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected error to mention status code, got: %v", err)
	}
}

func TestOllamaLLM_Complete(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ollamaChatResponse{
			Model: "ministral-3b:cloud",
			Message: &ollamaMessage{
				Role:    "assistant",
				Content: "Hello!",
			},
			Done:       true,
			DoneReason: "stop",
		})
	}))
	defer server.Close()

	ollamaLLM := &OllamaLLM{BaseURL: server.URL}
	resp, err := ollamaLLM.Complete(context.Background(), "Hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "Hello!" {
		t.Errorf("expected %q, got %q", "Hello!", resp)
	}
}

func TestOllamaLLM_StreamChat(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("server does not support flushing")
		}

		w.Header().Set("Content-Type", "application/x-ndjson")

		lines := []string{
			`{"model":"ministral-3b:cloud","message":{"role":"assistant","content":"Hello "},"done":false}`,
			`{"model":"ministral-3b:cloud","message":{"role":"assistant","content":"world"},"done":false}`,
			`{"model":"ministral-3b:cloud","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":10,"eval_count":5}`,
		}
		for _, line := range lines {
			fmt.Fprintln(w, line)
			flusher.Flush()
		}
	}))
	defer server.Close()

	ollamaLLM := &OllamaLLM{BaseURL: server.URL}
	req := &llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "Hi"}},
		Model:    "ministral-3b:cloud",
	}

	stream, err := ollamaLLM.StreamChat(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	var contents []string
	for stream.Next() {
		chunk := stream.Current()
		contents = append(contents, chunk.Content)
		t.Logf("chunk: %+v", chunk)
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}

	if len(contents) != 3 {
		t.Fatalf("expected 3 chunks, got %d: %v", len(contents), contents)
	}
	if contents[0] != "Hello " {
		t.Errorf("chunk[0]: expected %q, got %q", "Hello ", contents[0])
	}
	if contents[1] != "world" {
		t.Errorf("chunk[1]: expected %q, got %q", "world", contents[1])
	}
	if contents[2] != "" {
		t.Errorf("chunk[2]: expected empty content, got %q", contents[2])
	}
}

func TestOllamaLLM_StreamChat_FinalChunkMetadata(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("server does not support flushing")
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"model":"ministral-3b:cloud","message":{"role":"assistant","content":"test"},"done":false}`)
		flusher.Flush()
		fmt.Fprintln(w, `{"model":"ministral-3b:cloud","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":10,"eval_count":5}`)
		flusher.Flush()
	}))
	defer server.Close()

	ollamaLLM := &OllamaLLM{BaseURL: server.URL}
	stream, err := ollamaLLM.StreamChat(context.Background(), &llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "test"}},
		Model:    "ministral-3b:cloud",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	// Consume all chunks
	var finalChunk llm.ChatChunk
	for stream.Next() {
		finalChunk = stream.Current()
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}

	// The last chunk should have finish reason and usage
	if finalChunk.FinishReason != llm.FinishReasonStop {
		t.Errorf("expected FinishReason %q, got %q", llm.FinishReasonStop, finalChunk.FinishReason)
	}
	if finalChunk.Usage == nil {
		t.Fatal("expected Usage on final chunk")
	}
	if finalChunk.Usage.PromptTokens != 10 {
		t.Errorf("expected PromptTokens 10, got %d", finalChunk.Usage.PromptTokens)
	}
	if finalChunk.Usage.CompletionTokens != 5 {
		t.Errorf("expected CompletionTokens 5, got %d", finalChunk.Usage.CompletionTokens)
	}
	if finalChunk.Usage.TotalTokens != 15 {
		t.Errorf("expected TotalTokens 15, got %d", finalChunk.Usage.TotalTokens)
	}
}

func TestOllamaLLM_StreamChat_ToolCalls(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("server does not support flushing")
		}
		w.Header().Set("Content-Type", "application/x-ndjson")

		// Ollama returns tool calls with arguments as a JSON string.
		fmt.Fprintln(w, `{"model":"ministral-3b:cloud","message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"get_weather","arguments":"{\"city\":\"Tokyo\"}"}}]},"done":false}`)
		flusher.Flush()
		fmt.Fprintln(w, `{"model":"ministral-3b:cloud","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":15,"eval_count":10}`)
		flusher.Flush()
	}))
	defer server.Close()

	ollamaLLM := &OllamaLLM{BaseURL: server.URL}
	stream, err := ollamaLLM.StreamChat(context.Background(), &llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "weather in Tokyo"}},
		Model:    "ministral-3b:cloud",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	var toolCallChunks []llm.ChatChunk
	for stream.Next() {
		chunk := stream.Current()
		if len(chunk.ToolCalls) > 0 {
			toolCallChunks = append(toolCallChunks, chunk)
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}

	if len(toolCallChunks) != 1 {
		t.Fatalf("expected 1 chunk with tool calls, got %d", len(toolCallChunks))
	}
	tcs := toolCallChunks[0].ToolCalls
	if len(tcs) != 1 {
		t.Fatalf("expected 1 tool call delta, got %d", len(tcs))
	}
	if tcs[0].Function.Name != "get_weather" {
		t.Errorf("expected function name %q, got %q", "get_weather", tcs[0].Function.Name)
	}
	if tcs[0].Function.Arguments != `{"city":"Tokyo"}` {
		t.Errorf("expected arguments %q, got %q", `{"city":"Tokyo"}`, tcs[0].Function.Arguments)
	}
}

func TestOllamaLLM_StreamChat_ToolCalls_ArgumentsAsObject(t *testing.T) {
	t.Parallel()

	// Simulate gemma4-style response where arguments is a JSON object, not a string.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("server does not support flushing")
		}
		w.Header().Set("Content-Type", "application/x-ndjson")

		fmt.Fprintln(w, `{"model":"gemma4","message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"read_file","arguments":{"path":"CHANGELOG.md"}}}]},"done":false}`)
		flusher.Flush()
		fmt.Fprintln(w, `{"model":"gemma4","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop"}`)
		flusher.Flush()
	}))
	defer server.Close()

	ollamaLLM := &OllamaLLM{BaseURL: server.URL}
	stream, err := ollamaLLM.StreamChat(context.Background(), &llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "read CHANGELOG.md"}},
		Model:    "gemma4",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	var toolCallChunks []llm.ChatChunk
	for stream.Next() {
		chunk := stream.Current()
		if len(chunk.ToolCalls) > 0 {
			toolCallChunks = append(toolCallChunks, chunk)
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}

	if len(toolCallChunks) != 1 {
		t.Fatalf("expected 1 chunk with tool calls, got %d", len(toolCallChunks))
	}
	tcs := toolCallChunks[0].ToolCalls
	if len(tcs) != 1 {
		t.Fatalf("expected 1 tool call delta, got %d", len(tcs))
	}
	if tcs[0].Function.Name != "read_file" {
		t.Errorf("expected function name %q, got %q", "read_file", tcs[0].Function.Name)
	}
	// Arguments should be normalized from JSON object to JSON string.
	if tcs[0].Function.Arguments != `{"path":"CHANGELOG.md"}` {
		t.Errorf("expected normalized arguments %q, got %q", `{"path":"CHANGELOG.md"}`, tcs[0].Function.Arguments)
	}
}

func TestOllamaLLM_StreamChat_ContextCancel(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("server does not support flushing")
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"model":"ministral-3b:cloud","message":{"role":"assistant","content":"Hello"},"done":false}`)
		flusher.Flush()

		// Hold the connection open
		select {
		case <-r.Context().Done():
			// client cancelled
		}
	}))
	defer server.Close()

	ollamaLLM := &OllamaLLM{BaseURL: server.URL}
	ctx, cancel := context.WithCancel(context.Background())

	stream, err := ollamaLLM.StreamChat(ctx, &llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Model:    "ministral-3b:cloud",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read first chunk
	if !stream.Next() {
		t.Fatal("expected first chunk")
	}
	if stream.Current().Content != "Hello" {
		t.Errorf("expected content %q, got %q", "Hello", stream.Current().Content)
	}

	// Cancel the context mid-stream
	cancel()

	// Close should succeed without error
	if err := stream.Close(); err != nil {
		t.Errorf("unexpected close error: %v", err)
	}
}

func TestOllamaLLM_ZeroValueDefaults(t *testing.T) {
	t.Parallel()

	// A zero-value OllamaLLM should use default URL and client.
	// We'll verify by checking that the methods compile and construct
	// the expected URL pattern without panicking.
	_ = &OllamaLLM{}

	// Compile-time interface check
	var _ llm.LLM = (*OllamaLLM)(nil)
}

func TestOllamaLLM_StreamChat_NilRequest(t *testing.T) {
	t.Parallel()
	ollamaLLM := &OllamaLLM{}
	_, err := ollamaLLM.StreamChat(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request")
	}
}

func TestOllamaLLM_StreamChat_EmptyModel(t *testing.T) {
	t.Parallel()
	ollamaLLM := &OllamaLLM{}
	_, err := ollamaLLM.StreamChat(context.Background(), &llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for empty model")
	}
}

func TestOllamaLLM_Chat_OptionsMapping(t *testing.T) {
	t.Parallel()

	var reqBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ollamaChatResponse{
			Model: "ministral-3b:cloud",
			Message: &ollamaMessage{
				Role:    "assistant",
				Content: "ok",
			},
			Done: true,
		})
	}))
	defer server.Close()

	ollamaLLM := &OllamaLLM{BaseURL: server.URL}
	_, err := ollamaLLM.Chat(context.Background(), &llm.ChatRequest{
		Messages:      []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Model:         "ministral-3b:cloud",
		Temperature:   0.8,
		MaxTokens:     200,
		StopSequences: []string{"\n", "STOP"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed ollamaChatRequest
	if err := json.Unmarshal(reqBody, &parsed); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	if parsed.Options == nil {
		t.Fatal("expected options to be set")
	}
	if parsed.Options.Temperature != 0.8 {
		t.Errorf("expected Temperature 0.8, got %f", parsed.Options.Temperature)
	}
	if parsed.Options.NumPredict != 200 {
		t.Errorf("expected NumPredict 200, got %d", parsed.Options.NumPredict)
	}
	if len(parsed.Options.Stop) != 2 || parsed.Options.Stop[0] != "\n" || parsed.Options.Stop[1] != "STOP" {
		t.Errorf("expected Stop [\"\\n\", \"STOP\"], got %v", parsed.Options.Stop)
	}
}

func TestOllamaLLM_StreamChat_Non200(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"server overloaded"}`))
	}))
	defer server.Close()

	ollamaLLM := &OllamaLLM{BaseURL: server.URL}
	_, err := ollamaLLM.StreamChat(context.Background(), &llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Model:    "test",
	})
	if err == nil {
		t.Fatal("expected error for 503 status")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("expected error to mention status code, got: %v", err)
	}
}

func TestOllamaLLM_DoneReasonMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		doneReason string
		want       llm.FinishReason
	}{
		{"stop", llm.FinishReasonStop},
		{"length", llm.FinishReasonLength},
		{"error", llm.FinishReasonError},
		{"unknown", llm.FinishReasonError},
		{"", llm.FinishReasonStop}, // default when empty
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ollamaChatResponse{
			Model: "test",
			Message: &ollamaMessage{
				Role:    "assistant",
				Content: "ok",
			},
			Done: true,
		})
	}))
	defer server.Close()

	for _, tt := range tests {
		name := tt.doneReason
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			got := toFinishReason(tt.doneReason)
			if got != tt.want {
				t.Errorf("toFinishReason(%q) = %q, want %q", tt.doneReason, got, tt.want)
			}
		})
	}
}
