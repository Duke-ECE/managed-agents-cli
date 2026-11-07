package platform

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type Auth struct {
	Config Config
	Store  Store
	HTTP   *http.Client
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

func (a Auth) httpClient() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (a Auth) token(ctx context.Context, grant string, payload any) (Session, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return Session{}, err
	}
	endpoint := strings.TrimRight(a.Config.SupabaseURL, "/") + "/auth/v1/token?grant_type=" + grant
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(data)))
	if err != nil {
		return Session{}, err
	}
	req.Header.Set("apikey", a.Config.AnonKey)
	req.Header.Set("Content-Type", "application/json")
	response, err := a.httpClient().Do(req)
	if err != nil {
		return Session{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Session{}, responseError(response)
	}
	var result tokenResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return Session{}, err
	}
	if result.AccessToken == "" || result.RefreshToken == "" || result.ExpiresIn <= 0 {
		return Session{}, errors.New("authentication response is missing session fields")
	}
	return Session{AccessToken: result.AccessToken, RefreshToken: result.RefreshToken, ExpiresAt: time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)}, nil
}

func (a Auth) LoginPassword(ctx context.Context, email, password string) error {
	if email == "" || password == "" {
		return errors.New("email and password are required")
	}
	session, err := a.token(ctx, "password", map[string]string{"email": email, "password": password})
	if err != nil {
		return err
	}
	return a.saveSession(session)
}

func (a Auth) saveSession(session Session) error {
	if err := a.Store.Save(session); err != nil {
		return err
	}
	stored, err := a.Store.Load()
	if err != nil {
		return fmt.Errorf("login session was saved but could not be read back: %w", err)
	}
	if stored.AccessToken != session.AccessToken || stored.RefreshToken != session.RefreshToken || !stored.ExpiresAt.Equal(session.ExpiresAt) {
		return errors.New("login session could not be verified in credential storage")
	}
	return nil
}

func (a Auth) AccessToken(ctx context.Context) (string, error) {
	session, err := a.Store.Load()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", errors.New("not logged in; run managed-agents login")
		}
		return "", err
	}
	if session.ExpiresAt.After(time.Now().Add(time.Minute)) {
		return session.AccessToken, nil
	}
	if session.RefreshToken == "" {
		return "", errors.New("session expired; run managed-agents login")
	}
	refreshed, err := a.token(ctx, "refresh_token", map[string]string{"refresh_token": session.RefreshToken})
	if err != nil {
		return "", fmt.Errorf("session refresh failed; run managed-agents login: %w", err)
	}
	if err := a.Store.Save(refreshed); err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
}

func (a Auth) Logout(ctx context.Context) error {
	access, err := a.AccessToken(ctx)
	if err == nil {
		endpoint := strings.TrimRight(a.Config.SupabaseURL, "/") + "/auth/v1/logout?scope=local"
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
		if reqErr == nil {
			req.Header.Set("apikey", a.Config.AnonKey)
			req.Header.Set("Authorization", "Bearer "+access)
			if response, doErr := a.httpClient().Do(req); doErr == nil {
				response.Body.Close()
			}
		}
	}
	if err := a.Store.Delete(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

type callbackResult struct {
	code string
	err  error
}

// LoginBrowser starts a loopback listener before opening Supabase's GitHub
// authorization URL. Supabase redirects to the already allow-listed origin.
func (a Auth) LoginBrowser(ctx context.Context, openBrowser func(string) error) error {
	if a.Config.AnonKey == "" {
		return errors.New("Supabase public anon key is not configured")
	}
	callback, err := url.Parse(a.Config.CallbackURL)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:"+callback.Port())
	if err != nil {
		return fmt.Errorf("browser login needs localhost:%s; close the process using that port: %w", callback.Port(), err)
	}
	defer listener.Close()
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		return err
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	challenge := sha256.Sum256([]byte(verifier))
	params := url.Values{
		"provider":              {"github"},
		"redirect_to":           {a.Config.CallbackURL},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"s256"},
	}
	authorizeURL := strings.TrimRight(a.Config.SupabaseURL, "/") + "/auth/v1/authorize?" + params.Encode()
	result := make(chan callbackResult, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		message := r.URL.Query().Get("error_description")
		if message == "" {
			message = r.URL.Query().Get("error")
		}
		if message != "" {
			select {
			case result <- callbackResult{err: errors.New(message)}:
			default:
			}
			io.WriteString(w, "Managed Agents login failed. Return to the terminal.")
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Missing authorization code", http.StatusBadRequest)
			return
		}
		select {
		case result <- callbackResult{code: code}:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
			io.WriteString(w, `<!doctype html><html lang="en"><meta charset="utf-8"><title>Managed Agents login</title><p>Authorization received. You can close this tab.</p><script>history.replaceState(null, "", "/"); window.close();</script></html>`)
		default:
			http.Error(w, "Login already completed", http.StatusConflict)
		}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener)
	defer server.Shutdown(context.Background())
	if err := openBrowser(authorizeURL); err != nil {
		return err
	}
	wait, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	select {
	case received := <-result:
		if received.err != nil {
			return received.err
		}
		session, err := a.token(wait, "pkce", map[string]string{"auth_code": received.code, "code_verifier": verifier})
		if err != nil {
			return err
		}
		return a.saveSession(session)
	case <-wait.Done():
		return fmt.Errorf("browser login timed out or was cancelled: %w", wait.Err())
	}
}

func OpenBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("open browser failed; visit %s: %w", target, err)
	}
	return nil
}

func responseError(response *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	var body struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
		Message          string `json:"message"`
	}
	if json.Unmarshal(data, &body) == nil {
		message := body.ErrorDescription
		if message == "" {
			message = body.Message
		}
		if message == "" {
			message = body.Error
		}
		if message != "" {
			return fmt.Errorf("HTTP %d: %s", response.StatusCode, message)
		}
	}
	return fmt.Errorf("HTTP %d: %s", response.StatusCode, http.StatusText(response.StatusCode))
}
