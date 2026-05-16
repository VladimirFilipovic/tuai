package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/uuid"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role    Role      `json:"role"`
	Content string    `json:"content"`
	At      time.Time `json:"at"`
}

type Session struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Messages  []Message `json:"messages"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// ResumeID is the Claude Code session ID returned from the first
	// `claude -p` invocation. Subsequent turns pass it via --resume so
	// Claude continues the same on-disk conversation.
	ResumeID string `json:"resume_id,omitempty"`
}

type Store struct {
	dir string
}

func NewStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("home dir: %w", err)
	}
	dir := filepath.Join(home, ".config", "claudetui", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	return &Store{dir: dir}, nil
}

func (s *Store) New(name string) *Session {
	if name == "" {
		name = "Session " + time.Now().Format("Jan 2 15:04")
	}
	now := time.Now()
	return &Session{
		ID:        uuid.New().String(),
		Name:      name,
		Messages:  []Message{},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func (s *Store) Save(sess *Session) error {
	sess.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, sess.ID+".json"), data, 0o644)
}

func (s *Store) Load(id string) (*Session, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
	if err != nil {
		return nil, err
	}
	var sess Session
	return &sess, json.Unmarshal(data, &sess)
}

func (s *Store) List() ([]*Session, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var sessions []*Session
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		id := e.Name()[:len(e.Name())-5]
		sess, err := s.Load(id)
		if err != nil {
			continue
		}
		sessions = append(sessions, sess)
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	return sessions, nil
}

func (s *Store) Delete(id string) error {
	return os.Remove(filepath.Join(s.dir, id+".json"))
}
