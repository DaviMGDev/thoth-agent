package agent

import (
	"context"

	"github.com/DaviMGDev/thoth-agent/internal/llm"
)

// applyBeforeAgent runs all BeforeAgent hooks in forward order.
// Returns the (possibly modified) request. A nil return value from a hook is
// treated as "no change" and the input request is used instead.
func applyBeforeAgent(ctx context.Context, hooks []Hook, req *AgentRequest) (*AgentRequest, error) {
	for _, h := range hooks {
		out, err := h.BeforeAgent(ctx, req)
		if err != nil {
			return nil, err
		}
		if out != nil {
			req = out
		}
	}
	return req, nil
}

// applyAfterAgent runs all AfterAgent hooks in reverse order.
// runErr is the error the agent would return (nil on success).
func applyAfterAgent(ctx context.Context, hooks []Hook, req *AgentRequest, resp *AgentResponse, runErr error) (*AgentResponse, error) {
	for i := len(hooks) - 1; i >= 0; i-- {
		out, err := hooks[i].AfterAgent(ctx, req, resp, runErr)
		if err != nil {
			return nil, err
		}
		if out != nil {
			resp = out
		}
	}
	return resp, nil
}

// applyBeforeLLM runs all BeforeLLM hooks in forward order.
func applyBeforeLLM(ctx context.Context, hooks []Hook, req *llm.ChatRequest) (*llm.ChatRequest, error) {
	for _, h := range hooks {
		out, err := h.BeforeLLM(ctx, req)
		if err != nil {
			return nil, err
		}
		if out != nil {
			req = out
		}
	}
	return req, nil
}

// applyAfterLLM runs all AfterLLM hooks in reverse order.
func applyAfterLLM(ctx context.Context, hooks []Hook, req *llm.ChatRequest, resp *llm.ChatResponse) (*llm.ChatResponse, error) {
	for i := len(hooks) - 1; i >= 0; i-- {
		out, err := hooks[i].AfterLLM(ctx, req, resp)
		if err != nil {
			return nil, err
		}
		if out != nil {
			resp = out
		}
	}
	return resp, nil
}

// applyBeforeTool runs all BeforeTool hooks in forward order.
func applyBeforeTool(ctx context.Context, hooks []Hook, call *llm.ToolCall) (*llm.ToolCall, error) {
	for _, h := range hooks {
		out, err := h.BeforeTool(ctx, call)
		if err != nil {
			return nil, err
		}
		if out != nil {
			call = out
		}
	}
	return call, nil
}

// applyAfterTool runs all AfterTool hooks in reverse order.
// execErr is the error from tool.Execute (nil on success).
func applyAfterTool(ctx context.Context, hooks []Hook, call *llm.ToolCall, result string, execErr error) (string, error) {
	for i := len(hooks) - 1; i >= 0; i-- {
		var err error
		result, err = hooks[i].AfterTool(ctx, call, result, execErr)
		if err != nil {
			return "", err
		}
	}
	return result, nil
}
