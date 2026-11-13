package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/Duke-ECE/managed-agents-cli/internal/platform"
	"golang.org/x/term"
)

const usage = `PolyAgent CLI

Usage:
  polyagent login [--email address]       Browser GitHub login by default
  polyagent logout
  polyagent whoami
  polyagent account email <address>
  polyagent account password
  polyagent agents list|show <id>|create --file <path>|clone <id>|archive <id> [--yes]
  polyagent sessions list|show <id>|config <id>|rename <id> <title>|end <id>|delete <id> [--yes]
  polyagent keys list|create <name>|revoke <id> [--yes]
  polyagent chat (--agent <id> [--thinking <mode>] | --session <id>) <message>

Environment overrides:
  MANAGED_AGENTS_API_URL, MANAGED_AGENTS_SUPABASE_URL,
  MANAGED_AGENTS_SUPABASE_ANON_KEY
`

func Main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	store, err := platform.DefaultStore()
	if err != nil {
		fail(err)
	}
	auth := platform.Auth{Config: platform.FromEnvironment(), Store: store}
	api := platform.API{Auth: auth}
	if err := run(ctx, os.Args[1:], auth, api, os.Stdin, os.Stdout, os.Stderr); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "Error:", err)
	os.Exit(1)
}

func run(ctx context.Context, args []string, auth platform.Auth, api platform.API, in io.Reader, out, errOut io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(out, usage)
		return nil
	}
	switch args[0] {
	case "help", "--help", "-h":
		fmt.Fprint(out, usage)
		return nil
	case "login":
		flags := flag.NewFlagSet("login", flag.ContinueOnError)
		flags.SetOutput(errOut)
		email := flags.String("email", "", "email/password login instead of browser login")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("login does not accept positional arguments")
		}
		if *email != "" {
			password, err := readSecret("Password: ", in, errOut)
			if err != nil {
				return err
			}
			if err := auth.LoginPassword(ctx, *email, password); err != nil {
				return err
			}
		} else {
			fmt.Fprintln(errOut, "Opening GitHub login in your browser (callback: http://localhost:5173)...")
			if err := auth.LoginBrowser(ctx, platform.OpenBrowser); err != nil {
				return err
			}
		}
		fmt.Fprintln(out, "Logged in. Run polyagent whoami to inspect your account.")
		return nil
	case "logout":
		if len(args) != 1 {
			return errors.New("usage: polyagent logout")
		}
		if err := auth.Logout(ctx); err != nil {
			return err
		}
		fmt.Fprintln(out, "Logged out of this CLI session.")
		return nil
	case "whoami":
		if len(args) != 1 {
			return errors.New("usage: polyagent whoami")
		}
		return requestJSON(ctx, api, http.MethodGet, "/api/me", nil, out)
	case "account":
		return account(ctx, args[1:], auth, in, out, errOut)
	case "agents":
		return agents(ctx, args[1:], api, in, out, errOut)
	case "sessions":
		return sessions(ctx, args[1:], api, in, out, errOut)
	case "keys":
		return keys(ctx, args[1:], api, in, out, errOut)
	case "chat":
		return chat(ctx, args[1:], api, out, errOut)
	default:
		return fmt.Errorf("unknown command %q; run polyagent help", args[0])
	}
}

func account(ctx context.Context, args []string, auth platform.Auth, in io.Reader, out, errOut io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: polyagent account email <address> | password")
	}
	switch args[0] {
	case "email":
		if len(args) != 2 {
			return errors.New("usage: polyagent account email <address>")
		}
		if err := auth.UpdateUser(ctx, map[string]string{"email": args[1]}); err != nil {
			return err
		}
		fmt.Fprintln(out, "Email change requested. Follow the confirmation email before using the new address.")
		return nil
	case "password":
		if len(args) != 1 {
			return errors.New("usage: polyagent account password")
		}
		first, err := readSecret("New password: ", in, errOut)
		if err != nil {
			return err
		}
		second, err := readSecret("Confirm password: ", in, errOut)
		if err != nil {
			return err
		}
		if first != second {
			return errors.New("passwords do not match")
		}
		if err := auth.UpdateUser(ctx, map[string]string{"password": first}); err != nil {
			return err
		}
		fmt.Fprintln(out, "Password updated.")
		return nil
	default:
		return errors.New("usage: polyagent account email <address> | password")
	}
}

func readSecret(prompt string, in io.Reader, errOut io.Writer) (string, error) {
	fmt.Fprint(errOut, prompt)
	if file, ok := in.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		data, err := term.ReadPassword(int(file.Fd()))
		fmt.Fprintln(errOut)
		return string(data), err
	}
	var line []byte
	for len(line) < 4096 {
		one := make([]byte, 1)
		_, err := in.Read(one)
		if err != nil {
			if errors.Is(err, io.EOF) && len(line) > 0 {
				break
			}
			return "", err
		}
		if one[0] == '\n' {
			break
		}
		line = append(line, one[0])
	}
	if len(line) == 4096 {
		return "", errors.New("input is too long")
	}
	return strings.TrimSuffix(string(line), "\r"), nil
}

