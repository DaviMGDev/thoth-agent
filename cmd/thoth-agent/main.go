package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/DaviMGDev/thoth-agent/internal/agent"
	"github.com/DaviMGDev/thoth-agent/internal/llm"
	"github.com/DaviMGDev/thoth-agent/internal/providers/ollama"
	"github.com/DaviMGDev/thoth-agent/internal/tools"
)

// Version is set at build time via -ldflags.
// Defaults to "dev" when built without ldflags.
var Version = "dev"

// sessionFile is the on-disk format for persistent conversation state.
type sessionFile struct {
	Model    string        `json:"model"`
	Messages []llm.Message `json:"messages"`
}

func main() {
	// --- Flags ---

	var (
		prompt      string
		model       string
		sessionPath string
		verbose     bool
		quiet       bool
		providerURL string
		showVersion bool
	)

	flag.StringVar(&prompt, "prompt", "", "User prompt (reads from stdin if empty)")
	flag.StringVar(&prompt, "p", "", "User prompt (shorthand)")
	flag.StringVar(&model, "model", "gemma4:31b-cloud", "Model name")
	flag.StringVar(&model, "m", "gemma4:31b-cloud", "Model name (shorthand)")
	flag.StringVar(&sessionPath, "session", "", "Session file for persistent context")
	flag.StringVar(&sessionPath, "s", "", "Session file (shorthand)")
	flag.BoolVar(&verbose, "verbose", false, "Show tool calls and iteration info")
	flag.BoolVar(&verbose, "v", false, "Verbose (shorthand)")
	flag.BoolVar(&quiet, "quiet", false, "Only print final assistant response")
	flag.BoolVar(&quiet, "q", false, "Quiet (shorthand)")
	flag.StringVar(&providerURL, "provider-base-url", ollama.DefaultOllamaBaseURL, "Ollama base URL")
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: thoth-agent [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  thoth-agent -p \"What is 2+2?\"\n")
		fmt.Fprintf(os.Stderr, "  echo \"Hello\" | thoth-agent -q\n")
		fmt.Fprintf(os.Stderr, "  thoth-agent -s ./chat.json -p \"Remember my name is Davi\"\n")
		fmt.Fprintf(os.Stderr, "  thoth-agent -s ./chat.json -p \"What's my name?\"\n")
	}

	flag.Parse()

	// --- Version ---

	if showVersion {
		fmt.Println("thoth-agent version", Version)
		os.Exit(0)
	}

	// --- Prompt ---

	if prompt == "" {
		// Read from stdin if available
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			// Data is being piped in
			input, err := io.ReadAll(os.Stdin)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error reading stdin: %v\n", err)
				os.Exit(1)
			}
			prompt = strings.TrimSpace(string(input))
		}
	}

	if prompt == "" {
		flag.Usage()
		os.Exit(1)
	}

	// --- Agent wiring ---

	llmProvider := &ollama.OllamaLLM{BaseURL: providerURL}
	ag := &agent.FunctionCallingAgent{LLM: llmProvider}

	registeredTools := []llm.Tool{
		&tools.GetTimeTool{},
		&tools.ReadFileTool{},
		&tools.BashTool{},
	}

	// --- Session ---

	var messages []llm.Message

	if sessionPath != "" {
		sess, err := loadSession(sessionPath)
		if err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "error loading session: %v\n", err)
			os.Exit(1)
		}
		if sess != nil {
			messages = sess.Messages
			// Override model from session if not explicitly set on CLI.
			// Only override if the user didn't pass a custom model AND
			// the session has a model recorded.
			if sess.Model != "" && !modelFlagExplicit() {
				model = sess.Model
			}
		}
	}

	// Append the new user message.
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: prompt})

	// --- Context with signal handling ---

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// --- Stream agent ---

	req := &agent.AgentRequest{
		Messages: messages,
		Model:    model,
		Tools:    registeredTools,
	}

	stream, err := ag.StreamRun(ctx, req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	var finalContent string
	var hadError bool

	for stream.Next() {
		chunk := stream.Current()

		switch chunk.Type {
		case agent.AgentEventToken:
			if !quiet {
				fmt.Print(chunk.Content)
			}
			finalContent += chunk.Content

		case agent.AgentEventToolCall:
			if verbose && !quiet {
				if chunk.ToolCall != nil {
					fmt.Fprintf(os.Stderr, "\n🔧 %s", chunk.ToolCall.Function.Name)
				}
			}

		case agent.AgentEventToolResult:
			if verbose && !quiet {
				if chunk.ToolResult != nil {
					result := strings.TrimSpace(chunk.ToolResult.Result)
					if len(result) > 500 {
						result = result[:500] + "…"
					}
					if result != "" {
						fmt.Fprintf(os.Stderr, "\n   → %s", result)
					}
					fmt.Fprintf(os.Stderr, "\n")
				}
			}

		case agent.AgentEventIterationStart:
			// Intentionally silent — iteration markers are noise

		case agent.AgentEventDone:
			if quiet {
				// In quiet mode, print the full accumulated response once
				fmt.Println(strings.TrimSpace(finalContent))
			} else {
				fmt.Println()
			}
			if chunk.Error != "" {
				fmt.Fprintf(os.Stderr, "error: %s\n", chunk.Error)
				hadError = true
			}
		}
	}

	if err := stream.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		hadError = true
	}
	stream.Close()

	// --- Save session ---

	if sessionPath != "" && finalContent != "" {
		messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: finalContent})
		sess := sessionFile{
			Model:    model,
			Messages: messages,
		}
		if err := saveSession(sessionPath, &sess); err != nil {
			fmt.Fprintf(os.Stderr, "error saving session: %v\n", err)
			// Don't exit — session save failure shouldn't mask a successful run
		}
	}

	if hadError {
		os.Exit(1)
	}
}

// modelFlagExplicit returns true if the user explicitly passed --model or -m.
// We detect this by checking if the flag was set (flag.Visit).
func modelFlagExplicit() bool {
	explicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "model" || f.Name == "m" {
			explicit = true
		}
	})
	return explicit
}

// loadSession reads a session file from disk.
// Returns nil if the file doesn't exist (first run with a new session).
func loadSession(path string) (*sessionFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var sess sessionFile
	if err := json.NewDecoder(f).Decode(&sess); err != nil {
		return nil, fmt.Errorf("invalid session file %q: %w", path, err)
	}
	return &sess, nil
}

// saveSession writes a session file to disk atomically (write to temp, rename).
func saveSession(path string, sess *sessionFile) error {
	tmpPath := path + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(sess); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	f.Close()

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}
