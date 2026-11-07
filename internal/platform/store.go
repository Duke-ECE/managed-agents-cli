package platform

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return FileStore{Path: filepath.Join(home, ".managed-agents", "session.json")}, nil
}

type FileStore struct{ Path string }

func (s FileStore) Load() (Session, error) {
	info, err := os.Stat(s.Path)
	if err != nil {
		return Session{}, err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
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
