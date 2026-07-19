package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/DaviMGDev/thoth-agent/internal/llm"
)

// ---------------------------------------------------------------------------
// Hook — chaining order (Before forward, After reverse)
// ---------------------------------------------------------------------------

type orderTrackingHook struct {
	BaseHook
	name  string
	order *[]string
}

func (h *orderTrackingHook) BeforeAgent(ctx context.Context, req *AgentRequest) (*AgentRequest, error) {
	*h.order = append(*h.order, h.name+".before_agent")
	return req, nil
}

func (h *orderTrackingHook) AfterAgent(ctx context.Context, req *AgentRequest, resp *AgentResponse, runErr error) (*AgentResponse, error) {
	*h.order = append(*h.order, h.name+".after_agent")
	return resp, nil
}

func TestHook_ChainingOrder(t *testing.T) {
	var order []string

	hooks := []Hook{
		&orderTrackingHook{name: "A", order: &order},
		&orderTrackingHook{name: "B", order: &order},
		&orderTrackingHook{name: "C", order: &order},
	}

	agent := &FunctionCallingAgent{
		LLM:   &llm.MockLLM{},
		Hooks: hooks,
	}

	req := &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Model:    "mock",
	}
	_, err := agent.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Before: A → B → C (forward)
	// After:  C → B → A (reverse)
	want := []string{
		"A.before_agent", "B.before_agent", "C.before_agent",
		"C.after_agent", "B.after_agent", "A.after_agent",
	}
	if len(order) != len(want) {
		t.Fatalf("expected %d events, got %d: %v", len(want), len(order), order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("event %d: want %q, got %q", i, want[i], order[i])
		}
	}
}

// ---------------------------------------------------------------------------
// Hook — BeforeLLM injects a system message
// ---------------------------------------------------------------------------

type messageInjectorHook struct {
	BaseHook
	msg llm.Message
}

func (h *messageInjectorHook) BeforeLLM(ctx context.Context, req *llm.ChatRequest) (*llm.ChatRequest, error) {
	req.Messages = append([]llm.Message{h.msg}, req.Messages...)
	return req, nil
}

// captureLLMHook records the ChatRequest it sees in BeforeLLM for inspection.
type captureBeforeLLMHook struct {
	BaseHook
	mu   sync.Mutex
	reqs []*llm.ChatRequest
}

func (h *captureBeforeLLMHook) BeforeLLM(ctx context.Context, req *llm.ChatRequest) (*llm.ChatRequest, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.reqs = append(h.reqs, req)
	return req, nil
}

func TestHook_BeforeLLM_InjectsMessage(t *testing.T) {
	injectedMsg := llm.Message{
		Role:    llm.RoleSystem,
		Content: "The user is in timezone UTC-3",
	}

	capture := &captureBeforeLLMHook{}
	agent := &FunctionCallingAgent{
		LLM: &llm.MockLLM{},
		Hooks: []Hook{
			&messageInjectorHook{msg: injectedMsg},
			capture,
		},
	}

	req := &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Model:    "mock",
	}
	_, err := agent.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(capture.reqs) != 1 {
		t.Fatalf("expected 1 captured request, got %d", len(capture.reqs))
	}

	// The injected message should be first, and the second hook (capture) should see it.
	messages := capture.reqs[0].Messages
	if len(messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(messages))
	}
	if messages[0].Role != llm.RoleSystem || messages[0].Content != "The user is in timezone UTC-3" {
		t.Errorf("first message not the injected one: %+v", messages[0])
	}
}

// ---------------------------------------------------------------------------
// Hook — BeforeLLM error aborts the agent
// ---------------------------------------------------------------------------

type errorHook struct {
	BaseHook
	err error
}

func (h *errorHook) BeforeLLM(ctx context.Context, req *llm.ChatRequest) (*llm.ChatRequest, error) {
	return nil, h.err
}

