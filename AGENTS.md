# managed-agents-cli

Go 1.25 command-line client for Managed Agents. Follow the workspace instructions and [Duke-ECE standards](https://github.com/Duke-ECE/standards/blob/main/AGENTS.md), applying service-only rules only to server repositories.

- Command entry point: `cmd/managed-agents/main.go`; Supabase Auth, credentials, and backend HTTP client: `internal/platform/`.
- Browser login uses PKCE with the exact, already allow-listed `http://localhost:5173` callback. Never print authorization codes, access or refresh tokens, passwords, or API keys except the one-time result of `keys create`.
- User sessions live in `~/.managed-agents/session.json` under the OS user home directory. On Unix, the file is mode 0600 and its directory is created mode 0700; Windows relies on the user profile ACL. Keep the service-role key out of this repository and binary.
- Use user JWTs for `/api/me`, `/api/agents`, `/api/keys`, and `/api/sessions*`. Platform API keys authenticate only `/api/sessions*` in third-party applications.
- Agent templates are immutable after creation. Use clone and archive; never add an update command that calls the backend's rejecting PATCH route.
- Run `GOTOOLCHAIN=local go build ./...`, `go vet ./...`, and `go test ./...` before committing. Use stdlib `testing` and fake HTTP servers. All code comments and docs are English.
