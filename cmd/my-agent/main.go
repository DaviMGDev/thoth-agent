package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"my-agent/internal/agent"
	"my-agent/internal/llm"
	"my-agent/internal/providers/ollama"
	"my-agent/internal/tools"
)

// Version is set at build time via -ldflags.
// Defaults to "dev" when built without ldflags.
var Version = "dev"

func main() {
	// Support both -version and --version (Go's flag package handles -- as
	// end-of-flags, so we check os.Args directly for the double-dash form).
	for _, arg := range os.Args[1:] {
		if arg == "-version" || arg == "--version" {
			fmt.Println("my-agent version", Version)
			os.Exit(0)
		}
	}
	flag.Parse()

	llmProvider := &ollama.OllamaLLM{}
	ag := &agent.FunctionCallingAgent{LLM: llmProvider}
	model := "ministral-3:3b-cloud"

	registeredTools := []llm.Tool{
		&tools.GetTimeTool{},
		&tools.ReadFileTool{},
	}

	var messages []llm.Message
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("Chat with Ollama (type /exit to quit, /clear to reset)")
	fmt.Println()

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())

		switch {
		case input == "/exit":
			fmt.Println("goodbye")
			return
		case input == "/clear":
			messages = nil
			fmt.Println("history cleared")
			continue
		case input == "":
			continue
		}

		messages = append(messages, llm.Message{Role: llm.RoleUser, Content: input})

		req := &agent.AgentRequest{
			Messages: messages,
			Model:    model,
			Tools:    registeredTools,
		}

		stream, err := ag.StreamRun(context.Background(), req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			fmt.Fprintln(os.Stderr, "(is Ollama running? try: ollama serve)")
			// Remove the last user message so it can be retried
			messages = messages[:len(messages)-1]
			continue
		}

		var reply strings.Builder
		gotAny := false
		for stream.Next() {
			gotAny = true
			chunk := stream.Current()
			switch chunk.Type {
			case agent.AgentEventToken:
				fmt.Print(chunk.Content)
				reply.WriteString(chunk.Content)
			case agent.AgentEventToolCall:
				if chunk.ToolCall != nil {
					fmt.Printf("\n🔧 calling %s(%s)\n", chunk.ToolCall.Function.Name, chunk.ToolCall.Function.Arguments)
				}
			case agent.AgentEventToolResult:
				if chunk.ToolResult != nil {
					// Truncate long results for readability
					result := chunk.ToolResult.Result
					if len(result) > 200 {
						result = result[:200] + "..."
					}
					fmt.Printf("✅ %s → %s\n", chunk.ToolResult.Name, strings.TrimSpace(result))
				}
			case agent.AgentEventDone:
				if chunk.Error != "" {
					fmt.Fprintf(os.Stderr, "\nagent error: %s\n", chunk.Error)
				}
			}
		}
		if err := stream.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "\nstream error: %v\n", err)
			messages = messages[:len(messages)-1]
			continue
		}
		stream.Close()

		if !gotAny {
			fmt.Fprintln(os.Stderr, "(no response — is Ollama running? try: ollama serve)")
			messages = messages[:len(messages)-1]
			continue
		}

		messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: reply.String()})
		fmt.Println()
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "read error: %v\n", err)
		os.Exit(1)
	}
}
