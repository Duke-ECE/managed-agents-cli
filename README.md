# Managed Agents CLI

A Go command-line client for [Managed Agents](https://managed-agents.colab.duke.edu/). It signs in as a user and manages that user's agent templates, sessions, and integration API keys.

## Install

Install with Go 1.25 or newer:

```sh
go install github.com/Duke-ECE/managed-agents-cli/cmd/managed-agents@latest
managed-agents help
```

Prebuilt macOS, Linux, and Windows binaries are available on the [Releases page](https://github.com/Duke-ECE/managed-agents-cli/releases). Verify the archive against `SHA256SUMS` before installing it.

If a configured Go mirror cannot verify the new module, use the official proxy for installation:

```sh
GOPROXY=https://proxy.golang.org,direct GOSUMDB=sum.golang.org go install github.com/Duke-ECE/managed-agents-cli/cmd/managed-agents@latest
```

During development, use `go run ./cmd/managed-agents <command>` from this repository.

## Sign in

```sh
managed-agents login
managed-agents whoami
```

`login` opens GitHub OAuth in your browser. The CLI listens on `http://localhost:5173` for the one-time Supabase PKCE callback; that exact URL is already allow-listed for the platform. Close any Vite preview or other process using port 5173 before logging in. The callback page tries to close itself after authorization; if the browser blocks this, close the tab manually. The CLI exchanges the code for a user session and saves it in a hidden directory under your home folder. The browser does not hand its existing console session to the CLI; it performs a separate sign-in.

For an account with an email password:

```sh
managed-agents login --email you@example.com
```

The password is read without echo on an interactive terminal and is not stored. The access and refresh tokens are stored in `~/.managed-agents/session.json` (`~` means the OS user home directory). On macOS and Linux, the CLI creates the directory with mode `0700` and the file with mode `0600`; Windows uses the user profile ACL. `logout` revokes the current CLI refresh token when possible and removes the file. It does not sign out browser sessions. After upgrading from an older Keychain or config-directory build, run `managed-agents login` once to create the new file-based session.

## Commands

```sh
managed-agents whoami
managed-agents account email new@example.com
managed-agents account password

managed-agents agents list
managed-agents agents show <agent-id>
managed-agents agents create --file agent.json
managed-agents agents clone <agent-id>
managed-agents agents archive <agent-id>

managed-agents sessions list
managed-agents sessions show <session-id>
managed-agents sessions rename <session-id> "New title"
managed-agents sessions end <session-id>
managed-agents sessions delete <session-id>

managed-agents keys list
managed-agents keys create "server integration"
managed-agents keys revoke <key-id>

managed-agents chat --agent <agent-id> "Hello"
managed-agents chat --session <session-id> "Continue"
managed-agents logout
```

The built-in Default assistant has agent ID `a0000000-0000-0000-0000-000000000001`. Using the platform LLM requires membership. Agent templates are immutable after creation: clone one to make a new private template, and archive your old template. Platform templates are read-only but cloneable. Create input uses the backend JSON shape, for example:

```json
{
  "name": "Support assistant",
  "description": "Answers support questions",
  "system_prompt": "Answer clearly and concisely.",
  "llm_mode": "platform_default",
  "tools": []
}
```

For a custom model, supply `llm_mode: "custom"`, `llm_api_key`, `llm_base_url`, and `llm_model`. `llm_api_key` is write-only in the backend. Use `--file -` to read the create JSON from stdin when you do not want the provider key in a file.

Archive, end, delete, and revoke ask for confirmation; pass `--yes` in scripts. `sessions delete` permanently purges the transcript. Session list and transcript commands currently show the first/latest 50 records; pagination will be added after the initial CLI release.

`keys create` prints the new API key secret once. Save it in the third-party application's secret manager; the CLI does not retain it. Integration API keys authenticate only `/api/sessions*`, while user login is required for account, agent, and key management. Do not put either key into client-side browser code.

The `chat` command streams text and prints the session ID to stderr so you can continue it. If the stream ends before a `done` event, inspect `sessions show <id>` before resending: the currently deployed API does not deduplicate chat submissions.

## Development

```sh
GOTOOLCHAIN=local go build ./...
GOTOOLCHAIN=local go vet ./...
GOTOOLCHAIN=local go test ./...
```

Public service settings default to production and can be overridden for a staging environment with `MANAGED_AGENTS_API_URL`, `MANAGED_AGENTS_SUPABASE_URL`, and `MANAGED_AGENTS_SUPABASE_ANON_KEY`. The built-in anon key is public and used only to call Supabase Auth; no service-role key is part of this CLI.

The source of truth for the CLI's scope and release status is [requirement 0003](https://github.com/Duke-ECE/managed-agents-design/tree/feat/third-party-chat-sdk/requirements/0003-managed-agents-cli).
