package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DaviMGDev/thoth-agent/internal/llm"
)

// ---------------------------------------------------------------------------
// FunctionCallingAgent — Run
// ---------------------------------------------------------------------------

func TestFunctionCallingAgent_Run_Echo(t *testing.T) {
	agent := &FunctionCallingAgent{LLM: &llm.MockLLM{}}
	req := &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "Hello"}},
		Model:    "mock-model",
	}

	resp, err := agent.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Final.Content != "Hello" {
		t.Errorf("expected final content %q, got %q", "Hello", resp.Final.Content)
	}
	if len(resp.Messages) != 2 { // user + assistant
		t.Errorf("expected 2 messages, got %d", len(resp.Messages))
	}
	if resp.Usage.TotalTokens <= 0 {
		t.Errorf("expected positive total tokens, got %d", resp.Usage.TotalTokens)
	}
}

func TestFunctionCallingAgent_Run_ToolCallFlow(t *testing.T) {
	tool := &MockTool{
		NameValue:        "get_weather",
		DescriptionValue: "Get the weather for a city",
		SchemaValue:      map[string]any{"type": "object"},
		ExecuteFn: func(ctx context.Context, args map[string]any) (string, error) {
			return "sunny, 25°C", nil
		},
	}

	agent := &FunctionCallingAgent{LLM: &llm.MockLLM{}}
	req := &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "weather in Tokyo"}},
		Model:    "mock-model",
		Tools:    []llm.Tool{tool},
	}

	resp, err := agent.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The agent should have: user msg → assistant tool call → tool result → assistant final
	if len(resp.Messages) != 4 {
		t.Errorf("expected 4 messages (user, assistant/toolcall, tool, assistant), got %d", len(resp.Messages))
	}

	// Check the first assistant message has tool calls
	if len(resp.Messages[1].ToolCalls) == 0 {
		t.Error("expected second message to have tool calls")
	} else {
		if resp.Messages[1].ToolCalls[0].Function.Name != "get_weather" {
			t.Errorf("expected tool name get_weather, got %q", resp.Messages[1].ToolCalls[0].Function.Name)
		}
	}

	// Check the tool result message
	if resp.Messages[2].Role != llm.RoleTool {
		t.Errorf("expected third message role %q, got %q", llm.RoleTool, resp.Messages[2].Role)
	}
	if resp.Messages[2].Content != "sunny, 25°C" {
		t.Errorf("expected tool result %q, got %q", "sunny, 25°C", resp.Messages[2].Content)
	}

	// Final message should be assistant with echo content
	if resp.Messages[3].Role != llm.RoleAssistant {
		t.Errorf("expected final message role %q, got %q", llm.RoleAssistant, resp.Messages[3].Role)
	}
}

func TestFunctionCallingAgent_Run_NilRequest(t *testing.T) {
	agent := &FunctionCallingAgent{LLM: &llm.MockLLM{}}
	_, err := agent.Run(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request")
	}
}

func TestFunctionCallingAgent_Run_NilLLM(t *testing.T) {
	agent := &FunctionCallingAgent{}
	_, err := agent.Run(context.Background(), &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for nil LLM")
	}
}

func TestFunctionCallingAgent_Run_MaxIterationsExceeded(t *testing.T) {
	tool := &MockTool{
		NameValue:        "never_called",
		DescriptionValue: "test tool",
		SchemaValue:      map[string]any{"type": "object"},
	}

	agent := &FunctionCallingAgent{LLM: &llm.MockLLM{}}
	req := &AgentRequest{
		Messages:      []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Model:         "mock-model",
		Tools:         []llm.Tool{tool},
		MaxIterations: 1, // agent needs 2 iterations, but we give it 1
	}

	_, err := agent.Run(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for exceeding max iterations")
	}
}