func TestHook_BeforeLLM_ErrorAbortsAgent(t *testing.T) {
	wantErr := errors.New("hook rejected LLM call")

	agent := &FunctionCallingAgent{
		LLM:   &llm.MockLLM{},
		Hooks: []Hook{&errorHook{err: wantErr}},
	}

	req := &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Model:    "mock",
	}
	_, err := agent.Run(context.Background(), req)
	if err == nil {
		t.Fatal("expected error from hook, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected error %v, got %v", wantErr, err)
	}
}

// ---------------------------------------------------------------------------
// Hook — BeforeAgent error aborts before any LLM call
// ---------------------------------------------------------------------------

type beforeAgentErrorHook struct {
	BaseHook
	err error
}

func (h *beforeAgentErrorHook) BeforeAgent(ctx context.Context, req *AgentRequest) (*AgentRequest, error) {
	return nil, h.err
}

func TestHook_BeforeAgent_ErrorAbortsAgent(t *testing.T) {
	wantErr := errors.New("hook rejected agent run")

	agent := &FunctionCallingAgent{
		LLM:   &llm.MockLLM{},
		Hooks: []Hook{&beforeAgentErrorHook{err: wantErr}},
	}

	req := &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Model:    "mock",
	}
	_, err := agent.Run(context.Background(), req)
	if err == nil {
		t.Fatal("expected error from hook, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected error %v, got %v", wantErr, err)
	}
}

// ---------------------------------------------------------------------------
// Hook — AfterAgent fires on all exit paths
// ---------------------------------------------------------------------------

type afterAgentTrackingHook struct {
	BaseHook
	mu     sync.Mutex
	count  int
	runErr error // last runErr received
}

func (h *afterAgentTrackingHook) AfterAgent(ctx context.Context, req *AgentRequest, resp *AgentResponse, runErr error) (*AgentResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.count++
	h.runErr = runErr
	return resp, nil
}

func TestHook_AfterAgent_OnSuccess(t *testing.T) {
	hook := &afterAgentTrackingHook{}
	agent := &FunctionCallingAgent{
		LLM:   &llm.MockLLM{},
		Hooks: []Hook{hook},
	}

	_, err := agent.Run(context.Background(), &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Model:    "mock",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hook.count != 1 {
		t.Errorf("expected AfterAgent to fire once, got %d", hook.count)
	}
	if hook.runErr != nil {
		t.Errorf("expected runErr nil on success, got %v", hook.runErr)
	}
}

func TestHook_AfterAgent_OnLLMError(t *testing.T) {
	// Create a mock LLM that always errors — we use the nil-model path
	// which OllamaLLM rejects, but MockLLM doesn't. Instead, use a custom
	// LLM that returns an error.
	errLLM := &errorLLM{err: errors.New("llm unavailable")}

	hook := &afterAgentTrackingHook{}
	agent := &FunctionCallingAgent{
		LLM:   errLLM,
		Hooks: []Hook{hook},
	}

	_, err := agent.Run(context.Background(), &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Model:    "mock",
	})
	if err == nil {
		t.Fatal("expected LLM error, got nil")
	}
	if hook.count != 1 {
		t.Errorf("expected AfterAgent to fire once on LLM error, got %d", hook.count)
	}
}

func TestHook_AfterAgent_OnMaxIterations(t *testing.T) {
	hook := &afterAgentTrackingHook{}

	// MockLLM with tools always returns tool calls to user messages,
	// so with MaxIterations=1 it will exceed the limit.
	tool := &MockTool{
		NameValue:   "loop_forever",
		SchemaValue: map[string]any{"type": "object"},
	}
	agent := &FunctionCallingAgent{
		LLM:   &llm.MockLLM{},
		Hooks: []Hook{hook},
	}

	_, err := agent.Run(context.Background(), &AgentRequest{
		Messages:      []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Model:         "mock",
		Tools:         []llm.Tool{tool},
		MaxIterations: 1,
	})
	if err == nil {
		t.Fatal("expected max iterations error, got nil")
	}
	if hook.count != 1 {
		t.Errorf("expected AfterAgent to fire once on max iterations, got %d", hook.count)
	}
	if hook.runErr == nil {
		t.Error("expected non-nil runErr on max iterations")
	}
}

