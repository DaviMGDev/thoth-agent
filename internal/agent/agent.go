package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"my-agent/internal/llm"
)

// Agent orchestrates a tool-using conversation with an LLM.
type Agent interface {
	// Run executes a full conversation with tool calling loop.
	Run(ctx context.Context, req *AgentRequest) (*AgentResponse, error)
	// StreamRun executes the conversation and yields events for each step
	// (LLM tokens, tool call notifications, tool results).
	StreamRun(ctx context.Context, req *AgentRequest) (AgentStream, error)
}

// AgentRequest contains the parameters for an agent execution.
type AgentRequest struct {
	Messages      []llm.Message `json:"messages"`
	Model         string        `json:"model"`
	Tools         []llm.Tool    `json:"-"`
	Temperature   float64       `json:"temperature,omitempty"`
	MaxTokens     int           `json:"max_tokens,omitempty"`
	MaxIterations int           `json:"max_iterations,omitempty"`
	StopSequences []string      `json:"stop_sequences,omitempty"`
}

// AgentResponse contains the result of an agent execution.
type AgentResponse struct {
	Messages []llm.Message  `json:"messages"` // full conversation history
	Final    llm.Message    `json:"final"`    // the last assistant message
	Usage    llm.UsageStats `json:"usage"`    // cumulative token usage
}

// AgentEventType categorises an agent stream event.
type AgentEventType string

const (
	AgentEventToken      AgentEventType = "token"       // LLM streaming token
	AgentEventToolCall   AgentEventType = "tool_call"   // LLM requested a tool
	AgentEventToolResult AgentEventType = "tool_result" // tool returned a result
	AgentEventDone       AgentEventType = "done"        // agent finished
)

