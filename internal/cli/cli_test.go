package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Duke-ECE/managed-agents-cli/internal/platform"
)

type testStore struct{ session platform.Session }

func (s testStore) Load() (platform.Session, error) { return s.session, nil }
func (s testStore) Save(platform.Session) error     { return nil }
func (s testStore) Delete() error                   { return nil }

func TestWhoamiUsesUserToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/me" || r.Header.Get("Authorization") != "Bearer user-token" {
			t.Errorf("unexpected request: %s %s", r.URL, r.Header.Get("Authorization"))
		}
		w.Write([]byte(`{"email":"user@example.com","is_admin":false}`))
	}))
	defer server.Close()
	auth := platform.Auth{Config: platform.Config{APIURL: server.URL}, Store: testStore{platform.Session{AccessToken: "user-token", ExpiresAt: time.Now().Add(time.Hour)}}}
	var out, errOut bytes.Buffer
	if err := run(context.Background(), []string{"whoami"}, auth, platform.API{Auth: auth}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "user@example.com") {
		t.Fatalf("output: %s", out.String())
	}
}

func TestRenderSSERequiresDone(t *testing.T) {
	var out bytes.Buffer
	stream := "event: text_delta\ndata: {\"delta\":\"Hello\"}\n\nevent: done\ndata: {}\n\n"
	if err := renderSSE(strings.NewReader(stream), &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "Hello\n" {
		t.Fatalf("output = %q", out.String())
	}
	out.Reset()
	if err := renderSSE(strings.NewReader("event: text_delta\ndata: {\"delta\":\"partial\"}\n\n"), &out); err == nil || !strings.Contains(err.Error(), "inspect the transcript") {
		t.Fatalf("expected incomplete stream error, got %v", err)
	}
}

func TestDestructiveCommandsNeedConfirmation(t *testing.T) {
	var out, errOut bytes.Buffer
	err := run(context.Background(), []string{"keys", "revoke", "key-1"}, platform.Auth{}, platform.API{}, strings.NewReader(""), &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected confirmation, got %v", err)
	}
}

func TestReadSecretDoesNotConsumeNextLine(t *testing.T) {
	in := strings.NewReader("first\nsecond\n")
	first, err := readSecret("", in, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := readSecret("", in, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if first != "first" || second != "second" {
		t.Fatalf("got %q %q", first, second)
	}
}

func TestAgentCreateAndArchiveRoutes(t *testing.T) {
	var created, archived bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer user-token" {
			t.Error("missing user token")
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/agents":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["name"] != "Temporary" {
				t.Errorf("create body: %+v", body)
			}
			created = true
			w.Write([]byte(`{"id":"a-1","name":"Temporary"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/agents/a-1/archive":
			archived = true
			w.Write([]byte(`{"id":"a-1","archived_at":"now"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	auth := platform.Auth{Config: platform.Config{APIURL: server.URL}, Store: testStore{platform.Session{AccessToken: "user-token", ExpiresAt: time.Now().Add(time.Hour)}}}
	api := platform.API{Auth: auth}
	path := filepath.Join(t.TempDir(), "agent.json")
	if err := os.WriteFile(path, []byte(`{"name":"Temporary","llm_mode":"platform_default"}`), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if err := run(context.Background(), []string{"agents", "create", "--file", path}, auth, api, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"agents", "archive", "a-1", "--yes"}, auth, api, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !created || !archived {
		t.Fatalf("created=%v archived=%v", created, archived)
	}
}

func TestChatThinkingOnlyOnCreation(t *testing.T) {
	var createBody map[string]string
	var creates, streams int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/sessions":
			creates++
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Error(err)
			}
			w.Write([]byte(`{"session_id":"s-1","thinking_mode":"low"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/sessions/s-1/messages":
			streams++
			w.Header().Set("Content-Type", "text/event-stream")
			w.Write([]byte("event: done\ndata: {}\n\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	auth := platform.Auth{Config: platform.Config{APIURL: server.URL}, Store: testStore{platform.Session{AccessToken: "user-token", ExpiresAt: time.Now().Add(time.Hour)}}}
	api := platform.API{Auth: auth}
	var out, errOut bytes.Buffer
	if err := run(context.Background(), []string{"chat", "--agent", "a-1", "--thinking", "low", "hello"}, auth, api, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if createBody["agent_id"] != "a-1" || createBody["thinking_mode"] != "low" || creates != 1 || streams != 1 {
		t.Fatalf("create=%v creates=%d streams=%d", createBody, creates, streams)
	}
	if err := run(context.Background(), []string{"chat", "--session", "s-1", "again"}, auth, api, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if creates != 1 || streams != 2 {
		t.Fatalf("continuation created=%d streamed=%d", creates, streams)
	}
	if err := run(context.Background(), []string{"chat", "--session", "s-1", "--thinking", "high", "again"}, auth, api, strings.NewReader(""), &out, &errOut); err == nil {
		t.Fatal("expected --thinking rejection on continuation")
	}
	if err := run(context.Background(), []string{"chat", "--agent", "a-1", "--thinking", "medium", "hello"}, auth, api, strings.NewReader(""), &out, &errOut); err == nil {
		t.Fatal("expected invalid mode rejection")
	}
	if creates != 1 {
		t.Fatalf("invalid inputs created sessions: %d", creates)
	}
}
