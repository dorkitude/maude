package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// Session records the tmux resources Maude owns for a named Claude session.
type Session struct {
	Name               string    `json:"name"`
	SafeName           string    `json:"safe_name"`
	TmuxSession        string    `json:"tmux_session"`
	PaneTarget         string    `json:"pane_target"`
	ClaudeResume       string    `json:"claude_resume,omitempty"`
	LastStatus         string    `json:"last_status,omitempty"`
	LastIntervention   string    `json:"last_intervention,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	LastPromptAt       time.Time `json:"last_prompt_at,omitempty"`
	LastCaptureAt      time.Time `json:"last_capture_at,omitempty"`
	LastCaptureExcerpt string    `json:"last_capture_excerpt,omitempty"`
}

// Store persists session metadata below the configured state directory.
type Store struct {
	Root string
}

func New(root string) Store {
	if root == "" {
		root = "state"
	}
	return Store{Root: root}
}

func SafeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "default"
	}
	safe := unsafeName.ReplaceAllString(name, "-")
	safe = strings.Trim(safe, ".-")
	if safe == "" {
		return "default"
	}
	return safe
}

func (s Store) SessionPath(name string) string {
	return filepath.Join(s.Root, "sessions", SafeName(name)+".json")
}

func (s Store) SaveSession(sess Session) error {
	now := time.Now().UTC()
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = now
	}
	sess.UpdatedAt = now
	if sess.SafeName == "" {
		sess.SafeName = SafeName(sess.Name)
	}
	path := s.SessionPath(sess.Name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create session state dir: %w", err)
	}
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session state: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write session state: %w", err)
	}
	return nil
}

func (s Store) LoadSession(name string) (Session, error) {
	data, err := os.ReadFile(s.SessionPath(name))
	if err != nil {
		return Session{}, err
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return Session{}, fmt.Errorf("decode session state: %w", err)
	}
	return sess, nil
}

func (s Store) DeleteSession(name string) error {
	err := os.Remove(s.SessionPath(name))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s Store) ListSessions() ([]Session, error) {
	dir := filepath.Join(s.Root, "sessions")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sessions := make([]Session, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var sess Session
		if err := json.Unmarshal(data, &sess); err != nil {
			return nil, fmt.Errorf("decode %s: %w", entry.Name(), err)
		}
		sessions = append(sessions, sess)
	}
	return sessions, nil
}
