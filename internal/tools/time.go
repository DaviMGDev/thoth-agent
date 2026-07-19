package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/DaviMGDev/thoth-agent/internal/llm"
)

var _ llm.Tool = (*GetTimeTool)(nil)

// GetTimeTool returns the current date and time, optionally in a specified timezone.
type GetTimeTool struct{}

func (t *GetTimeTool) Name() string { return "get_time" }

func (t *GetTimeTool) Description() string {
	return "Return the current date and time. Optionally accepts an IANA timezone name (e.g. America/Sao_Paulo, Europe/London, UTC)."
}

func (t *GetTimeTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"timezone": map[string]any{
				"type":        "string",
				"description": "IANA timezone name (e.g. America/Sao_Paulo, Europe/London, UTC). If omitted, uses the system's local timezone.",
			},
		},
	}
}

func (t *GetTimeTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	loc := time.Local
	if tz, ok := args["timezone"]; ok {
		name, ok := tz.(string)
		if !ok {
			return "", fmt.Errorf("timezone must be a string, got %T", tz)
		}
		loaded, err := time.LoadLocation(name)
		if err != nil {
			return fmt.Sprintf("Unknown timezone %q. Try identifiers like UTC, America/Sao_Paulo, Europe/London.", name), nil
		}
		loc = loaded
	}
	now := time.Now().In(loc)
	return fmt.Sprintf("%s (%s)", now.Format(time.RFC1123Z), loc.String()), nil
}
