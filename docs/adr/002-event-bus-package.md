# ADR 002: In-Process Event Bus Package

**Status**: Accepted

**Date**: 2026-07-10

## Context

The `thoth-agent` framework has a strict layered architecture (`llm` ← `agent` ← `cmd/`) with clear integration points where events naturally occur:

1. **LLM layer** — chat requests, responses, streaming chunks, errors.
2. **Agent layer** — tool calls, tool results, iteration boundaries, agent completion.
3. **Application layer** — session lifecycle, user messages, configuration changes.

Currently, these events are internal implementation details. Cross-cutting concerns such as logging, metrics, tracing, audit trails, and plugin hooks require modifying the core agent code. An event bus would decouple event producers from consumers, allowing these concerns to be added as independent subscribers.

The project's design intent mandates **zero external dependencies** (stdlib only). This constrains the event bus to an in-process, pure-Go implementation using channels and goroutines.

## Decision

Add a new `internal/bus/` package providing a lightweight, in-process, pub/sub event bus. The package consists of:

### Interface (`Bus`)

```go
type Bus interface {
    Subscribe(eventType string, handler Handler) (unsubscribe func(), err error)
    Publish(ctx context.Context, event Event) error
    Close() error
}
```

### Key Types

- **`Event`** — a generic payload with a `Type` string and `Payload any` field.
- **`Handler`** — interface with `HandleEvent(context.Context, Event) error`.
- **`HandlerFunc`** — adapter so closures can serve as handlers.

### Concrete Implementation (`InMemoryBus`)

- Backed by a `map[string][]*subscription` protected by `sync.RWMutex`.
- Each subscription has a buffered channel (capacity 64).
- A dedicated goroutine reads from the channel and calls the handler.
- `Publish` fan-out is non-blocking: if a subscriber's buffer is full, the event is dropped for that subscriber (at-most-once delivery).
- `Close` sets a closed flag, closes all channels, and blocks via `sync.WaitGroup` until all in-flight handlers complete.
- Handler panics are recovered so one bad handler never crashes the bus.
- Thread-safe (`RLock` for publish, full `Lock` for mutations).

### Test Double (`MockBus`)

- Records all published events in a slice.
- Each method has an injectable `*Func` override for testing error paths.
- Safe for concurrent use.

### Package Location

The package lives at `internal/bus/`, alongside the existing `internal/llm/` and `internal/agent/` packages. It has **zero internal dependencies** — it imports only from the standard library.

### Wiring Strategy

The bus is added with **no importers in the existing codebase**. Future work will:

1. Add an optional `bus.Bus` field to `FunctionCallingAgent` (nil-safe: if nil, no events are published).
2. Publish agent lifecycle events (`agent.tool_call`, `agent.tool_result`, `agent.iteration`, `agent.done`) in the agent loop.
3. Optionally publish LLM events (`llm.request`, `llm.chunk`, `llm.error`) from providers.
4. Wire subscribers in the application entry point (`cmd/thoth-agent/main.go`) for logging, metrics, etc.

This phased approach keeps changes minimal and reversible.

## Consequences

### Positive

- **Decoupling** — Cross-cutting concerns (logging, metrics, tracing) can be added as subscribers without modifying core agent or LLM code.
- **Testability** — The `Bus` interface allows test doubles; `MockBus` is ready for use in agent tests.
- **Zero dependencies** — Stdlib-only, aligning with the project's design intent.
- **Small surface** — Three methods on `Bus`, one on `Handler`, one concrete event struct.
- **Graceful shutdown** — `Close` guarantees in-flight handlers complete before returning.
- **Resilience** — Handler panics are contained; slow subscribers don't block publishers.
- **ADR-documented** — The decision and its rationale are recorded for future maintainers.

### Negative

- **At-most-once delivery** — Events are dropped if a subscriber's buffer is full. For the use cases anticipated (logging, metrics), this is acceptable.
- **No wildcard subscriptions** — Subscribers must match event types exactly. A glob layer could be added later without breaking the interface.
- **Goroutine per subscriber** — Each subscription adds a goroutine. For hundreds of subscribers this is acceptable; for thousands, a different architecture would be needed.

### Risks

- **Event type string collisions** — Two producers could accidentally use the same event type string. Mitigation: documented convention of dotted hierarchical naming (e.g., `"agent.tool_call"`).
- **Handler blocking indefinitely** — A handler that never returns leaks its goroutine. Mitigation: handlers are expected to be well-behaved; the bus is in-process and synchronous-friendly producers can observe latency. Documented in package doc.
- **Over-engineering** — The project currently has no event bus consumers. Mitigation: the package is small (~150 lines) and costs nothing to carry. If it never gets wired in, it can be removed trivially.

## Rejected Alternatives

1. **External library (Watermill, "asaskevich/EventBus")** — Rejected because it would introduce external dependencies, violating the zero-dependency design intent. These libraries also bring features (persistent queues, wildcard routing, delivery retries) that are unnecessary for an in-process event bus in a small project.

2. **Synchronous delivery** — The publisher blocks until all handlers complete. Rejected because it couples publication latency to the slowest handler, defeating the purpose of decoupling.

3. **Single-goroutine dispatch** — A single goroutine reads from a shared channel and calls all handlers sequentially. Rejected because one slow handler would delay all others; also less idiomatic for fan-out delivery.

4. **Wire the bus into the agent immediately** — Rejected per current requirements. The package is added as infrastructure; integration will happen in a follow-up.

5. **No event bus at all (keep status quo)** — Rejected because the hardcoded `fmt.Print` calls in the agent loop and the inability to add cross-cutting concerns without modifying core code are already limiting the project's evolvability.
