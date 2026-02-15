package authstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultPath_UsesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}

	want := filepath.Join(home, ".ppw", "tokens.json")
	if got != want {
		t.Fatalf("DefaultPath = %q, want %q", got, want)
	}
}

func TestNew_ExpandsTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	s, err := New("~/.ppw/custom.json")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	want := filepath.Join(home, ".ppw", "custom.json")
	if s.Path() != want {
		t.Fatalf("Path = %q, want %q", s.Path(), want)
	}
}

func TestLoad_NotFound(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "tokens.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = s.Load()
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load error = %v, want ErrNotFound", err)
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tokens.json")
	s, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	exp := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	want := StoreData{
		Meta: MetaTokens{
			UserID:          "26600065222929008",
			UserAccessToken: "meta-user-token",
			UserExpiresAt:   exp,
		},
		Threads: ThreadsTokens{
			UserID:      "25343690875308127",
			AccessToken: "threads-access-token",
			ExpiresAt:   exp,
		},
	}

	if err := s.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.Version != SchemaVersion {
		t.Fatalf("version = %d, want %d", got.Version, SchemaVersion)
	}
	if got.Meta.UserAccessToken != want.Meta.UserAccessToken {
		t.Fatalf("meta token = %q, want %q", got.Meta.UserAccessToken, want.Meta.UserAccessToken)
	}
	if got.Threads.AccessToken != want.Threads.AccessToken {
		t.Fatalf("threads token = %q, want %q", got.Threads.AccessToken, want.Threads.AccessToken)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatalf("updated_at should be set")
	}
}

func TestSave_EnforcesPermissions(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tokens.json")

	s, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Save(StoreData{}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	fileInfo, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file perm = %o, want 600", perm)
	}

	dirInfo, err := os.Stat(filepath.Dir(p))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Fatalf("dir perm = %o, want 700", perm)
	}
}

func TestUpdate_CreateAndMutate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tokens.json")
	s, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := s.Update(func(in StoreData) (StoreData, error) {
		in.Meta.UserAccessToken = "first"
		return in, nil
	}); err != nil {
		t.Fatalf("Update(create): %v", err)
	}

	if err := s.Update(func(in StoreData) (StoreData, error) {
		if in.Meta.UserAccessToken != "first" {
			t.Fatalf("unexpected pre-update token = %q", in.Meta.UserAccessToken)
		}
		in.Meta.UserAccessToken = "second"
		return in, nil
	}); err != nil {
		t.Fatalf("Update(mutate): %v", err)
	}

	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Meta.UserAccessToken != "second" {
		t.Fatalf("meta token = %q, want %q", got.Meta.UserAccessToken, "second")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tokens.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte("{invalid"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	s, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = s.Load()
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestUpdate_NilFn(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "tokens.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Update(nil); err == nil {
		t.Fatal("expected error for nil update function")
	}
}
