package platform

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type memoryStore struct {
	session Session
	exists  bool
}

func (s *memoryStore) Load() (Session, error) {
	if !s.exists {
		return Session{}, os.ErrNotExist
	}
	return s.session, nil
}
func (s *memoryStore) Save(session Session) error { s.session = session; s.exists = true; return nil }
func (s *memoryStore) Delete() error              { s.exists = false; return nil }

func TestBrowserLoginUsesPKCEAndStoresSession(t *testing.T) {
	var verifier string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/v1/token" || r.URL.Query().Get("grant_type") != "pkce" {
			t.Errorf("unexpected token path: %s", r.URL)
			http.Error(w, "bad", 400)
			return
		}
		if r.Header.Get("apikey") != "public-test" {
			t.Error("missing public key")
		}
		var request struct {
			Code     string `json:"auth_code"`
			Verifier string `json:"code_verifier"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.Code != "one-time-code" {
			t.Errorf("code = %q", request.Code)
		}
		verifier = request.Verifier
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":3600}`))
	}))
	defer server.Close()
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := free.Addr().(*net.TCPAddr).Port
	free.Close()
	callback := "http://localhost:" + strconv.Itoa(port)
	store := &memoryStore{}
	auth := Auth{Config: Config{SupabaseURL: server.URL, AnonKey: "public-test", CallbackURL: callback}, Store: store}
	var challenge string
	err = auth.LoginBrowser(context.Background(), func(authorize string) error {
		u, err := url.Parse(authorize)
		if err != nil {
			return err
		}
		if u.Path != "/auth/v1/authorize" || u.Query().Get("provider") != "github" || u.Query().Get("redirect_to") != callback || u.Query().Get("code_challenge_method") != "s256" {
			t.Errorf("bad authorize URL: %s", authorize)
		}
		challenge = u.Query().Get("code_challenge")
		go func() {
			response, err := http.Get(callback + "/?code=one-time-code")
			if err != nil {
				t.Error(err)
				return
			}
			response.Body.Close()
		}()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(verifier))
	if challenge != base64.RawURLEncoding.EncodeToString(sum[:]) {
		t.Error("PKCE challenge does not match verifier")
	}
	if !store.exists || store.session.AccessToken != "access" || store.session.RefreshToken != "refresh" {
		t.Fatalf("session not stored: %+v", store.session)
	}
}

func TestAccessTokenRefreshesExpiredSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("grant_type") != "refresh_token" {
			t.Errorf("grant = %s", r.URL)
		}
		w.Write([]byte(`{"access_token":"new-access","refresh_token":"rotated","expires_in":3600}`))
	}))
	defer server.Close()
	store := &memoryStore{session: Session{AccessToken: "old", RefreshToken: "old-refresh", ExpiresAt: time.Now().Add(-time.Minute)}, exists: true}
	auth := Auth{Config: Config{SupabaseURL: server.URL, AnonKey: "public-test"}, Store: store}
	token, err := auth.AccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if token != "new-access" || store.session.RefreshToken != "rotated" {
		t.Fatalf("refresh failed: token=%q session=%+v", token, store.session)
	}
}

func TestFileStoreRejectsLoosePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	store := FileStore{Path: path}
	if err := store.Save(Session{AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil || !strings.Contains(err.Error(), "readable by other users") {
		t.Fatalf("expected permission error, got %v", err)
	}
}

func TestAccessTokenRequiresLogin(t *testing.T) {
	auth := Auth{Store: &memoryStore{}}
	if _, err := auth.AccessToken(context.Background()); err == nil || !strings.Contains(err.Error(), "login") {
		t.Fatalf("expected login error, got %v", err)
	}
}

func TestUpdateUserSendsAuthenticatedRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/auth/v1/user" || r.Header.Get("Authorization") != "Bearer user-token" || r.Header.Get("apikey") != "public-test" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["email"] != "next@example.com" {
			t.Errorf("body = %+v", body)
		}
		w.Write([]byte(`{"email":"next@example.com"}`))
	}))
	defer server.Close()
	auth := Auth{Config: Config{SupabaseURL: server.URL, AnonKey: "public-test"}, Store: &memoryStore{session: Session{AccessToken: "user-token", ExpiresAt: time.Now().Add(time.Hour)}, exists: true}}
	if err := auth.UpdateUser(context.Background(), map[string]string{"email": "next@example.com"}); err != nil {
		t.Fatal(err)
	}
}
