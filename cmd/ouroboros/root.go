package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/DaviMGDev/ouroboros/internal/agent"
	"github.com/DaviMGDev/ouroboros/internal/llm"
	"github.com/DaviMGDev/ouroboros/internal/providers/ollama"
	"github.com/DaviMGDev/ouroboros/internal/tools"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// newRootCmd builds the root "oro" command with all flags, Viper config
// bindings, and the full agent execution pipeline.
func newRootCmd() *cobra.Command {
	var (
		prompt      string
		model       string
		sessionPath string
		verbose     bool
		quiet       bool
		providerURL string
		showVersion bool
		configPath  string
	)

	cmd := &cobra.Command{
		Use:   "oro",
		Short: "Ouroboros — an LLM agent framework CLI",
		Long: `Ouroboros is a Go-based LLM agent framework with tool-calling,
streaming chat, and persistent multi-turn conversations.

Run a single prompt:
  oro -p "What is 2+2?"

Pipe from stdin:
  echo "Hello" | oro -q

Persistent multi-turn conversation:
  oro -s ./chat.json -p "My name is Davi"
  oro -s ./chat.json -p "What's my name?"

Verbose mode (see tool calls):
  oro -v -p "List files in /tmp"`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return initConfig(cmd, configPath)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// --- Version ---
			if showVersion {
				fmt.Println("oro version", Version)
				return nil
			}

			// --- Prompt ---
			if prompt == "" {
				// Read from stdin if available
				stat, _ := os.Stdin.Stat()
				if (stat.Mode() & os.ModeCharDevice) == 0 {
					input, err := io.ReadAll(os.Stdin)
					if err != nil {
						return fmt.Errorf("reading stdin: %w", err)
					}
					prompt = strings.TrimSpace(string(input))
				}
			}

			if prompt == "" {
				// Show help and exit. Cobra doesn't show usage for a
				// missing argument by default, so we print it manually.
				cmd.Help()
				fmt.Fprintln(os.Stderr)
				return fmt.Errorf("no prompt provided (use -p or pipe from stdin)")
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
					return fmt.Errorf("loading session: %w", err)
				}
				if sess != nil {
					messages = sess.Messages
					// Override model from session if not explicitly set on CLI.
					if sess.Model != "" && !cmd.Flags().Changed("model") {
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
				return fmt.Errorf("starting agent: %w", err)
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
					// Intentionally silent

				case agent.AgentEventDone:
					if quiet {
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
				return fmt.Errorf("stream: %w", err)
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
					// session save failure doesn't mask a successful run
				}
			}

			if hadError {
				return fmt.Errorf("agent completed with errors")
			}
			return nil
		},
	}

	// --- Flags ---
	cmd.Flags().StringVarP(&prompt, "prompt", "p", "", "User prompt (reads from stdin if empty)")
	cmd.Flags().StringVarP(&model, "model", "m", "gemma4:31b-cloud", "Model name")
	cmd.Flags().StringVarP(&sessionPath, "session", "s", "", "Session file for persistent context")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show tool calls and iteration info")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Only print final assistant response")
	cmd.Flags().StringVar(&providerURL, "provider-base-url", ollama.DefaultOllamaBaseURL, "Ollama base URL")
	cmd.Flags().BoolVar(&showVersion, "version", false, "Print version and exit")
	cmd.Flags().StringVar(&configPath, "config", "", "Config file path (default: ./ouroboros.yaml, ~/.config/ouroboros/config.yaml)")

	// Suppress Cobra's default error handling — we handle it in main().
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	return cmd
}

// ---------------------------------------------------------------------------
// Viper config resolution
// ---------------------------------------------------------------------------

// initConfig reads the optional config file and applies values to flags
// that weren't explicitly set on the command line. Precedence:
//
//	CLI flags > env vars > config file > defaults
func initConfig(cmd *cobra.Command, configPath string) error {
	viper.SetConfigName("ouroboros")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("$HOME/.config/ouroboros")

	if configPath != "" {
		viper.SetConfigFile(configPath)
	}

	// Bind env vars with ORO_ prefix (e.g. ORO_MODEL).
	viper.SetEnvPrefix("oro")
	viper.AutomaticEnv()

	// Read config file (optional — fine if not found).
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			fmt.Fprintf(os.Stderr, "warning: config: %v\n", err)
		}
		// If the user explicitly passed --config and the file doesn't exist,
		// that's an error.
		if configPath != "" {
			if _, isNotFound := err.(viper.ConfigFileNotFoundError); isNotFound {
				return fmt.Errorf("config file not found: %s", configPath)
			}
		}
	}

	// Apply config values to flags only when the flag wasn't explicitly set.
	//
	// model:    from "model" key
	// provider-base-url: from "provider.base_url" key
	if !cmd.Flags().Changed("model") && viper.IsSet("model") {
		if v := viper.GetString("model"); v != "" {
			cmd.Flags().Set("model", v)
		}
	}
	if !cmd.Flags().Changed("provider-base-url") && viper.IsSet("provider.base_url") {
		if v := viper.GetString("provider.base_url"); v != "" {
			cmd.Flags().Set("provider-base-url", v)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Session persistence
// ---------------------------------------------------------------------------

// sessionFile is the on-disk format for persistent conversation state.
type sessionFile struct {
	Model    string        `json:"model"`
	Messages []llm.Message `json:"messages"`
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
