package main

import (
	"context"
	"testing"
)

func TestMockLLM_Chat(t *testing.T) {
	m := &MockLLM{}
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
}

func TestMockLLM_Complete(t *testing.T) {
	m := &MockLLM{}

	response, err := m.Complete(context.Background(), "Hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if response != "Hello" {
		t.Errorf("expected %q, got %q", "Hello", response)
	}
}
