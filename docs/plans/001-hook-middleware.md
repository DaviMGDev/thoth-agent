# Plan 001: Hook Middleware for FunctionCallingAgent

**Status**: Planned

**Date**: 2026-07-12

## Design Decisions

| # | Decision | Rationale |
|---|----------|-----------|
| 1 | Single `Hook` interface + `BaseHook` | One `[]Hook` slice to manage. Users embed `BaseHook` and override only the hooks they need. |
| 2 | Before methods: forward order (0→1→2). After methods: reverse order (2→1→0). | Matches HTTP middleware unwinding, `defer` stack, and gRPC interceptor semantics. |
| 3 | `AfterAgent` fires on all exit paths (success, max-iterations, error, context cancel) | Hooks can clean up resources regardless of how the agent exits. Receives `runErr error` for failure paths. |
| 4 | Before hook returning `nil` data pointer = no-op (treat as input unchanged) | Use error return to signal skip/abort. Avoids ambiguity between "no change" and "skip entirely." |
| 5 | `AfterTool` receives the tool execution error as a separate `execErr error` parameter | Cleaner than parsing `{"error":"..."}` from the result string. |

## Hook Interface

New file: `internal/agent/hook.go`

```go
package agent

import (
    "context"
    "github.com/DaviMGDev/thoth-agent/internal/llm"
)

// Hook intercepts the agent's execution at six defined points.
// Implementors embed [BaseHook] and override the methods they need.
type Hook interface {
    BeforeAgent(ctx context.Context, req *AgentRequest) (*AgentRequest, error)
    AfterAgent(ctx context.Context, req *AgentRequest, resp *AgentResponse, runErr error) (*AgentResponse, error)
    BeforeLLM(ctx context.Context, req *llm.ChatRequest) (*llm.ChatRequest, error)
    AfterLLM(ctx context.Context, req *llm.ChatRequest, resp *llm.ChatResponse) (*llm.ChatResponse, error)
    BeforeTool(ctx context.Context, call *llm.ToolCall) (*llm.ToolCall, error)
    AfterTool(ctx context.Context, call *llm.ToolCall, result string, execErr error) (string, error)
}

// BaseHook provides no-op implementations of every [Hook] method.
// Embed it and override only the hooks you need.
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
```

### Return semantics

| Return value | Meaning |
|---|---|
| `(data, nil)` | Continue with `data`. If `data` is the same pointer as input (or `nil` for the pointer types), no change was made. |
| `(nil, nil)` | No-op — treat as if the hook didn't exist. Only valid for pointer-returning methods (`BeforeAgent`, `AfterAgent`, `BeforeLLM`, `AfterLLM`, `BeforeTool`). For `AfterTool` (returns string), `("", nil)` means no change. |
| `(_, err)` | Abort the agent. The agent returns this error to the caller. For `AfterAgent`, the error is logged but the original `runErr` is returned to preserve the root cause. |

## Chaining Helpers

New file: `internal/agent/hook_chain.go`

Functions that apply a `[]Hook` in order (Before) or reverse order (After):

```go
func applyBeforeAgent(hooks []Hook, ctx context.Context, req *AgentRequest) (*AgentRequest, error) {
    for _, h := range hooks {
        var err error
        req, err = h.BeforeAgent(ctx, req)
        if err != nil {
            return nil, err
        }
    }
    return req, nil
}

func applyAfterAgent(hooks []Hook, ctx context.Context, req *AgentRequest, resp *AgentResponse, runErr error) (*AgentResponse, error) {
    for i := len(hooks) - 1; i >= 0; i-- {
        var err error
        resp, err = hooks[i].AfterAgent(ctx, req, resp, runErr)
        if err != nil {
            // Log but don't replace the root error
            return resp, runErr
        }
    }
    return resp, nil
}

// applyBeforeLLM, applyAfterLLM, applyBeforeTool, applyAfterTool — same patterns.
```

## Integration Points in agent.go

### Run() — line 126

Six integration points. Line numbers reference current `agent.go`:

| # | Hook | Line | Trigger |
|---|------|------|---------|
| **P1** | `BeforeAgent` | After nil-checks at line 133, before `maxIter` init | Agent starts |
| **P2** | `BeforeLLM` | After building `chatReq` at line 156, before `a.LLM.Chat()` at line 162 | Each iteration, before LLM call |
| **P3** | `AfterLLM` | After `a.LLM.Chat()` returns at line 162, before tool-call check at line 171 | Each iteration, after LLM responds |
| **P4** | `BeforeTool` | Inside `executeTools()`, before `tool.Execute()` at line 237 | Per tool execution |
| **P5** | `AfterTool` | Inside `executeTools()`, after `tool.Execute()` returns at line 237 | Per tool execution |
| **P6** | `AfterAgent` | Before every `return` statement (success at line 185, max-iterations at line 190, errors at lines 148, 163) | Agent exits |

### executeTools() — line 198

Hooks fire inside the parallel goroutine, per tool:

