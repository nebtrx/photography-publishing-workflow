// Package authstore provides secure persistent storage for OAuth token material.
//
// The store uses:
//   - default location: ~/.ppw/tokens.json (override via PPW_TOKEN_STORE)
//   - atomic writes (temp file + rename)
//   - restrictive permissions (dir 0700, file 0600)
//   - process and file locking for safe concurrent updates
package authstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// SchemaVersion is the current token store schema version.
	SchemaVersion = 1

	defaultDirName  = ".ppw"
	defaultFileName = "tokens.json"
)

// ErrNotFound indicates the token store file does not yet exist.
var ErrNotFound = errors.New("token store not found")

// StoreData is the persisted token-store document.
type StoreData struct {
	Version   int           `json:"version"`
	UpdatedAt time.Time     `json:"updated_at,omitempty"`
	Meta      MetaTokens    `json:"meta,omitempty"`
	Threads   ThreadsTokens `json:"threads,omitempty"`
}

// MetaTokens holds Meta user/page token state.
type MetaTokens struct {
	UserID          string    `json:"user_id,omitempty"`
	Scopes          []string  `json:"scopes,omitempty"`
	UserAccessToken string    `json:"user_access_token,omitempty"`
	UserExpiresAt   time.Time `json:"user_expires_at,omitempty"`
	PageAccessToken string    `json:"page_access_token,omitempty"`
	PageDerivedAt   time.Time `json:"page_derived_at,omitempty"`
}

// ThreadsTokens holds Threads OAuth token state.
type ThreadsTokens struct {
	UserID       string    `json:"user_id,omitempty"`
	Scopes       []string  `json:"scopes,omitempty"`
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
}

// Store reads/writes token material from disk.
type Store struct {
	path string
	mu   sync.Mutex
}

// New creates a token store.
// If path is empty, DefaultPath() is used.
func New(path string) (*Store, error) {
	var err error
	if strings.TrimSpace(path) == "" {
		path, err = DefaultPath()
		if err != nil {
			return nil, err
		}
	} else {
		path, err = expandPath(path)
		if err != nil {
			return nil, err
		}
	}
	return &Store{path: path}, nil
}

// DefaultPath resolves the token store path.
// Priority:
// 1) PPW_TOKEN_STORE if set
// 2) ~/.ppw/tokens.json
func DefaultPath() (string, error) {
	if v := strings.TrimSpace(os.Getenv("PPW_TOKEN_STORE")); v != "" {
		return expandPath(v)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, defaultDirName, defaultFileName), nil
}

// Path returns the fully resolved store path.
func (s *Store) Path() string {
	return s.path
}

// Load reads and parses token data.
func (s *Store) Load() (StoreData, error) {
	var out StoreData
	err := s.withLock(func() error {
		data, err := s.loadUnlocked()
		if err != nil {
			return err
		}
		out = data
		return nil
	})
	return out, err
}

// Save writes token data atomically.
func (s *Store) Save(data StoreData) error {
	return s.withLock(func() error {
		if err := ensureDir(filepath.Dir(s.path)); err != nil {
			return err
		}
		if data.Version == 0 {
			data.Version = SchemaVersion
		}
		data.UpdatedAt = time.Now().UTC()
		return writeAtomic(s.path, data)
	})
}

// Update performs a read-modify-write under the same lock.
func (s *Store) Update(fn func(StoreData) (StoreData, error)) error {
	if fn == nil {
		return fmt.Errorf("update function is required")
	}

	return s.withLock(func() error {
		current, err := s.loadUnlocked()
		if err != nil {
			if !errors.Is(err, ErrNotFound) {
				return err
			}
			current = StoreData{Version: SchemaVersion}
		}

		next, err := fn(current)
		if err != nil {
			return err
		}

		if next.Version == 0 {
			next.Version = SchemaVersion
		}
		next.UpdatedAt = time.Now().UTC()

		if err := ensureDir(filepath.Dir(s.path)); err != nil {
			return err
		}
		return writeAtomic(s.path, next)
	})
}

func (s *Store) loadUnlocked() (StoreData, error) {
	var out StoreData

	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, ErrNotFound
		}
		return out, fmt.Errorf("read token store: %w", err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("parse token store: %w", err)
	}
	if out.Version == 0 {
		out.Version = SchemaVersion
	}
	if err := ensureFileMode(s.path, 0o600); err != nil {
		return out, err
	}
	return out, nil
}

func (s *Store) withLock(fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Dir(s.path)
	if err := ensureDir(dir); err != nil {
		return err
	}

	lockPath := s.path + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open lock file %s: %w", lockPath, err)
	}
	defer lockFile.Close()

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire lock %s: %w", lockPath, err)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

	return fn()
}

func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create token-store dir %s: %w", dir, err)
	}
	return ensureFileMode(dir, 0o700)
}

func ensureFileMode(path string, want os.FileMode) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	have := info.Mode().Perm()
	if have == want {
		return nil
	}
	if err := os.Chmod(path, want); err != nil {
		return fmt.Errorf("set permissions %s to %o: %w", path, want, err)
	}
	return nil
}

func writeAtomic(path string, data StoreData) error {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal token store: %w", err)
	}
	b = append(b, '\n')

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tokens-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp token file: %w", err)
	}
	tmpPath := tmp.Name()

	cleanup := func() {
		tmp.Close()
		_ = os.Remove(tmpPath)
	}

	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("set temp token file permissions: %w", err)
	}
	if _, err := tmp.Write(b); err != nil {
		cleanup()
		return fmt.Errorf("write temp token file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync temp token file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp token file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace token store: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("set token store permissions: %w", err)
	}

	return nil
}

func expandPath(path string) (string, error) {
	p := strings.TrimSpace(path)
	if p == "" {
		return "", fmt.Errorf("path is empty")
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if p == "~" {
			return home, nil
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~/"))
	}
	return filepath.Clean(p), nil
}
