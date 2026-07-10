package main

import (
	"context"
	"testing"
)

func TestMockLLM_Chat(t *testing.T) {
	m := &MockLLM{}

	t.Run("basic echo", func(t *testing.T) {
		req := &ChatRequest{
			Messages: []Message{
				{Role: RoleUser, Content: "Hello"},
			},
			Model:       "mock-model",
			Temperature: 0.7,
			MaxTokens:   100,
		}

		resp, err := m.Chat(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp.Message.Role != RoleAssistant {
			t.Errorf("expected role %q, got %q", RoleAssistant, resp.Message.Role)
		}
		if resp.Message.Content != "Hello" {
			t.Errorf("expected content %q, got %q", "Hello", resp.Message.Content)
		}
		if resp.Model != "mock-model" {
			t.Errorf("expected model %q, got %q", "mock-model", resp.Model)
		}
		if resp.FinishReason != FinishReasonStop {
			t.Errorf("expected FinishReason %q, got %q", FinishReasonStop, resp.FinishReason)
		}
	})

	t.Run("empty messages slice", func(t *testing.T) {
		req := &ChatRequest{
			Messages: []Message{},
			Model:    "mock-model",
		}

		resp, err := m.Chat(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Message.Content != "" {
			t.Errorf("expected empty content, got %q", resp.Message.Content)
		}
	})

	t.Run("nil messages slice", func(t *testing.T) {
		req := &ChatRequest{
			Messages: nil,
			Model:    "mock-model",
		}

		resp, err := m.Chat(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Message.Content != "" {
			t.Errorf("expected empty content, got %q", resp.Message.Content)
		}
	})

	t.Run("empty content", func(t *testing.T) {
		req := &ChatRequest{
			Messages: []Message{
				{Role: RoleUser, Content: ""},
			},
			Model: "mock-model",
		}

		resp, err := m.Chat(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Message.Content != "" {
			t.Errorf("expected empty content, got %q", resp.Message.Content)
		}
	})

	t.Run("multiple messages returns last", func(t *testing.T) {
		req := &ChatRequest{
			Messages: []Message{
				{Role: RoleSystem, Content: "You are a helpful assistant."},
				{Role: RoleUser, Content: "What is Go?"},
				{Role: RoleAssistant, Content: "Go is a programming language."},
				{Role: RoleUser, Content: "Tell me more."},
			},
			Model: "mock-model",
		}

		resp, err := m.Chat(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Message.Content != "Tell me more." {
			t.Errorf("expected last message content %q, got %q", "Tell me more.", resp.Message.Content)
		}
	})

	t.Run("usage stats reflect content length", func(t *testing.T) {
		content := "Hello, World!"
		req := &ChatRequest{
			Messages: []Message{
				{Role: RoleUser, Content: content},
			},
			Model: "mock-model",
		}

		resp, err := m.Chat(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		wantTokens := len(content)
		if resp.Usage.PromptTokens != wantTokens {
			t.Errorf("expected PromptTokens %d, got %d", wantTokens, resp.Usage.PromptTokens)
		}
		if resp.Usage.CompletionTokens != wantTokens {
			t.Errorf("expected CompletionTokens %d, got %d", wantTokens, resp.Usage.CompletionTokens)
		}
		if resp.Usage.TotalTokens != wantTokens*2 {
			t.Errorf("expected TotalTokens %d, got %d", wantTokens*2, resp.Usage.TotalTokens)
		}
	})

	t.Run("nil request returns error", func(t *testing.T) {
		_, err := m.Chat(context.Background(), nil)
		if err == nil {
			t.Fatal("expected error for nil request, got nil")
		}
	})
}

func TestMockLLM_Complete(t *testing.T) {
	m := &MockLLM{}

	t.Run("basic echo", func(t *testing.T) {
		response, err := m.Complete(context.Background(), "Hello")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if response != "Hello" {
			t.Errorf("expected %q, got %q", "Hello", response)
		}
	})

	t.Run("empty string", func(t *testing.T) {
		response, err := m.Complete(context.Background(), "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if response != "" {
			t.Errorf("expected empty string, got %q", response)
		}
	})

	t.Run("unicode and special characters", func(t *testing.T) {
		input := "Hello, 世界! ±≈∫ 🎉"
		response, err := m.Complete(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if response != input {
			t.Errorf("expected %q, got %q", input, response)
		}
	})

	t.Run("multi-line string", func(t *testing.T) {
		input := "line1\nline2\nline3"
		response, err := m.Complete(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if response != input {
			t.Errorf("expected %q, got %q", input, response)
		}
	})
}

func TestMockLLM_StreamChat(t *testing.T) {
	m := &MockLLM{}

	t.Run("basic stream echoes content", func(t *testing.T) {
		req := &ChatRequest{
			Messages: []Message{
				{Role: RoleUser, Content: "Hello"},
			},
			Model: "mock-model",
		}

		stream, err := m.StreamChat(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer stream.Close()

		// First chunk: content
		if !stream.Next() {
			t.Fatal("expected first chunk")
		}
		chunk := stream.Current()
		if chunk.Content != "Hello" {
			t.Errorf("expected content %q, got %q", "Hello", chunk.Content)
		}
		if chunk.Role != RoleAssistant {
			t.Errorf("expected role %q, got %q", RoleAssistant, chunk.Role)
		}

		// Second chunk: finish reason + usage
		if !stream.Next() {
			t.Fatal("expected second chunk")
		}
		chunk = stream.Current()
		if chunk.FinishReason != FinishReasonStop {
			t.Errorf("expected FinishReason %q, got %q", FinishReasonStop, chunk.FinishReason)
		}
		if chunk.Usage == nil {
			t.Fatal("expected usage stats")
		}
		wantTokens := len("Hello")
		if chunk.Usage.PromptTokens != wantTokens {
			t.Errorf("expected PromptTokens %d, got %d", wantTokens, chunk.Usage.PromptTokens)
		}
		if chunk.Usage.CompletionTokens != wantTokens {
			t.Errorf("expected CompletionTokens %d, got %d", wantTokens, chunk.Usage.CompletionTokens)
		}
		if chunk.Usage.TotalTokens != wantTokens*2 {
			t.Errorf("expected TotalTokens %d, got %d", wantTokens*2, chunk.Usage.TotalTokens)
		}

		// Stream exhausted
		if stream.Next() {
			t.Fatal("expected no more chunks")
		}
		if err := stream.Err(); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("empty messages slice", func(t *testing.T) {
		req := &ChatRequest{
			Messages: []Message{},
			Model:    "mock-model",
		}

		stream, err := m.StreamChat(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer stream.Close()

		if !stream.Next() {
			t.Fatal("expected first chunk")
		}
		if stream.Current().Content != "" {
			t.Errorf("expected empty content, got %q", stream.Current().Content)
		}
	})

	t.Run("nil messages slice", func(t *testing.T) {
		req := &ChatRequest{
			Messages: nil,
			Model:    "mock-model",
		}

		stream, err := m.StreamChat(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer stream.Close()

		if !stream.Next() {
			t.Fatal("expected first chunk")
		}
		if stream.Current().Content != "" {
			t.Errorf("expected empty content, got %q", stream.Current().Content)
		}
	})

	t.Run("empty content", func(t *testing.T) {
		req := &ChatRequest{
			Messages: []Message{
				{Role: RoleUser, Content: ""},
			},
			Model: "mock-model",
		}

		stream, err := m.StreamChat(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer stream.Close()

		if !stream.Next() {
			t.Fatal("expected first chunk")
		}
		if stream.Current().Content != "" {
			t.Errorf("expected empty content, got %q", stream.Current().Content)
		}
	})

	t.Run("multiple messages returns last", func(t *testing.T) {
		req := &ChatRequest{
			Messages: []Message{
				{Role: RoleSystem, Content: "You are a helpful assistant."},
				{Role: RoleUser, Content: "What is Go?"},
				{Role: RoleAssistant, Content: "Go is a programming language."},
				{Role: RoleUser, Content: "Tell me more."},
			},
			Model: "mock-model",
		}

		stream, err := m.StreamChat(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer stream.Close()

		if !stream.Next() {
			t.Fatal("expected first chunk")
		}
		if stream.Current().Content != "Tell me more." {
			t.Errorf("expected last message content %q, got %q", "Tell me more.", stream.Current().Content)
		}
	})

	t.Run("nil request returns error", func(t *testing.T) {
		_, err := m.StreamChat(context.Background(), nil)
		if err == nil {
			t.Fatal("expected error for nil request, got nil")
		}
	})

	t.Run("close mid-stream stops iteration", func(t *testing.T) {
		req := &ChatRequest{
			Messages: []Message{
				{Role: RoleUser, Content: "Hello"},
			},
			Model: "mock-model",
		}

		stream, err := m.StreamChat(context.Background(), req)
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

		// Close mid-stream
		if err := stream.Close(); err != nil {
			t.Errorf("unexpected close error: %v", err)
		}

		// Next() must return false after close
		if stream.Next() {
			t.Fatal("expected no more chunks after close")
		}
	})

	t.Run("multiple close is safe", func(t *testing.T) {
		req := &ChatRequest{
			Messages: []Message{
				{Role: RoleUser, Content: "Hello"},
			},
			Model: "mock-model",
		}

		stream, err := m.StreamChat(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		stream.Close()
		stream.Close() // second close must not panic
	})
}
