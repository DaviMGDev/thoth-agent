package agent

import (
	"context"

	"github.com/DaviMGDev/thoth-agent/internal/llm"
)

// Hook intercepts the agent's execution at six defined points.
//
// Implementors embed [BaseHook] and override only the methods they need.
// Hooks are chained in order: Before methods run forward (Hook[0] → Hook[1] → ...),
// After methods run in reverse (Hook[N-1] → ... → Hook[0]).
//
// Returning an error from any hook method aborts the agent. Returning nil for a
// pointer-typed data parameter is a no-op (treated as the input unchanged).
type Hook interface {
	// BeforeAgent fires once when Run/StreamRun begins, after nil checks.
	BeforeAgent(ctx context.Context, req *AgentRequest) (*AgentRequest, error)

	// AfterAgent fires once when Run/StreamRun exits on any path:
	// success, max-iterations exceeded, LLM error, or context cancellation.
	// runErr is the error that the agent would return to the caller (nil on success).
	AfterAgent(ctx context.Context, req *AgentRequest, resp *AgentResponse, runErr error) (*AgentResponse, error)

	// BeforeLLM fires before each LLM call inside the iteration loop.
	// The ChatRequest carries the full message history at this point.
	BeforeLLM(ctx context.Context, req *llm.ChatRequest) (*llm.ChatRequest, error)

	// AfterLLM fires after each LLM call completes (streaming or non-streaming).
	// For streaming, this fires after all chunks have been accumulated.
	AfterLLM(ctx context.Context, req *llm.ChatRequest, resp *llm.ChatResponse) (*llm.ChatResponse, error)

	// BeforeTool fires before each tool.Execute call, per tool call.
	BeforeTool(ctx context.Context, call *llm.ToolCall) (*llm.ToolCall, error)

	// AfterTool fires after each tool.Execute completes.
	// execErr is the error returned by tool.Execute (nil on success).
	// The returned string replaces the tool result in the message history.
	AfterTool(ctx context.Context, call *llm.ToolCall, result string, execErr error) (string, error)
}

// BaseHook provides no-op implementations of every [Hook] method.
// Embed it in your struct and override only the hooks you need.
type BaseHook struct{}

func (BaseHook) BeforeAgent(ctx context.Context, req *AgentRequest) (*AgentRequest, error) {
	return req, nil
}

func (BaseHook) AfterAgent(ctx context.Context, req *AgentRequest, resp *AgentResponse, runErr error) (*AgentResponse, error) {
	return resp, nil
}

func (BaseHook) BeforeLLM(ctx context.Context, req *llm.ChatRequest) (*llm.ChatRequest, error) {
	return req, nil
}

func (BaseHook) AfterLLM(ctx context.Context, req *llm.ChatRequest, resp *llm.ChatResponse) (*llm.ChatResponse, error) {
	return resp, nil
}

func (BaseHook) BeforeTool(ctx context.Context, call *llm.ToolCall) (*llm.ToolCall, error) {
	return call, nil
}

func (BaseHook) AfterTool(ctx context.Context, call *llm.ToolCall, result string, execErr error) (string, error) {
	return result, nil
}
