package tui

import (
	"strings"
	"testing"

	"github.com/DaviMGDev/thoth-agent/internal/llm"

	tea "github.com/charmbracelet/bubbletea"
)

// renderView captures the full View() output for a model at a given screen size.
func renderView(width, height int) string {
	m := NewModel()
	m.Width = width
	m.Height = height
	m.Ready = true

	// Simulate a resize event to set up component dimensions
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m = updated.(*Model)

	return m.View()
}

// countLines returns the number of lines in s (split by \n).
func countLines(s string) int {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return 0
	}
	return len(strings.Split(s, "\n"))
}

// maxLineWidth returns the maximum visual line width in s.
func maxLineWidth(s string) int {
	max := 0
	lines := strings.Split(s, "\n")
	for _, l := range lines {
		clean := stripANSI(l)
		runes := []rune(clean)
		if len(runes) > max {
			max = len(runes)
		}
	}
	return max
}

// stripANSI removes ANSI escape sequences from a string.
func stripANSI(s string) string {
	var out []rune
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		if runes[i] == '\x1b' {
			i++
			for i < len(runes) && !(runes[i] >= 'A' && runes[i] <= 'Z') && !(runes[i] >= 'a' && runes[i] <= 'z') {
				i++
			}
			i++
			continue
		}
		out = append(out, runes[i])
		i++
	}
	return string(out)
}

func TestViewFitsWithinScreenSize(t *testing.T) {
	tests := []struct {
		name        string
		screenWidth  int
		screenHeight int
	}{
		{"80x24 (typical terminal)", 80, 24},
		{"100x30 (wide terminal)", 100, 30},
		{"120x40 (large terminal)", 120, 40},
		{"60x15 (small terminal)", 60, 15},
		{"40x10 (minimum terminal)", 40, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := renderView(tt.screenWidth, tt.screenHeight)

			lines := countLines(output)
			width := maxLineWidth(output)

			if lines > tt.screenHeight {
				t.Errorf(
					"View() overflows vertically: got %d lines, screen height = %d (overflow by %d)",
					lines, tt.screenHeight, lines-tt.screenHeight,
				)
			}
			if width > tt.screenWidth {
				for i, l := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
					w := len([]rune(stripANSI(l)))
					if w > tt.screenWidth {
						t.Logf("  line %d: width=%d content=%q", i, w, stripANSI(l))
					}
				}
				t.Errorf(
					"View() overflows horizontally: got max width %d, screen width = %d (overflow by %d)",
					width, tt.screenWidth, width-tt.screenWidth,
				)
			}
		})
	}
}

func TestViewFitsWithMultipleSessions(t *testing.T) {
	m := NewModel()
	m.Sessions = []*Session{
		NewSession(0, "Main Chat"),
		NewSession(1, "Work"),
		NewSession(2, "Random"),
		NewSession(3, "Archived"),
	}
	m.ActiveIdx = 0
	m.Width = 80
	m.Height = 24
	m.Ready = true

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	output := m.View()

	lines := countLines(output)
	width := maxLineWidth(output)

	if lines > 24 {
		t.Errorf("Overflows vertically: %d lines (max 24)", lines)
	}
	if width > 80 {
		t.Errorf("Overflows horizontally: %d cols (max 80)", width)
	}
}

func TestViewWithMessages(t *testing.T) {
	m := NewModel()
	m.Sessions[0].AddMessage(llm.Message{Role: llm.RoleUser, Content: "Hello"})
	m.Sessions[0].AddMessage(llm.Message{Role: llm.RoleAssistant, Content: "Hi there!"})
	m.Width = 80
	m.Height = 24
	m.Ready = true

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	output := m.View()

	lines := countLines(output)
	width := maxLineWidth(output)

	if lines > 24 {
		t.Errorf("Overflows vertically: %d lines (max 24)", lines)
	}
	if width > 80 {
		t.Errorf("Overflows horizontally: %d cols (max 80)", width)
	}

	// Check that messages are visible in output
	clean := stripANSI(output)
	if !strings.Contains(clean, "Hello") {
		t.Errorf("Expected user message 'Hello' to appear in output")
	}
	if !strings.Contains(clean, "Hi there!") {
		t.Errorf("Expected assistant message 'Hi there!' to appear in output")
	}
}

func TestTooSmallTerminal(t *testing.T) {
	m := NewModel()
	m.Width = 30
	m.Height = 5
	m.Ready = true

	output := m.View()
	clean := stripANSI(output)

	if !strings.Contains(clean, "Terminal too small") {
		t.Errorf("Expected 'Terminal too small' warning for tiny terminal")
	}
}