// errorLLM is an LLM that always returns an error.
type errorLLM struct {
	err error
}

func (e *errorLLM) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, e.err
}

func (e *errorLLM) Complete(ctx context.Context, prompt string) (string, error) {
	return "", e.err
}

func (e *errorLLM) StreamChat(ctx context.Context, req *llm.ChatRequest) (llm.ChatStream, error) {
	return nil, e.err
}

// ---------------------------------------------------------------------------
// Hook — AfterTool receives execErr
// ---------------------------------------------------------------------------

type afterToolCaptureHook struct {
	BaseHook
	mu       sync.Mutex
	results  []string
	execErrs []error
}

func (h *afterToolCaptureHook) AfterTool(ctx context.Context, call *llm.ToolCall, result string, execErr error) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.results = append(h.results, result)
	h.execErrs = append(h.execErrs, execErr)
	return result, nil
}

func TestHook_AfterTool_ReceivesExecError(t *testing.T) {
	execErr := errors.New("tool failed")
	tool := &MockTool{
		NameValue:   "failing_tool",
		SchemaValue: map[string]any{"type": "object"},
		ExecuteFn: func(ctx context.Context, args map[string]any) (string, error) {
			return "", execErr
		},
	}

	hook := &afterToolCaptureHook{}
	agent := &FunctionCallingAgent{
		LLM:   &llm.MockLLM{},
		Hooks: []Hook{hook},
	}

	_, err := agent.Run(context.Background(), &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "run failing tool"}},
		Model:    "mock",
		Tools:    []llm.Tool{tool},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(hook.execErrs) != 1 {
		t.Fatalf("expected 1 AfterTool call, got %d", len(hook.execErrs))
	}
	if !errors.Is(hook.execErrs[0], execErr) {
		t.Errorf("expected execErr %v, got %v", execErr, hook.execErrs[0])
	}
	// result should be empty string on error
	if hook.results[0] != "" {
		t.Errorf("expected empty result on tool error, got %q", hook.results[0])
	}
}

// ---------------------------------------------------------------------------
// Hook — nil Hooks is safe (zero overhead)
// ---------------------------------------------------------------------------

func TestHook_NilHooks_NoPanic(t *testing.T) {
	// Zero-value agent — Hooks is nil
	agent := &FunctionCallingAgent{LLM: &llm.MockLLM{}}

	_, err := agent.Run(context.Background(), &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Model:    "mock",
	})
	if err != nil {
		t.Fatalf("unexpected error with nil hooks: %v", err)
	}
}