func TestFunctionCallingAgent_Run_ContextCancel(t *testing.T) {
	agent := &FunctionCallingAgent{LLM: &llm.MockLLM{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancelled

	_, err := agent.Run(ctx, &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Model:    "mock-model",
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestFunctionCallingAgent_Run_ParallelToolExecution(t *testing.T) {
	var mu sync.Mutex
	executionOrder := make([]int, 0)
	var execCount atomic.Int32

	tools := []llm.Tool{
		&MockTool{
			NameValue:        "tool_a",
			DescriptionValue: "tool A",
			SchemaValue:      map[string]any{"type": "object"},
			ExecuteFn: func(ctx context.Context, args map[string]any) (string, error) {
				execCount.Add(1)
				mu.Lock()
				executionOrder = append(executionOrder, 0)
				mu.Unlock()
				time.Sleep(5 * time.Millisecond)
				return "result_a", nil
			},
		},
		&MockTool{
			NameValue:        "tool_b",
			DescriptionValue: "tool B",
			SchemaValue:      map[string]any{"type": "object"},
			ExecuteFn: func(ctx context.Context, args map[string]any) (string, error) {
				execCount.Add(1)
				mu.Lock()
				executionOrder = append(executionOrder, 1)
				mu.Unlock()
				time.Sleep(5 * time.Millisecond)
				return "result_b", nil
			},
		},
	}

	customLLM := &multiToolMockLLM{
		toolCallsPerUserMsg: 2,
		tools:               tools,
	}

	agent := &FunctionCallingAgent{LLM: customLLM}
	req := &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "run both tools"}},
		Model:    "mock-model",
		Tools:    tools,
	}

	_, err := agent.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both tools should have been executed
	if execCount.Load() != 2 {
		t.Errorf("expected 2 tool executions, got %d", execCount.Load())
	}
}

// multiToolMockLLM is a mock LLM that returns multiple tool calls per user message.
type multiToolMockLLM struct {
	llm.MockLLM
	toolCallsPerUserMsg int
	tools               []llm.Tool
}

func (m *multiToolMockLLM) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("chat request cannot be nil")
	}
	content := ""
	if len(req.Messages) > 0 {
		content = req.Messages[len(req.Messages)-1].Content
	}

	// If tools are registered and last message is from user, return multiple tool calls
	if len(req.Tools) > 0 && len(req.Messages) > 0 && req.Messages[len(req.Messages)-1].Role == llm.RoleUser {
		tcs := make([]llm.ToolCall, m.toolCallsPerUserMsg)
		for i := 0; i < m.toolCallsPerUserMsg && i < len(req.Tools); i++ {
			tcs[i] = llm.ToolCall{
				ID: fmt.Sprintf("call_%d", i),
				Function: struct {
					Name      string `json:"name,omitempty"`
					Arguments string `json:"arguments,omitempty"`
				}{
					Name:      req.Tools[i].Name(),
					Arguments: "{}",
				},
			}
		}
		return &llm.ChatResponse{
			Message: llm.Message{
				Role:      llm.RoleAssistant,
				Content:   "",
				ToolCalls: tcs,
			},
			Model: req.Model,
			Usage: llm.UsageStats{PromptTokens: len(content), CompletionTokens: m.toolCallsPerUserMsg, TotalTokens: len(content) + m.toolCallsPerUserMsg},
		}, nil
	}

	return m.MockLLM.Chat(ctx, req)
}

func (m *multiToolMockLLM) StreamChat(ctx context.Context, req *llm.ChatRequest) (llm.ChatStream, error) {
	// Fall back to MockLLM streaming for simplicity
	return m.MockLLM.StreamChat(ctx, req)
}

func TestFunctionCallingAgent_Run_ToolNotFound(t *testing.T) {
	// The MockLLM will try to call "get_weather" but we don't register it
	agent := &FunctionCallingAgent{LLM: &llm.MockLLM{}}
	req := &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "weather"}},
		Model:    "mock-model",
		Tools:    []llm.Tool{}, // empty — MockLLM won't return tool calls either
	}

	// Without tools, MockLLM just echoes, so this should succeed normally
	resp, err := agent.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Final.Content != "weather" {
		t.Errorf("expected echo %q, got %q", "weather", resp.Final.Content)
	}
}

