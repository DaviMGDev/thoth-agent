# Roadmap

Planned features and improvements for thoth-agent.

## Session Management

Multi-session conversation management via the `--session` flag in the CLI, with JSON file persistence.

- Load/save conversation history from a session file
- Each session maintains full message history (`[]llm.Message`)
- Sessions stored as JSON files, human-readable and portable
- Supports multi-turn conversations across CLI invocations

## REPL TUI _(moved to `tui` branch)_

> The TUI has been extracted to the `tui` branch for independent development. See that branch for the Bubble Tea-based interactive terminal UI with streaming responses, session management, and keyboard navigation.

## Configurable Agent

Make the agent configurable via external files (YAML/TOML/JSON).

- Model selection, provider settings, system prompts
- Tool registration and enable/disable
- Config file locations: `~/.config/thoth-agent/config.yaml` or `./thoth-agent.yaml`

## Multiple Providers

Support providers beyond Ollama (OpenAI, Anthropic, etc.).

## Permission System

User-defined permission profiles for tool usage. Each tool belongs to a category, and profiles configure whether to always ask, allow, disallow, or hide.

Built-in profiles: `read_only`, `always_ask`, `read_write`, `yolo`.

```json
{
  "read_only": {
    "read": "allow",
    "*": "ask"
  },
  "yolo": {
    "*": "allow"
  },
  "custom": {
    "*": "ask",
    "tools": {
      "bash": "allow"
    }
  }
}
```

## Mixture of Agents

A custom agent that calls multiple LLMs with one main LLM acting as the aggregator. Supports `self_moa` and `multi_moa` patterns.

### Fallback / Dual Mode

Fallback defines a secondary LLM that takes over when the primary cannot continue. For example, using `deepseek-v4-flash` as the main model with `mimo-v2.5` as a fallback for image processing or API outages.

Dual mode is similar but the second model acts as a second opinion rather than a fallback. Exact behavior to be determined.

## Subagents

A tool that can invoke other agent instances. Subagents should be configurable via a file.

## Agent Skills

Support for agent skills with a better implementation than most agents. The key feature is a **skill hinter** — before each LLM call, the harness injects a list of recommended skills based on context (not just the user prompt, but also between agent loop iterations).

## Prompt Templates

Prompt templating system (similar to Pi's approach).

## Observability / Logging

A better observability and logging system. The agent should be able to analyze user usage to suggest creation of new hooks, skills, or prompts.

## Plan Mode

A read-only planning phase where the agent creates `plan*.md` files and tracks tasks with a checkpoint system per turn iteration, keeping the checklist up to date. After planning, an implement mode switches to `yolo` or `read_write` permission profile for execution.

The goal is to keep the harness in control of tracking rather than relying on the model.

## Protocols

- **MCP** — Built-in Model Context Protocol support
- **ACP** — Agent Client Protocol
- **A2A** — Agent-to-Agent Protocol

## LSP Built-in

Language Server Protocol integration.

## Web UI

If ACP is successfully implemented, build a web UI with artifact rendering support (LaTeX, Mermaid, HTML).

## Custom DSL for Orchestration

A custom DSL for agent orchestration. This is a long-term aspiration — requires a mature code agent to be reliable first.

---

_Last updated: 2026-07-13_