func TestHook_EmptyHooks_NoPanic(t *testing.T) {
	agent := &FunctionCallingAgent{
		LLM:   &llm.MockLLM{},
		Hooks: []Hook{},
	}

	_, err := agent.Run(context.Background(), &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Model:    "mock",
	})
	if err != nil {
		t.Fatalf("unexpected error with empty hooks: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Hook — multiple hooks compose correctly (transformation chain)
// ---------------------------------------------------------------------------

type prefixResultHook struct {
	BaseHook
	prefix string
}

func (h *prefixResultHook) AfterTool(ctx context.Context, call *llm.ToolCall, result string, execErr error) (string, error) {
	if execErr != nil {
		return result, nil
	}
	return h.prefix + result, nil
}

func TestHook_MultipleHooks_AfterToolComposition(t *testing.T) {
	tool := &MockTool{
		NameValue:   "get_temp",
		SchemaValue: map[string]any{"type": "object"},
		ExecuteFn: func(ctx context.Context, args map[string]any) (string, error) {
			return "25°C", nil
		},
	}

	// Two hooks that each add a prefix. After runs in reverse:
	// Hook[1] fires first (closest to tool), Hook[0] fires last.
	// Hook[1] adds "[sanitized] " → "[sanitized] 25°C"
	// Hook[0] adds "[enriched] " → "[enriched] [sanitized] 25°C"
	hooks := []Hook{
		&prefixResultHook{prefix: "[enriched] "},
		&prefixResultHook{prefix: "[sanitized] "},
	}

	// Capture what the LLM sees (the tool result fed back to messages)
	capture := &captureBeforeLLMHook{}
	allHooks := append(hooks, capture)

	agent := &FunctionCallingAgent{
		LLM:   &llm.MockLLM{},
		Hooks: allHooks,
	}

	_, err := agent.Run(context.Background(), &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "what's the temperature?"}},
		Model:    "mock",
		Tools:    []llm.Tool{tool},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// After the tool loop, the MockLLM echoes the last message on the second iteration.
	// The tool result in the message history should contain the transformed result.
	// The second captured BeforeLLM request should include the tool result message.
	if len(capture.reqs) < 2 {
		t.Fatalf("expected at least 2 LLM calls (tool + response), got %d", len(capture.reqs))
	}

	// Find the tool result message in the second request
	found := false
	for _, msg := range capture.reqs[1].Messages {
		if msg.Role == llm.RoleTool {
			if msg.Content == "[enriched] [sanitized] 25°C" {
				found = true
			} else {
				t.Errorf("expected composed tool result %q, got %q", "[enriched] [sanitized] 25°C", msg.Content)
			}
		}
	}
	if !found {
		t.Error("tool result message with expected content not found")
	}
}

// ---------------------------------------------------------------------------
// Hook — StreamRun integration
// ---------------------------------------------------------------------------

func TestHook_StreamRun_NilHooks(t *testing.T) {
	agent := &FunctionCallingAgent{LLM: &llm.MockLLM{}}
	stream, err := agent.StreamRun(context.Background(), &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Model:    "mock",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	count := 0
	for stream.Next() {
		count++
	}
	if err := stream.Err(); err != nil {
		t.Errorf("unexpected stream error: %v", err)
	}
	if count == 0 {
		t.Error("expected at least one stream event")
	}
}

func TestHook_StreamRun_BeforeAgentError(t *testing.T) {
	wantErr := errors.New("stream rejected")
	agent := &FunctionCallingAgent{
		LLM:   &llm.MockLLM{},
		Hooks: []Hook{&beforeAgentErrorHook{err: wantErr}},
	}

	stream, err := agent.StreamRun(context.Background(), &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Model:    "mock",
	})
	if err != nil {
		t.Fatalf("unexpected StreamRun error: %v", err)
	}

	// Stream should close immediately because the goroutine returns on hook error.
	for stream.Next() {
	}
	// No error from the stream itself — the hook error is swallowed in the goroutine.
}

// ---------------------------------------------------------------------------
// Hook — AfterAgent fires on context cancellation
// ---------------------------------------------------------------------------

func TestHook_AfterAgent_OnContextCancel(t *testing.T) {
	hook := &afterAgentTrackingHook{}
	agent := &FunctionCallingAgent{
		LLM:   &llm.MockLLM{},
		Hooks: []Hook{hook},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancelled

	_, err := agent.Run(ctx, &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Model:    "mock",
	})
	if err == nil {
		t.Fatal("expected context error, got nil")
	}
	if hook.count != 1 {
		t.Errorf("expected AfterAgent to fire once on ctx cancel, got %d", hook.count)
	}
}

// ---------------------------------------------------------------------------
// Hook — BaseHook has no effect (compile-time + behavioral check)
// ---------------------------------------------------------------------------

func TestHook_BaseHook_Noop(t *testing.T) {
	// A hook that embeds BaseHook and overrides nothing should be transparent.
	hook := &BaseHook{}
	agent := &FunctionCallingAgent{
		LLM:   &llm.MockLLM{},
		Hooks: []Hook{hook},
	}

	resp, err := agent.Run(context.Background(), &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
		Model:    "mock",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Final.Content != "hello" {
		t.Errorf("expected echo %q, got %q", "hello", resp.Final.Content)
	}
}

// ---------------------------------------------------------------------------
// Compile-time checks
// ---------------------------------------------------------------------------

func TestHook_InterfaceCompiles(t *testing.T) {
	var _ Hook = (*BaseHook)(nil)
	var _ Hook = &orderTrackingHook{}
	var _ Hook = &messageInjectorHook{}
	var _ Hook = &errorHook{}
	var _ Hook = &afterAgentTrackingHook{}
	var _ Hook = &afterToolCaptureHook{}
	var _ Hook = &prefixResultHook{}
}

// argsTransformHook injects a field into tool arguments using proper JSON manipulation.
type argsTransformHook struct {
	BaseHook
}

func (h *argsTransformHook) BeforeTool(ctx context.Context, call *llm.ToolCall) (*llm.ToolCall, error) {
	var args map[string]any
	if call.Function.Arguments != "" {
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			return call, nil // skip if unparseable
		}
	} else {
		args = make(map[string]any)
	}
	args["injected"] = true
	raw, _ := json.Marshal(args)
	call.Function.Arguments = string(raw)
	return call, nil
}

func TestHook_BeforeTool_TransformsArguments(t *testing.T) {
	var capturedArgs map[string]any
	tool := &MockTool{
		NameValue:   "weather",
		SchemaValue: map[string]any{"type": "object"},
		ExecuteFn: func(ctx context.Context, args map[string]any) (string, error) {
			capturedArgs = args
			return "sunny", nil
		},
	}

	agent := &FunctionCallingAgent{
		LLM:   &llm.MockLLM{},
		Hooks: []Hook{&argsTransformHook{}},
	}

	_, err := agent.Run(context.Background(), &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "weather"}},
		Model:    "mock",
		Tools:    []llm.Tool{tool},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if v, ok := capturedArgs["injected"]; !ok || v != true {
		t.Errorf("expected injected=true in tool args, got %v", capturedArgs)
	}
}