func agents(ctx context.Context, args []string, api platform.API, in io.Reader, out, errOut io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: polyagent agents list|show|create|clone|archive")
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return errors.New("usage: polyagent agents list")
		}
		data, err := api.JSON(ctx, http.MethodGet, "/api/agents", nil)
		if err != nil {
			return err
		}
		return renderList(data, "agents", []string{"ID", "NAME", "VISIBILITY", "LLM MODE", "ARCHIVED"}, []string{"id", "name", "visibility", "llm_mode", "archived_at"}, out)
	case "show":
		if len(args) != 2 {
			return errors.New("usage: polyagent agents show <id>")
		}
		return requestJSON(ctx, api, http.MethodGet, "/api/agents/"+url.PathEscape(args[1]), nil, out)
	case "create":
		flags := flag.NewFlagSet("agents create", flag.ContinueOnError)
		flags.SetOutput(errOut)
		file := flags.String("file", "", "JSON agent definition; use - for stdin")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *file == "" || flags.NArg() != 0 {
			return errors.New("usage: polyagent agents create --file <path|->")
		}
		var data []byte
		var err error
		if *file == "-" {
			data, err = io.ReadAll(io.LimitReader(in, 1<<20))
		} else {
			data, err = os.ReadFile(*file)
		}
		if err != nil {
			return err
		}
		if !json.Valid(data) {
			return errors.New("agent definition must be valid JSON")
		}
		return requestJSON(ctx, api, http.MethodPost, "/api/agents", json.RawMessage(data), out)
	case "clone":
		if len(args) != 2 {
			return errors.New("usage: polyagent agents clone <id>")
		}
		return requestJSON(ctx, api, http.MethodPost, "/api/agents/"+url.PathEscape(args[1])+"/clone", map[string]string{}, out)
	case "archive":
		rest, yes := extractYes(args[1:])
		if len(rest) != 1 {
			return errors.New("usage: polyagent agents archive <id> [--yes]")
		}
		if err := confirm("Archive agent "+rest[0], yes, in, errOut); err != nil {
			return err
		}
		return requestJSON(ctx, api, http.MethodPost, "/api/agents/"+url.PathEscape(rest[0])+"/archive", map[string]string{}, out)
	default:
		return fmt.Errorf("unknown agents command %q", args[0])
	}
}

func sessions(ctx context.Context, args []string, api platform.API, in io.Reader, out, errOut io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: polyagent sessions list|show|config|rename|end|delete")
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return errors.New("usage: polyagent sessions list")
		}
		data, err := api.JSON(ctx, http.MethodGet, "/api/sessions?limit=50", nil)
		if err != nil {
			return err
		}
		return renderList(data, "sessions", []string{"ID", "TITLE", "STATUS", "AGENT", "LAST ACTIVE"}, []string{"id", "title", "status", "agent_id", "last_active"}, out)
	case "show":
		if len(args) != 2 {
			return errors.New("usage: polyagent sessions show <id>")
		}
		return requestJSON(ctx, api, http.MethodGet, "/api/sessions/"+url.PathEscape(args[1])+"/messages?limit=50", nil, out)
	case "config":
		if len(args) != 2 {
			return errors.New("usage: polyagent sessions config <id>")
		}
		return requestJSON(ctx, api, http.MethodGet, "/api/sessions/"+url.PathEscape(args[1])+"/config", nil, out)
	case "rename":
		if len(args) != 3 {
			return errors.New("usage: polyagent sessions rename <id> <title>")
		}
		return requestJSON(ctx, api, http.MethodPatch, "/api/sessions/"+url.PathEscape(args[1])+"/title", map[string]string{"title": args[2]}, out)
	case "end", "delete":
		rest, yes := extractYes(args[1:])
		if len(rest) != 1 {
			return fmt.Errorf("usage: polyagent sessions %s <id> [--yes]", args[0])
		}
		if err := confirm(strings.ToUpper(args[0][:1])+args[0][1:]+" session "+rest[0], yes, in, errOut); err != nil {
			return err
		}
		path := "/api/sessions/" + url.PathEscape(rest[0])
		if args[0] == "delete" {
			path += "?purge=true"
		}
		return requestJSON(ctx, api, http.MethodDelete, path, nil, out)
	default:
		return fmt.Errorf("unknown sessions command %q", args[0])
	}
}

