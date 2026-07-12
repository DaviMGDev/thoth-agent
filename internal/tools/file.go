package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"my-agent/internal/llm"
)

var _ llm.Tool = (*ReadFileTool)(nil)

// ReadFileTool reads the contents of a file and returns them.
// Paths are restricted to the current working directory and its subdirectories
// to prevent reading sensitive files outside the project.
type ReadFileTool struct{}

func (t *ReadFileTool) Name() string { return "read_file" }

func (t *ReadFileTool) Description() string {
	return "Read the contents of a file. The path is resolved relative to the current working directory. Returns the full file content as a string. Cannot read files outside the working directory."
}

func (t *ReadFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the file to read, relative to the current working directory.",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	path, ok := args["path"]
	if !ok {
		return "", fmt.Errorf("missing required argument: path")
	}
	pathStr, ok := path.(string)
	if !ok {
		return "", fmt.Errorf("path must be a string, got %T", path)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot determine working directory: %w", err)
	}

	abs, err := filepath.Abs(pathStr)
	if err != nil {
		return "", fmt.Errorf("cannot resolve path %q: %w", pathStr, err)
	}

	// Prevent directory traversal outside CWD
	if !strings.HasPrefix(abs, cwd+string(filepath.Separator)) && abs != cwd {
		return "", fmt.Errorf("path %q is outside the working directory", pathStr)
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("File not found: %s", pathStr), nil
		}
		if os.IsPermission(err) {
			return fmt.Sprintf("Permission denied: %s", pathStr), nil
		}
		return "", fmt.Errorf("read file %q: %w", pathStr, err)
	}

	// Truncate very large files
	const maxLen = 10_000
	if len(data) > maxLen {
		return string(data[:maxLen]) + fmt.Sprintf("\n\n... [truncated, %d more bytes]", len(data)-maxLen), nil
	}

	return string(data), nil
}