```go
func (a *FunctionCallingAgent) executeTools(ctx context.Context, toolCalls []llm.ToolCall, tools []llm.Tool) []string {
    results := make([]string, len(toolCalls))
    var wg sync.WaitGroup

    for i, tc := range toolCalls {
        i, tc := i, tc
        wg.Add(1)
        go func() {
            defer wg.Done()

            // P4: BeforeTool
            tcPtr, hookErr := applyBeforeTool(a.Hooks, ctx, &tc)
            if hookErr != nil {
                results[i] = fmt.Sprintf(`{"error":"hook rejected: %v"}`, hookErr)
                return
            }
            if tcPtr == nil {
                results[i] = fmt.Sprintf(`{"error":"tool %q not found"}`, tc.Function.Name)
                return
            }

            // ... existing lookup and execute ...

            result, execErr := tool.Execute(ctx, args)

            // P5: AfterTool
            result, hookErr = applyAfterTool(a.Hooks, ctx, &tc, result, execErr)
            if hookErr != nil {
                results[i] = fmt.Sprintf(`{"error":"hook rejected: %v"}`, hookErr)
                return
            }
            results[i] = result
        }()
    }
    wg.Wait()
    return results
}
```

### StreamRun() — line 250

Same six hooks fire at the same logical seams as `Run()`. Key difference: `AfterLLM` fires after the streaming loop accumulates all deltas (after `llmStream.Close()` at line 330), not after each chunk.

For `StreamRun`, the hook invocations go inside `streamLoop()`:

- **BeforeAgent**: at the top of `streamLoop`, after `maxIter` init (line 272)
- **BeforeLLM**: after `chatReq` is built (line 291), before `a.LLM.StreamChat()` (line 297)
- **AfterLLM**: after `llmStream.Close()` (line 330), before tool-call check (line 339)
- **BeforeTool/AfterTool**: same `executeTools()` calls, hooks fire identically
- **AfterAgent**: at each return point within `streamLoop` (lines 278, 299, 402, 407)

## agent.go Struct Change

```go
type FunctionCallingAgent struct {
    LLM        llm.LLM
    Hooks      []Hook        // ordered; nil or empty = no overhead
    ChunkDelay time.Duration
}
```

All existing fields unchanged. `Hooks` is nil-safe — if nil or empty, the chaining helpers are no-ops (return input unchanged).

## Example Hook: System Message Injector

```go
type TimezoneHook struct {
    agent.BaseHook
    Location string
}

func (h *TimezoneHook) BeforeLLM(ctx context.Context, req *llm.ChatRequest) (*llm.ChatRequest, error) {
    loc, err := time.LoadLocation(h.Location)
    if err != nil {
        return req, nil // skip injection if timezone invalid
    }
    msg := llm.Message{
        Role:    llm.RoleSystem,
        Content: fmt.Sprintf("Current time: %s", time.Now().In(loc).Format(time.RFC3339)),
    }
    req.Messages = append([]llm.Message{msg}, req.Messages...)
    return req, nil
}
```

## Files to Create

| File | Contents |
|------|----------|
| `internal/agent/hook.go` | `Hook` interface + `BaseHook` + doc comments |
| `internal/agent/hook_chain.go` | `applyBefore*` / `applyAfter*` helper functions |
| `internal/agent/hook_test.go` | Tests: chaining order, nil-safety, error propagation, short-circuit |

## Files to Modify

| File | Changes |
|------|---------|
| `internal/agent/agent.go` | Add `Hooks []Hook` field. Insert hook calls at P1-P6 in `Run()`, `streamLoop()`, and `executeTools()`. |

## Files NOT Touched

| File | Reason |
|------|--------|
| `internal/bus/bus.go`, `internal/bus/memory.go`, `internal/bus/bus_test.go` | Keep bus package pristine |
| `internal/llm/llm.go` | Interface and types unchanged |
| `cmd/thoth-agent/main.go` | REPL works without hooks (nil `Hooks` = zero overhead) |

## Verification

- [ ] `go test ./internal/agent/` — all existing tests pass (nil `Hooks` = no behavior change)
- [ ] New test: `TestHook_ChainingOrder` — BeforeAgent fires 0→1→2, AfterAgent fires 2→1→0
- [ ] New test: `TestHook_BeforeLLM_InjectsMessage` — hook adds system message, agent sends it to LLM
- [ ] New test: `TestHook_BeforeLLM_ErrorAborts` — hook returns error, agent returns that error
- [ ] New test: `TestHook_AfterTool_ReceivesError` — tool fails, AfterTool gets `execErr != nil`
- [ ] New test: `TestHook_AfterAgent_OnLLMError` — LLM call fails, AfterAgent fires with `runErr != nil`
- [ ] New test: `TestHook_NilHooks_NoPanic` — zero-value agent with `Hooks == nil` runs correctly
- [ ] `go vet ./internal/agent/` — no issues
- [ ] `gofmt -l ./internal/agent/` — no diff