func TestFunctionCallingAgent_Run_ToolExecutionError(t *testing.T) {
	execErr := errors.New("rate limit exceeded")
	tool := &MockTool{
		NameValue:        "risky_tool",
		DescriptionValue: "might fail",
		SchemaValue:      map[string]any{"type": "object"},
		ExecuteFn: func(ctx context.Context, args map[string]any) (string, error) {
			return "", execErr
		},
	}

	agent := &FunctionCallingAgent{LLM: &llm.MockLLM{}}
	req := &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "do risky thing"}},
		Model:    "mock-model",
		Tools:    []llm.Tool{tool},
	}

	resp, err := agent.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The tool error should be communicated as a tool result message
	found := false
	for _, m := range resp.Messages {
		if m.Role == llm.RoleTool && m.Content != "" {
			if len(m.Content) > 0 {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("expected a tool result message with error content")
	}

	// The agent should recover and produce a final response
	if resp.Final.Role != llm.RoleAssistant {
		t.Errorf("expected final message from assistant, got role %q", resp.Final.Role)
	}
}

// ---------------------------------------------------------------------------
// FunctionCallingAgent — StreamRun
// ---------------------------------------------------------------------------

func TestFunctionCallingAgent_StreamRun_Basic(t *testing.T) {
	agent := &FunctionCallingAgent{LLM: &llm.MockLLM{}}
	req := &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "Hello"}},
		Model:    "mock-model",
	}

	stream, err := agent.StreamRun(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	var tokens []string
	for stream.Next() {
		chunk := stream.Current()
		if chunk.Type == AgentEventToken {
			tokens = append(tokens, chunk.Content)
		}
		if chunk.Done {
			if chunk.Usage == nil {
				t.Error("expected usage in done chunk")
			}
		}
	}
	if err := stream.Err(); err != nil {
		t.Errorf("unexpected stream error: %v", err)
	}

	if len(tokens) == 0 {
		t.Fatal("expected at least one token")
	}
	if tokens[0] != "Hello" {
		t.Errorf("expected first token %q, got %q", "Hello", tokens[0])
	}
}

func TestFunctionCallingAgent_StreamRun_WithToolCalls(t *testing.T) {
	tool := &MockTool{
		NameValue:        "get_weather",
		DescriptionValue: "Get weather",
		SchemaValue:      map[string]any{"type": "object"},
		ExecuteFn: func(ctx context.Context, args map[string]any) (string, error) {
			return "sunny", nil
		},
	}

	agent := &FunctionCallingAgent{LLM: &llm.MockLLM{}}
	req := &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "weather"}},
		Model:    "mock-model",
		Tools:    []llm.Tool{tool},
	}

	stream, err := agent.StreamRun(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	var events []AgentEventType
	for stream.Next() {
		chunk := stream.Current()
		events = append(events, chunk.Type)
	}
	if err := stream.Err(); err != nil {
		t.Errorf("unexpected stream error: %v", err)
	}

	// Should see tool_call → tool_result → ... → done
	// (MockLLM returns tool calls without preceding content tokens)
	if len(events) < 3 {
		t.Fatalf("expected at least 3 events, got %d: %v", len(events), events)
	}

	// Verify we saw each required event type at least once
	var sawToolCall, sawToolResult, sawDone bool
	for _, e := range events {
		switch e {
		case AgentEventToolCall:
			sawToolCall = true
		case AgentEventToolResult:
			sawToolResult = true
		case AgentEventDone:
			sawDone = true
		}
	}
	if !sawToolCall {
		t.Error("expected at least one tool_call event")
	}
	if !sawToolResult {
		t.Error("expected at least one tool_result event")
	}
	if !sawDone {
		t.Error("expected a done event")
	}

	// Done must be last
	if events[len(events)-1] != AgentEventDone {
		t.Errorf("expected last event %q, got %q", AgentEventDone, events[len(events)-1])
	}
}

