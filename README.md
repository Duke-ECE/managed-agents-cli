# PolyAgent CLI

A Go command-line client for [PolyAgent](https://managed-agents.colab.duke.edu/). It signs in as a user and manages that user's agent templates, sessions, and integration API keys.

## Install

Install with Go 1.25 or newer:

```sh
go install github.com/Duke-ECE/managed-agents-cli/cmd/polyagent@v0.2.0
polyagent help
```

Prebuilt macOS, Linux, and Windows binaries are available on the [Releases page](https://github.com/Duke-ECE/managed-agents-cli/releases). Earlier releases use the `managed-agents` command. Verify the archive against `SHA256SUMS` before installing it.

During development, use `go run ./cmd/polyagent <command>` from this repository.

Existing scripts can keep using `managed-agents` via the legacy
`cmd/managed-agents` entry point. Both commands share the same sign-in session.

## Sign in

```sh
polyagent login
polyagent whoami
```

`login` opens GitHub OAuth in your browser. The CLI listens on `http://localhost:5173` for the one-time Supabase PKCE callback; that exact URL is already allow-listed for the platform. Close any Vite preview or other process using port 5173 before logging in. The callback page tries to close itself after authorization; if the browser blocks this, close the tab manually. The CLI exchanges the code for a user session and saves it in a hidden directory under your home folder. The browser does not hand its existing console session to the CLI; it performs a separate sign-in.

For an account with an email password:

```sh
polyagent login --email you@example.com
```

The password is read without echo on an interactive terminal and is not stored. The access and refresh tokens are stored in `~/.managed-agents/session.json` (`~` means the OS user home directory). On macOS and Linux, the CLI creates the directory with mode `0700` and the file with mode `0600`; Windows uses the user profile ACL. `logout` revokes the current CLI refresh token when possible and removes the file. It does not sign out browser sessions. After upgrading from an older Keychain or config-directory build, run `managed-agents login` once to create the new file-based session.

## Commands

```sh
polyagent whoami
polyagent account email new@example.com
polyagent account password

polyagent agents list
polyagent agents show <agent-id>
polyagent agents create --file agent.json
polyagent agents clone <agent-id>
polyagent agents archive <agent-id>

polyagent sessions list
polyagent sessions show <session-id>
polyagent sessions config <session-id>
polyagent sessions rename <session-id> "New title"
polyagent sessions end <session-id>
polyagent sessions delete <session-id>

polyagent keys list
polyagent keys create "server integration"
polyagent keys revoke <key-id>

polyagent chat --agent <agent-id> "Hello"
polyagent chat --agent <agent-id> --thinking low "Solve this carefully"
polyagent chat --session <session-id> "Continue"
polyagent logout
```

The built-in Default assistant has agent ID `a0000000-0000-0000-0000-000000000001`. Using the platform LLM requires membership. Agent templates are immutable after creation: clone one to make a new private template, and archive your old template. Platform templates are read-only but cloneable. Create input uses the backend JSON shape, for example:

`--thinking` chooses `off`, `low`, `high`, `max`, or `provider_default` when creating a session. If omitted, the agent template setting applies. The choice stays with the session; `chat --session` cannot change it. `sessions config <id>` shows the frozen mode. Explicit `off`, `low`, `high`, and `max` currently require the official DeepSeek Flash endpoint; the API rejects unsupported model combinations.

```json
{
  "name": "Support assistant",
  "description": "Answers support questions",
  "system_prompt": "Answer clearly and concisely.",
  "llm_mode": "platform_default",
  "thinking_mode": "provider_default",
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
