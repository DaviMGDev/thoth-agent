package tui

import "github.com/DaviMGDev/thoth-agent/internal/llm"

// MaxMessagesPerSession is the maximum number of messages retained in a
// session's history. When exceeded, the oldest 25% are pruned to avoid
// unbounded memory growth.
const MaxMessagesPerSession = 500

// MaxInputHistoryEntries is the maximum number of past user inputs retained
// for recall via Up/Down arrow navigation.
const MaxInputHistoryEntries = 50

// Session represents a single chat conversation with independent history.
type Session struct {
	ID       int
	Name     string
	Messages []llm.Message

	// Input history for Up/Down recall
	InputHistory []string // ring buffer, newest last
	HistoryPos   int      // -1 = not browsing, 0..len-1 = browsing
	PendingInput string   // saved in-progress text when user starts browsing
}

// NewSession creates a session with the given ID and display name.
func NewSession(id int, name string) *Session {
	return &Session{
		ID:           id,
		Name:         name,
		Messages:     make([]llm.Message, 0),
		InputHistory: make([]string, 0, MaxInputHistoryEntries+1),
		HistoryPos:   -1,
	}
}

// AddMessage appends a message to the session's conversation history.
// If the history exceeds MaxMessagesPerSession, the oldest 25% are pruned.
func (s *Session) AddMessage(msg llm.Message) {
	s.Messages = append(s.Messages, msg)
	if len(s.Messages) > MaxMessagesPerSession {
		prune := len(s.Messages) - MaxMessagesPerSession + (MaxMessagesPerSession / 4)
		s.Messages = s.Messages[prune:]
	}
}

// AddToHistory appends an input to the history buffer, evicting the oldest
// entry if the maximum is exceeded. Consecutive duplicates are skipped.
func (s *Session) AddToHistory(input string) {
	if len(s.InputHistory) > 0 && s.InputHistory[len(s.InputHistory)-1] == input {
		return // skip duplicate of last entry
	}
	s.InputHistory = append(s.InputHistory, input)
	if len(s.InputHistory) > MaxInputHistoryEntries {
		s.InputHistory = s.InputHistory[1:] // evict oldest
	}
	s.HistoryPos = -1
	s.PendingInput = ""
}
