package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Session struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type Store interface {
	Load() (Session, error)
	Save(Session) error
	Delete() error
}

func DefaultStore() (Store, error) {
	if runtime.GOOS == "darwin" {
		return KeychainStore{}, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return FileStore{Path: filepath.Join(dir, "managed-agents", "session.json")}, nil
}

type KeychainStore struct{}

const keychainService = "managed-agents-cli"

func (KeychainStore) Load() (Session, error) {
	data, err := exec.Command("security", "find-generic-password", "-a", "default", "-s", keychainService, "-w").Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 44 {
			return Session{}, os.ErrNotExist
		}
		return Session{}, fmt.Errorf("read session from Keychain: %w", err)
	}
	var session Session
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &session); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (KeychainStore) Save(session Session) error {
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	// security accepts the value as an argument; it is never logged or written
	// to the repository. macOS stores it as a login-keychain generic password.
	cmd := exec.Command("security", "add-generic-password", "-U", "-a", "default", "-s", keychainService, "-w", string(data))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("save session to Keychain: %w", err)
	}
	return nil
}

func (KeychainStore) Delete() error {
	cmd := exec.Command("security", "delete-generic-password", "-a", "default", "-s", keychainService)
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 44 {
			return os.ErrNotExist
		}
		return fmt.Errorf("delete session from Keychain: %w", err)
	}
	return nil
}

type FileStore struct{ Path string }

func (s FileStore) Load() (Session, error) {
	info, err := os.Stat(s.Path)
	if err != nil {
		return Session{}, err
	}
	if info.Mode().Perm()&0077 != 0 {
		return Session{}, errors.New("session file is readable by other users")
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return Session{}, err
	}
	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s FileStore) Save(session Session) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(s.Path), ".session-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if err := temp.Chmod(0600); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), s.Path)
}

func (s FileStore) Delete() error { return os.Remove(s.Path) }
