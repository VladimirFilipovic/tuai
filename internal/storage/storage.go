package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/google/uuid"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
	// Tool carries the tool name when Role==RoleTool. Content holds the raw
	// streaming-JSON input for that tool call so we can re-render it the
	// same way the live stream did.
	Tool string    `json:"tool,omitempty"`
	At   time.Time `json:"at"`
}

type Session struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Messages  []Message `json:"messages"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Project is the working directory where the session was created.
	// Used to filter the sessions list to the current project.
	Project string `json:"project,omitempty"`

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
	dir := filepath.Join(home, ".config", "tuai", "sessions")
	// 0o700 — session files contain transcript content (sometimes secrets
	// the user pasted into a prompt). Don't let other local users read.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	return &Store{dir: dir}, nil
}

func (s *Store) New(name string) *Session {
	if name == "" {
		name = "Session " + time.Now().Format("Jan 2 15:04")
	}
	now := time.Now()
	// Tag the session with the current working directory so the sessions
	// list can filter by project. Best-effort: empty on error.
	project, _ := os.Getwd()
	return &Session{
		ID:        uuid.New().String(),
		Name:      name,
		Messages:  []Message{},
		CreatedAt: now,
		UpdatedAt: now,
		Project:   project,
	}
}

// validID rejects ids that would escape the sessions dir or smuggle a path
// separator. Real IDs are UUIDs (uuid.New().String()), so the allowed set is
// narrow; this guards future callers that might forward an externally-sourced
// id (imported session, URL param, CLI flag).
var idRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func (s *Store) idPath(id string) (string, error) {
	if id == "" || !idRe.MatchString(id) {
		return "", fmt.Errorf("invalid session id: %q", id)
	}
	return filepath.Join(s.dir, id+".json"), nil
}

func (s *Store) Save(sess *Session) error {
	sess.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	final, err := s.idPath(sess.ID)
	if err != nil {
		return err
	}
	// Write to a sibling .tmp then rename — atomic on POSIX, so a crash
	// mid-flush leaves the prior session intact rather than half-written
	// JSON that Load() will silently drop.
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s *Store) Load(id string) (*Session, error) {
	p, err := s.idPath(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
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
	p, err := s.idPath(id)
	if err != nil {
		return err
	}
	return os.Remove(p)
}