// ToolResultEvent carries the result of a tool execution.
type ToolResultEvent struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
	Result    string         `json:"result,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// AgentChunk is one event from a streaming agent execution.
type AgentChunk struct {
	Type       AgentEventType     `json:"type"`
	Content    string             `json:"content,omitempty"`
	Role       llm.MessageRole    `json:"role,omitempty"`
	ToolCall   *llm.ToolCallDelta `json:"tool_call,omitempty"`
	ToolResult *ToolResultEvent   `json:"tool_result,omitempty"`
	Usage      *llm.UsageStats    `json:"usage,omitempty"`
	Done       bool               `json:"done,omitempty"`
	Error      string             `json:"error,omitempty"` // non-empty when the agent encounters a fatal error
}

// AgentStream is a streaming iterator over agent execution events.
// The caller MUST call Close() when finished.
type AgentStream interface {
	Next() bool
	Current() AgentChunk
	Err() error
	Close() error
}

// --- Concrete AgentStream -------------------------------------------------

type agentStream struct {
	ch    chan AgentChunk
	cur   AgentChunk
	err   error
	done  bool
	close sync.Once
}

func (s *agentStream) Next() bool {
	if s.done {
		return false
	}
	chunk, ok := <-s.ch
	if !ok {
		s.done = true
		return false
	}
	s.cur = chunk
	if chunk.Done {
		s.done = true
	}
	return true
}

func (s *agentStream) Current() AgentChunk { return s.cur }

func (s *agentStream) Err() error { return s.err }

func (s *agentStream) Close() error {
	s.close.Do(func() { s.done = true })
	return nil
}

// --- FunctionCallingAgent -------------------------------------------------

// FunctionCallingAgent is a concrete Agent that uses the function-calling
// pattern: call LLM → if tool calls → execute tools in parallel → feed
// results back → repeat until the LLM responds with content.
type FunctionCallingAgent struct {
	LLM        llm.LLM
	Hooks      []Hook        // ordered; nil or empty = no hook overhead
	ChunkDelay time.Duration // when >0, adds delay between stream chunks for testing
}

var _ Agent = (*FunctionCallingAgent)(nil)

// Run executes the tool-calling loop synchronously.
func (a *FunctionCallingAgent) Run(ctx context.Context, req *AgentRequest) (resp *AgentResponse, runErr error) {
	if req == nil {
		return nil, fmt.Errorf("agent: request cannot be nil")
	}
	if a.LLM == nil {
		return nil, fmt.Errorf("agent: LLM is nil")
	}

	// P6: AfterAgent — deferred to wrap all exit paths (success, max-iterations, errors)
	originalReq := req
	defer func() {
		if len(a.Hooks) > 0 {
			resp, _ = applyAfterAgent(ctx, a.Hooks, originalReq, resp, runErr)
		}
	}()

	// P1: BeforeAgent
	if len(a.Hooks) > 0 {
		var hookErr error
		req, hookErr = applyBeforeAgent(ctx, a.Hooks, req)
		if hookErr != nil {
			runErr = hookErr
			return
		}
	}

	maxIter := req.MaxIterations
	if maxIter <= 0 {
		maxIter = 10
	}

	messages := make([]llm.Message, len(req.Messages))
	copy(messages, req.Messages)

	var totalUsage llm.UsageStats

	for iter := 0; iter < maxIter; iter++ {
		select {
		case <-ctx.Done():
			runErr = ctx.Err()
			return
		default:
		}

		chatReq := &llm.ChatRequest{
			Messages:      messages,
			Model:         req.Model,
			Temperature:   req.Temperature,
			MaxTokens:     req.MaxTokens,
			StopSequences: req.StopSequences,
			Tools:         req.Tools,
		}

		// P2: BeforeLLM
		if len(a.Hooks) > 0 {
			var hookErr error
			chatReq, hookErr = applyBeforeLLM(ctx, a.Hooks, chatReq)
			if hookErr != nil {
				runErr = hookErr
				return
			}
		}

		llmResp, err := a.LLM.Chat(ctx, chatReq)
		if err != nil {
			runErr = fmt.Errorf("agent: llm chat iteration %d: %w", iter, err)
			return
		}

		// P3: AfterLLM
		if len(a.Hooks) > 0 {
			var hookErr error
			llmResp, hookErr = applyAfterLLM(ctx, a.Hooks, chatReq, llmResp)
			if hookErr != nil {
				runErr = hookErr
				return
			}
		}

		// Accumulate usage across iterations
		totalUsage.PromptTokens += llmResp.Usage.PromptTokens
		totalUsage.CompletionTokens += llmResp.Usage.CompletionTokens
		totalUsage.TotalTokens += llmResp.Usage.TotalTokens

		if len(llmResp.Message.ToolCalls) > 0 {
			// LLM wants to call tools — append assistant message with tool calls
			messages = append(messages, llmResp.Message)

			// Execute tools in parallel (P4/P5 fire inside executeTools)
			results := a.executeTools(ctx, llmResp.Message.ToolCalls, req.Tools)
			for _, tr := range results {
				messages = append(messages, llm.Message{
					Role:    llm.RoleTool,
					Content: tr,
				})
			}
			continue
		}

		// LLM responded with content — done
		messages = append(messages, llmResp.Message)
		resp = &AgentResponse{
			Messages: messages,
			Final:    llmResp.Message,
			Usage:    totalUsage,
		}
		return
	}

	runErr = fmt.Errorf("agent: max iterations (%d) exceeded without final response", maxIter)
	return
}

// executeTools runs all tool calls in parallel and returns their results as strings.
// Hook errors (P4/P5) are encoded as tool result errors — they do not abort the agent
// because other tool goroutines may already be in-flight.
func (a *FunctionCallingAgent) executeTools(ctx context.Context, toolCalls []llm.ToolCall, tools []llm.Tool) []string {
	results := make([]string, len(toolCalls))
	var wg sync.WaitGroup

	for i, tc := range toolCalls {
		i, tc := i, tc
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Check context before executing
			select {
			case <-ctx.Done():
				results[i] = fmt.Sprintf(`{"error":"context cancelled"}`)
				return
			default:
			}

			// P4: BeforeTool
			currentCall := tc
			if len(a.Hooks) > 0 {
				tcPtr, hookErr := applyBeforeTool(ctx, a.Hooks, &currentCall)
				if hookErr != nil {
					results[i] = fmt.Sprintf(`{"error":"hook: %v"}`, hookErr)
					return
				}
				if tcPtr != nil {
					currentCall = *tcPtr
				}
			}

			var tool llm.Tool
			for _, t := range tools {
				if t.Name() == currentCall.Function.Name {
					tool = t
					break
				}
			}
			if tool == nil {
				results[i] = fmt.Sprintf(`{"error":"tool %q not found"}`, currentCall.Function.Name)
				return
			}

			var args map[string]any
			if currentCall.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(currentCall.Function.Arguments), &args); err != nil {
					results[i] = fmt.Sprintf(`{"error":"failed to parse arguments: %v"}`, err)
					return
				}
			}

			result, execErr := tool.Execute(ctx, args)

			// P5: AfterTool
			if len(a.Hooks) > 0 {
				var hookErr error
				result, hookErr = applyAfterTool(ctx, a.Hooks, &currentCall, result, execErr)
				if hookErr != nil {
					results[i] = fmt.Sprintf(`{"error":"hook: %v"}`, hookErr)
					return
				}
			} else if execErr != nil {
				results[i] = fmt.Sprintf(`{"error":"%v"}`, execErr)
				return
			}
			results[i] = result
		}()
	}

	wg.Wait()
	return results
}

// StreamRun executes the tool-calling loop and streams events.
func (a *FunctionCallingAgent) StreamRun(ctx context.Context, req *AgentRequest) (AgentStream, error) {
	if req == nil {
		return nil, fmt.Errorf("agent: request cannot be nil")
	}
	if a.LLM == nil {
		return nil, fmt.Errorf("agent: LLM is nil")
	}

	ch := make(chan AgentChunk, 64)
	s := &agentStream{ch: ch}

	go a.streamLoop(ctx, req, ch)
	return s, nil
}

func (a *FunctionCallingAgent) streamLoop(ctx context.Context, req *AgentRequest, ch chan AgentChunk) {
	defer close(ch)

	// P6: AfterAgent — deferred to wrap all exit paths
	didAfterAgent := false
	defer func() {
		if len(a.Hooks) > 0 && !didAfterAgent {
			applyAfterAgent(ctx, a.Hooks, req, nil, nil)
		}
	}()

	// P1: BeforeAgent
	if len(a.Hooks) > 0 {
		var hookErr error
		req, hookErr = applyBeforeAgent(ctx, a.Hooks, req)
		if hookErr != nil {
			ch <- AgentChunk{Type: AgentEventDone, Done: true, Error: fmt.Sprintf("before-agent hook: %v", hookErr)}
			return
		}
	}

	maxIter := req.MaxIterations
	if maxIter <= 0 {
		maxIter = 10
	}

	messages := make([]llm.Message, len(req.Messages))
	copy(messages, req.Messages)

	var totalUsage llm.UsageStats

	for iter := 0; iter < maxIter; iter++ {
		select {
		case <-ctx.Done():
			ch <- AgentChunk{Type: AgentEventDone, Done: true}
			return
		default:
		}

		chatReq := &llm.ChatRequest{
			Messages:      messages,
			Model:         req.Model,
			Temperature:   req.Temperature,
			MaxTokens:     req.MaxTokens,
			StopSequences: req.StopSequences,
			Tools:         req.Tools,
		}

		// P2: BeforeLLM
		if len(a.Hooks) > 0 {
			var hookErr error
			chatReq, hookErr = applyBeforeLLM(ctx, a.Hooks, chatReq)
			if hookErr != nil {
				ch <- AgentChunk{Type: AgentEventDone, Done: true, Error: fmt.Sprintf("before-llm hook: %v", hookErr)}
				return
			}
		}

		llmStream, err := a.LLM.StreamChat(ctx, chatReq)
		if err != nil {
			ch <- AgentChunk{Type: AgentEventDone, Done: true, Error: fmt.Sprintf("llm call: %v", err)}
			return
		}

		// Accumulate full response from streaming chunks
		var sb strings.Builder
		var toolCallDeltas []llm.ToolCallDelta
		var finalRole llm.MessageRole
		var finalUsage *llm.UsageStats

		for llmStream.Next() {
			chunk := llmStream.Current()

			// Yield content tokens
			if chunk.Content != "" {
				sb.WriteString(chunk.Content)
				safeSend(ctx, ch, AgentChunk{
					Type:    AgentEventToken,
					Content: chunk.Content,
					Role:    chunk.Role,
				})
			}

			if chunk.Role != "" {
				finalRole = chunk.Role
			}
			if chunk.Usage != nil {
				finalUsage = chunk.Usage
			}
			if len(chunk.ToolCalls) > 0 {
				toolCallDeltas = append(toolCallDeltas, chunk.ToolCalls...)
			}
		}
		llmStream.Close()

		// Build a ChatResponse from accumulated streaming data for AfterLLM hook
		accumulatedResp := &llm.ChatResponse{
			Message: llm.Message{
				Role:    finalRole,
				Content: sb.String(),
			},
			Model:        req.Model,
			FinishReason: llm.FinishReasonStop,
		}
		if finalRole == "" {
			accumulatedResp.Message.Role = llm.RoleAssistant
		}
		if finalUsage != nil {
			accumulatedResp.Usage = *finalUsage
		}

		// P3: AfterLLM
		if len(a.Hooks) > 0 {
			var hookErr error
			accumulatedResp, hookErr = applyAfterLLM(ctx, a.Hooks, chatReq, accumulatedResp)
			if hookErr != nil {
				ch <- AgentChunk{Type: AgentEventDone, Done: true, Error: fmt.Sprintf("after-llm hook: %v", hookErr)}
				return
			}
		}

		// Accumulate usage
		if finalUsage != nil {
			totalUsage.PromptTokens += finalUsage.PromptTokens
			totalUsage.CompletionTokens += finalUsage.CompletionTokens
			totalUsage.TotalTokens += finalUsage.TotalTokens
		}

		if len(toolCallDeltas) > 0 {
			// Build assistant message with tool calls
			assistantMsg := llm.Message{
				Role:    finalRole,
				Content: sb.String(),
			}
			if finalRole == "" {
				assistantMsg.Role = llm.RoleAssistant
			}

			// Convert deltas to ToolCall and yield events
			toolCalls := make([]llm.ToolCall, len(toolCallDeltas))
			for i, d := range toolCallDeltas {
				toolCalls[i] = llm.ToolCall{
					ID: d.ID,
					Function: struct {
						Name      string `json:"name,omitempty"`
						Arguments string `json:"arguments,omitempty"`
					}{
						Name:      d.Function.Name,
						Arguments: d.Function.Arguments,
					},
				}

				// Yield tool call event
				delta := d // copy
				safeSend(ctx, ch, AgentChunk{
					Type:     AgentEventToolCall,
					ToolCall: &delta,
				})
			}

			assistantMsg.ToolCalls = toolCalls
			messages = append(messages, assistantMsg)

			// Execute tools in parallel (P4/P5 fire inside executeTools)
			results := a.executeTools(ctx, toolCalls, req.Tools)

			// Yield tool result events and append to history
			for i, tc := range toolCalls {
				var args map[string]any
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)

				tre := &ToolResultEvent{
					Name:      tc.Function.Name,
					Arguments: args,
					Result:    results[i],
				}

				safeSend(ctx, ch, AgentChunk{
					Type:       AgentEventToolResult,
					ToolResult: tre,
				})

				messages = append(messages, llm.Message{
					Role:    llm.RoleTool,
					Content: results[i],
				})
			}

			// Loop back to call LLM again with tool results
			continue
		}

		// LLM responded with content — done
		assistantMsg := llm.Message{
			Role:    finalRole,
			Content: sb.String(),
		}
		if finalRole == "" {
			assistantMsg.Role = llm.RoleAssistant
		}
		messages = append(messages, assistantMsg)

		totalUsageCopy := totalUsage
		safeSend(ctx, ch, AgentChunk{
			Type:  AgentEventDone,
			Done:  true,
			Usage: &totalUsageCopy,
		})

		// P6: AfterAgent — fire synchronously on success before defer
		if len(a.Hooks) > 0 {
			didAfterAgent = true
			applyAfterAgent(ctx, a.Hooks, req, &AgentResponse{
				Messages: messages,
				Final:    assistantMsg,
				Usage:    totalUsage,
			}, nil)
		}
		return
	}

	// Max iterations exceeded
	if len(a.Hooks) > 0 {
		didAfterAgent = true
		applyAfterAgent(ctx, a.Hooks, req, nil, fmt.Errorf("agent: max iterations (%d) exceeded without final response", maxIter))
	}
	safeSend(ctx, ch, AgentChunk{
		Type: AgentEventDone,
		Done: true,
	})
}

// safeSend sends a chunk to the channel or aborts if context is cancelled.
func safeSend(ctx context.Context, ch chan AgentChunk, chunk AgentChunk) {
	select {
	case ch <- chunk:
	case <-ctx.Done():
	}
}

// --- Agent helpers --------------------------------------------------------

// compile-time interface check
var _ AgentStream = (*agentStream)(nil)

// --- MockAgent ------------------------------------------------------------

// MockAgent is a deterministic Agent implementation for testing.
// It returns pre-configured responses in sequence.
type MockAgent struct {
	Responses []*AgentResponse
	Err       error
	Index     int
	mu        sync.Mutex
}

var _ Agent = (*MockAgent)(nil)

func (m *MockAgent) Run(ctx context.Context, req *AgentRequest) (*AgentResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("agent: request cannot be nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.Err != nil {
		return nil, m.Err
	}
	if m.Index >= len(m.Responses) {
		return nil, fmt.Errorf("mock agent: no more responses (index %d)", m.Index)
	}
	resp := m.Responses[m.Index]
	m.Index++
	return resp, nil
}

func (m *MockAgent) StreamRun(ctx context.Context, req *AgentRequest) (AgentStream, error) {
	// For testing, StreamRun delegates to Run and wraps the result as a stream.
	// This is intentionally simple — tests that need fine-grained streaming
	// control should construct a custom AgentStream.
	resp, err := m.Run(ctx, req)
	if err != nil {
		return nil, err
	}

	ch := make(chan AgentChunk, 4)
	s := &agentStream{ch: ch}

	go func() {
		defer close(ch)
		if resp.Final.Content != "" {
			ch <- AgentChunk{
				Type:    AgentEventToken,
				Content: resp.Final.Content,
				Role:    resp.Final.Role,
			}
		}
		usage := resp.Usage
		ch <- AgentChunk{
			Type:  AgentEventDone,
			Done:  true,
			Usage: &usage,
		}
	}()

	return s, nil
}

// --- MockTool -------------------------------------------------------------

// MockTool is a deterministic Tool implementation for testing.
type MockTool struct {
	NameValue        string
	DescriptionValue string
	SchemaValue      map[string]any
	ExecuteFn        func(ctx context.Context, args map[string]any) (string, error)
}

var _ llm.Tool = (*MockTool)(nil)

func (m *MockTool) Name() string           { return m.NameValue }
func (m *MockTool) Description() string    { return m.DescriptionValue }
func (m *MockTool) Schema() map[string]any { return m.SchemaValue }
func (m *MockTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if m.ExecuteFn == nil {
		return "mock result", nil
	}
	return m.ExecuteFn(ctx, args)
}
