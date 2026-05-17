package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestStore returns a Store rooted in a temporary directory that t cleans
// up automatically. Bypasses NewStore so tests don't touch the user's home.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return &Store{dir: dir}
}

func TestStore_NewAssignsDefaults(t *testing.T) {
	s := newTestStore(t)
	sess := s.New("")
	if sess.ID == "" {
		t.Errorf("New: ID is empty")
	}
	if sess.Name == "" {
		t.Errorf("New: Name is empty")
	}
	if sess.CreatedAt.IsZero() || sess.UpdatedAt.IsZero() {
		t.Errorf("New: timestamps zero")
	}
	if len(sess.Messages) != 0 {
		t.Errorf("New: Messages should be empty, got %d", len(sess.Messages))
	}

	named := s.New("my name")
	if named.Name != "my name" {
		t.Errorf("New: custom name not honored, got %q", named.Name)
	}
}

func TestStore_SaveLoadRoundtrip(t *testing.T) {
	s := newTestStore(t)
	sess := s.New("test")
	sess.Messages = []Message{
		{Role: RoleUser, Content: "hello", At: time.Now()},
		{Role: RoleAssistant, Content: "world", At: time.Now()},
	}
	sess.ResumeID = "claude-session-abc"

	if err := s.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := s.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Name != sess.Name {
		t.Errorf("Name: got %q want %q", loaded.Name, sess.Name)
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("Messages: got %d want 2", len(loaded.Messages))
	}
	if loaded.Messages[0].Content != "hello" || loaded.Messages[1].Content != "world" {
		t.Errorf("Messages content mismatch: %+v", loaded.Messages)
	}
	if loaded.ResumeID != "claude-session-abc" {
		t.Errorf("ResumeID: got %q", loaded.ResumeID)
	}
}

func TestStore_LoadReturnsErrOnCorrupt(t *testing.T) {
	s := newTestStore(t)
	id := "broken"
	if err := os.WriteFile(filepath.Join(s.dir, id+".json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := s.Load(id)
	if err == nil {
		t.Fatalf("Load: want error on corrupt file, got nil")
	}
	if got != nil {
		t.Errorf("Load: want nil session on error, got %+v", got)
	}
}

func TestStore_ListSortsByUpdatedAtDesc(t *testing.T) {
	s := newTestStore(t)
	older := s.New("older")
	older.UpdatedAt = time.Now().Add(-2 * time.Hour)
	if err := writeRaw(s, older); err != nil {
		t.Fatal(err)
	}

	newer := s.New("newer")
	newer.UpdatedAt = time.Now()
	if err := writeRaw(s, newer); err != nil {
		t.Fatal(err)
	}

	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List: want 2, got %d", len(got))
	}
	if got[0].Name != "newer" || got[1].Name != "older" {
		t.Errorf("List ordering: got %q, %q want %q, %q",
			got[0].Name, got[1].Name, "newer", "older")
	}
}

func TestStore_ListIgnoresNonJSON(t *testing.T) {
	s := newTestStore(t)
	if err := os.WriteFile(filepath.Join(s.dir, "readme.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List: non-JSON files should be ignored, got %d entries", len(got))
	}
}

func TestStore_Delete(t *testing.T) {
	s := newTestStore(t)
	sess := s.New("doomed")
	if err := s.Save(sess); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(sess.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Load(sess.ID); err == nil {
		t.Errorf("Load: expected error after delete")
	}
}

// writeRaw bypasses Save's UpdatedAt bump so tests can pin timestamps.
func writeRaw(s *Store, sess *Session) error {
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, sess.ID+".json"), data, 0o644)
}