func keys(ctx context.Context, args []string, api platform.API, in io.Reader, out, errOut io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: polyagent keys list|create|revoke")
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return errors.New("usage: polyagent keys list")
		}
		data, err := api.JSON(ctx, http.MethodGet, "/api/keys", nil)
		if err != nil {
			return err
		}
		return renderList(data, "keys", []string{"ID", "NAME", "PREFIX", "CREATED", "REVOKED"}, []string{"id", "name", "prefix", "created_at", "revoked_at"}, out)
	case "create":
		if len(args) != 2 {
			return errors.New("usage: polyagent keys create <name>")
		}
		fmt.Fprintln(errOut, "The new secret is shown once. Store it in your application's secret manager.")
		return requestJSON(ctx, api, http.MethodPost, "/api/keys", map[string]string{"name": args[1]}, out)
	case "revoke":
		rest, yes := extractYes(args[1:])
		if len(rest) != 1 {
			return errors.New("usage: polyagent keys revoke <id> [--yes]")
		}
		if err := confirm("Revoke API key "+rest[0], yes, in, errOut); err != nil {
			return err
		}
		return requestJSON(ctx, api, http.MethodDelete, "/api/keys/"+url.PathEscape(rest[0]), nil, out)
	default:
		return fmt.Errorf("unknown keys command %q", args[0])
	}
}

func chat(ctx context.Context, args []string, api platform.API, out, errOut io.Writer) error {
	flags := flag.NewFlagSet("chat", flag.ContinueOnError)
	flags.SetOutput(errOut)
	agent := flags.String("agent", "", "existing accessible agent ID")
	session := flags.String("session", "", "existing session ID")
	thinking := flags.String("thinking", "", "new session mode: off|low|high|max|provider_default")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if (*agent == "") == (*session == "") || flags.NArg() != 1 {
		return errors.New("usage: polyagent chat (--agent <id> [--thinking <mode>] | --session <id>) <message>")
	}
	if *thinking != "" {
		if *session != "" {
			return errors.New("--thinking is only valid when creating a session with --agent")
		}
		switch *thinking {
		case "off", "low", "high", "max", "provider_default":
		default:
			return fmt.Errorf("invalid --thinking mode %q", *thinking)
		}
	}
	id := *session
	if id == "" {
		body := map[string]string{"agent_id": *agent}
		if *thinking != "" {
			body["thinking_mode"] = *thinking
		}
		data, err := api.JSON(ctx, http.MethodPost, "/api/sessions", body)
		if err != nil {
			return err
		}
		var created struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(data, &created); err != nil {
			return err
		}
		if created.SessionID == "" {
			return errors.New("create-session response is missing session_id")
		}
		id = created.SessionID
		fmt.Fprintln(errOut, "Session:", id)
	}
	response, err := api.Stream(ctx, id, flags.Arg(0))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return renderSSE(response.Body, out)
}

func renderSSE(body io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	event := "message"
	var data []string
	done := false
	flush := func() error {
		if len(data) == 0 {
			event = "message"
			return nil
		}
		payload := strings.Join(data, "\n")
		switch event {
		case "text_delta":
			var part struct {
				Delta   string `json:"delta"`
				Content string `json:"content"`
				Text    string `json:"text"`
			}
			if err := json.Unmarshal([]byte(payload), &part); err != nil {
				return err
			}
			value := part.Delta
			if value == "" {
				value = part.Content
			}
			if value == "" {
				value = part.Text
			}
			if _, err := io.WriteString(out, value); err != nil {
				return err
			}
		case "error":
			return fmt.Errorf("chat stream error: %s", payload)
		case "done":
			done = true
		}
		event = "message"
		data = nil
		return nil
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if err := flush(); err != nil {
		return err
	}
	if !done {
		return errors.New("chat stream ended without done; inspect the transcript before retrying")
	}
	_, err := fmt.Fprintln(out)
	return err
}

func requestJSON(ctx context.Context, api platform.API, method, path string, body any, out io.Writer) error {
	data, err := api.JSON(ctx, method, path, body)
	if err != nil {
		return err
	}
	if string(data) == "null" {
		_, err = fmt.Fprintln(out, "OK")
		return err
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, data, "", "  "); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, pretty.String())
	return err
}

func renderList(data json.RawMessage, field string, headings, columns []string, out io.Writer) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	var rows []map[string]any
	if err := json.Unmarshal(object[field], &rows); err != nil {
		return fmt.Errorf("response is missing %s: %w", field, err)
	}
	writer := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, strings.Join(headings, "\t"))
	for _, row := range rows {
		values := make([]string, len(columns))
		for i, column := range columns {
			if row[column] != nil {
				values[i] = fmt.Sprint(row[column])
			}
			values[i] = strings.ReplaceAll(values[i], "\t", " ")
			values[i] = strings.ReplaceAll(values[i], "\n", " ")
		}
		fmt.Fprintln(writer, strings.Join(values, "\t"))
	}
	return writer.Flush()
}

func extractYes(args []string) ([]string, bool) {
	var rest []string
	yes := false
	for _, arg := range args {
		if arg == "--yes" {
			yes = true
		} else {
			rest = append(rest, arg)
		}
	}
	return rest, yes
}

func confirm(action string, yes bool, in io.Reader, errOut io.Writer) error {
	if yes {
		return nil
	}
	file, ok := in.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return errors.New("add --yes to confirm this operation in a non-interactive terminal")
	}
	fmt.Fprintf(errOut, "%s? Type yes to continue: ", action)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil {
		return err
	}
	if strings.TrimSpace(line) != "yes" {
		return errors.New("cancelled")
	}
	return nil
}