// ---------------------------------------------------------------------------
// Hook — AfterTool can transform the result
// ---------------------------------------------------------------------------

type resultTransformHook struct {
	BaseHook
}

func (h *resultTransformHook) AfterTool(ctx context.Context, call *llm.ToolCall, result string, execErr error) (string, error) {
	if execErr != nil {
		return fmt.Sprintf("transformed-error: %v", execErr), nil
	}
	return fmt.Sprintf("transformed: %s", result), nil
}

func TestHook_AfterTool_TransformsResult(t *testing.T) {
	tool := &MockTool{
		NameValue:   "weather",
		SchemaValue: map[string]any{"type": "object"},
		ExecuteFn: func(ctx context.Context, args map[string]any) (string, error) {
			return "sunny", nil
		},
	}

	agent := &FunctionCallingAgent{
		LLM:   &llm.MockLLM{},
		Hooks: []Hook{&resultTransformHook{}},
	}

	resp, err := agent.Run(context.Background(), &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "weather"}},
		Model:    "mock",
		Tools:    []llm.Tool{tool},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find the tool result message
	found := false
	for _, m := range resp.Messages {
		if m.Role == llm.RoleTool && m.Content == "transformed: sunny" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected transformed tool result in message history")
	}
}

// ---------------------------------------------------------------------------
// Hook — AfterLLM can modify the response
// ---------------------------------------------------------------------------

type prefixResponseHook struct {
	BaseHook
	prefix string
}

func (h *prefixResponseHook) AfterLLM(ctx context.Context, req *llm.ChatRequest, resp *llm.ChatResponse) (*llm.ChatResponse, error) {
	resp.Message.Content = h.prefix + resp.Message.Content
	return resp, nil
}

func TestHook_AfterLLM_TransformsResponse(t *testing.T) {
	agent := &FunctionCallingAgent{
		LLM:   &llm.MockLLM{},
		Hooks: []Hook{&prefixResponseHook{prefix: ">> "}},
	}

	resp, err := agent.Run(context.Background(), &AgentRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Model:    "mock",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Final.Content != ">> hi" {
		t.Errorf("expected %q, got %q", ">> hi", resp.Final.Content)
	}
}
