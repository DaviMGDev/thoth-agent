# ADR 001: Restructure from Flat `package main` to Module Layout

**Status**: Accepted

**Date**: 2026-07-09

## Context

The `thoth-agent` project started as a prototype with all Go source files in a single flat `package main` directory. This worked well during early development but created several problems as the project grew:

1. **No package boundaries** — All types (LLM interfaces, agent logic, provider implementations) lived in the same namespace, making it impossible to enforce encapsulation or import cycle prevention at the compiler level.

2. **Unclear ownership** — There was no way to distinguish which types belonged to the core framework vs. provider implementations vs. the REPL entry point.

3. **Difficult to test in isolation** — Tests had access to all internal types, making it easy to accidentally couple test logic to implementation details.

4. **Poor discoverability** — New contributors had to read through all files to understand the module boundaries since there were none.

5. **Not consumable as a library** — Any external Go module importing this project would get the entire flat namespace including provider implementations and main(), making it unusable as a framework dependency.

## Decision

Restructure the project into a standard Go module layout with subpackages:

```
thoth-agent/
├── internal/
│   ├── llm/                  # LLM interface + shared types + MockLLM
│   ├── agent/                # Agent interface + FunctionCallingAgent + MockAgent + MockTool
│   └── providers/
│       └── ollama/           # OllamaLLM provider implementation
├── cmd/
│   └── thoth-agent/             # main() — REPL entry point
├── docs/
│   └── adr/                  # Architecture Decision Records
├── .golangci.yml
├── Makefile
├── go.mod
└── README.md
```

Key decisions within this structure:

- **`internal/`**: All packages are under `internal/` to signal that they are not stable public APIs. This prevents external consumers from importing them and allows us to refactor freely.

- **`internal/llm/` (package `llm`)**: Contains the `LLM` interface, all shared types (`Message`, `ChatRequest`, `ChatResponse`, `UsageStats`, `FinishReason`, `ChatStream`, `ChatChunk`, `ToolCallDelta`, `ToolCall`), the `Tool` interface, and the `MockLLM`/`MockChatStream` test doubles. This package has zero internal dependencies — it is the foundation.

- **`internal/agent/` (package `agent`)**: Contains the `Agent` interface, `FunctionCallingAgent` (concrete tool-calling loop with parallel execution), `AgentStream`, `MockAgent`, and `MockTool`. Imports only from `internal/llm`.

- **`internal/providers/ollama/` (package `ollama`)**: Contains `OllamaLLM` and all Ollama-specific wire types. Imports only from `internal/llm`.

- **`cmd/thoth-agent/` (package `main`)**: Contains the REPL entry point. Imports from both `internal/agent` and `internal/providers/ollama`. This is the only package with a `main()` function.

- **Test packages**: Tests may use either the same package (e.g., `package agent`, `package ollama`, `package bus`) for access to unexported helpers, or an external test package (`package llm_test`) for black-box testing. Same-package tests access internal types like `ollamaChatRequest` and `agentStream`; external test packages enforce interface-only access.

## Consequences

### Positive

- **Clear boundaries** — Import cycles are caught at compile time. The dependency graph is strict: `llm` ← `agent`, `llm` ← `ollama`, `agent`+`ollama` ← `cmd/thoth-agent`.

- **Encapsulation** — Unexported types (e.g., `ollamaChatRequest`, `ollamaChatStream`, `agentStream`) are now truly private to their package.

- **Isolated testing** — Each package can be tested independently. Tests no longer have unrestricted access to all types.

- **Consumable as a library** — Although everything is in `internal/` (and thus not importable by external modules), the structure makes it straightforward to promote parts to public packages in the future.

- **Standard layout** — Follows Go community conventions, making the project immediately familiar to new contributors.

### Negative

- **More verbose imports** — Code that previously used `Message` directly must now use `llm.Message` or `agent.AgentRequest`.

- **Temporary erosion of zero-value usability** — Users who previously imported `package main` must now adjust import paths. Since the project is in `internal/`, this only affects internal consumers anyway.

- **Split test helpers** — The `multiToolMockLLM` test helper in `agent_test.go` now lives alongside the agent tests rather than being globally accessible.

### Risks

- **Over-engineering** — The current flat structure was viable for a 7-file project. The new layout adds overhead (package names, imports, directory navigation). Mitigation: the overhead is standard Go practice and will pay off as the project grows.

- **Breaking local scripts** — Any scripts that ran `go run .` must now use `go run ./cmd/thoth-agent/`. Mitigation: the Makefile and README have been updated.

## Rejected Alternatives

1. **Keep flat `package main`** — Rejected because it prevents clean separation of concerns and makes the project unusable as a library dependency.

2. **Single `pkg/` directory with all files** — Rejected because it provides no meaningful separation; it just moves the flat namespace to a different directory.

3. **Public packages (no `internal/`)** — Rejected because the project is in early prototyping and public packages would create a backward-compatibility burden. The `internal/` convention lets us refactor freely.

4. **Separate Go module for providers** — Rejected as premature. A separate module for providers (e.g., `thoth-agent-provider-ollama`) would add release overhead. This can be done later when the provider surface stabilizes.