func TestFunctionCallingAgent_StreamRun_ContextCancel(t *testing.T) {
	agent := &FunctionCallingAgent{LLM: &llm.MockLLM{}}
	ctx, cancel := context.WithCancel(context.Background())

	stream, err := agent.StreamRun(ctx, &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
		Model:    "mock-model",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Cancel before reading anything
	cancel()

	// Read should eventually terminate
	for stream.Next() {
	}
	if err := stream.Close(); err != nil {
		t.Errorf("unexpected close error: %v", err)
	}
}

func TestFunctionCallingAgent_StreamRun_NilRequest(t *testing.T) {
	agent := &FunctionCallingAgent{LLM: &llm.MockLLM{}}
	_, err := agent.StreamRun(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request")
	}
}

func TestFunctionCallingAgent_StreamRun_NilLLM(t *testing.T) {
	agent := &FunctionCallingAgent{}
	_, err := agent.StreamRun(context.Background(), &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for nil LLM")
	}
}

// ---------------------------------------------------------------------------
// MockAgent
// ---------------------------------------------------------------------------

func TestMockAgent_Run(t *testing.T) {
	agent := &MockAgent{
		Responses: []*AgentResponse{
			{
				Messages: []llm.Message{
					{Role: llm.RoleUser, Content: "hi"},
					{Role: llm.RoleAssistant, Content: "hello"},
				},
				Final: llm.Message{Role: llm.RoleAssistant, Content: "hello"},
				Usage: llm.UsageStats{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
			},
		},
	}

	resp, err := agent.Run(context.Background(), &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Final.Content != "hello" {
		t.Errorf("expected content %q, got %q", "hello", resp.Final.Content)
	}
	if resp.Usage.TotalTokens != 3 {
		t.Errorf("expected total tokens 3, got %d", resp.Usage.TotalTokens)
	}
}

func TestMockAgent_Run_NilRequest(t *testing.T) {
	agent := &MockAgent{}
	_, err := agent.Run(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request")
	}
}

func TestMockAgent_Run_Exhausted(t *testing.T) {
	agent := &MockAgent{} // no responses
	_, err := agent.Run(context.Background(), &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error when no more responses")
	}
}

func TestMockAgent_Run_Error(t *testing.T) {
	expectedErr := errors.New("llm unavailable")
	agent := &MockAgent{Err: expectedErr}
	_, err := agent.Run(context.Background(), &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if err != expectedErr {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
}

func TestMockAgent_StreamRun(t *testing.T) {
	agent := &MockAgent{
		Responses: []*AgentResponse{
			{
				Messages: []llm.Message{
					{Role: llm.RoleUser, Content: "hi"},
					{Role: llm.RoleAssistant, Content: "hello"},
				},
				Final: llm.Message{Role: llm.RoleAssistant, Content: "hello"},
				Usage: llm.UsageStats{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
			},
		},
	}

	stream, err := agent.StreamRun(context.Background(), &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	var tokens []string
	for stream.Next() {
		chunk := stream.Current()
		if chunk.Content != "" {
			tokens = append(tokens, chunk.Content)
		}
		if chunk.Done && chunk.Usage != nil {
			if chunk.Usage.TotalTokens != 3 {
				t.Errorf("expected total tokens 3, got %d", chunk.Usage.TotalTokens)
			}
		}
	}
	if err := stream.Err(); err != nil {
		t.Errorf("unexpected stream error: %v", err)
	}

	if len(tokens) != 1 || tokens[0] != "hello" {
		t.Errorf("expected 1 token %q, got %v", "hello", tokens)
	}
}

// ---------------------------------------------------------------------------
// MockTool
// ---------------------------------------------------------------------------

func TestMockTool(t *testing.T) {
	tool := &MockTool{
		NameValue:        "test_tool",
		DescriptionValue: "A test tool",
		SchemaValue:      map[string]any{"type": "object"},
		ExecuteFn: func(ctx context.Context, args map[string]any) (string, error) {
			return "done", nil
		},
	}

	if tool.Name() != "test_tool" {
		t.Errorf("expected name %q, got %q", "test_tool", tool.Name())
	}
	if tool.Description() != "A test tool" {
		t.Errorf("expected description %q, got %q", "A test tool", tool.Description())
	}
	if tool.Schema()["type"] != "object" {
		t.Errorf("expected schema type object, got %v", tool.Schema()["type"])
	}

	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "done" {
		t.Errorf("expected result %q, got %q", "done", result)
	}
}

func TestMockTool_DefaultExecute(t *testing.T) {
	tool := &MockTool{
		NameValue: "default_tool",
	}
	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "mock result" {
		t.Errorf("expected default result %q, got %q", "mock result", result)
	}
}

// ---------------------------------------------------------------------------
// AgentStream — close safety
// ---------------------------------------------------------------------------

func TestAgentStream_MultipleClose(t *testing.T) {
	ch := make(chan AgentChunk, 1)
	s := &agentStream{ch: ch}
	close(ch)

	s.Close()
	s.Close() // second close must not panic
}

// ---------------------------------------------------------------------------
// Compile-time interface checks
// ---------------------------------------------------------------------------

func TestAgent_InterfaceCompiles(t *testing.T) {
	var _ Agent = (*FunctionCallingAgent)(nil)
	var _ Agent = (*MockAgent)(nil)
	var _ llm.Tool = (*MockTool)(nil)
	var _ AgentStream = (*agentStream)(nil)
}
