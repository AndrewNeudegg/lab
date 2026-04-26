# homelabctl

`homelabctl` is the supported command-line operator interface for `homelabd` HTTP mode. Use it instead of ad hoc `curl` for task, approval, event, chat, and terminal interactions.

Start `homelabd` in HTTP mode before using the CLI:

```bash
go run ./cmd/homelabd -mode http -config config.json
```

The default API address is `http://127.0.0.1:18080`. Override it with `-addr` or `HOMELABD_ADDR`:

```bash
go run ./cmd/homelabctl -addr http://127.0.0.1:18080 health
HOMELABD_ADDR=http://127.0.0.1:18080 go run ./cmd/homelabctl tasks
```

## Operator Rule

Keep this document, `cmd/homelabctl`, and the `homelabd` HTTP API in sync. When a new operator interaction is added to `homelabd`, add or update the matching `homelabctl` command and tests in the same change. If a workflow requires repeated chat, task, approval, event, or terminal interaction and `homelabctl` is not useful enough, extend the CLI rather than bypassing it.

## Interactive Shell

Use the shell for chat commands and natural operator requests:

```bash
go run ./cmd/homelabctl shell
```

Inside the shell, enter the same commands accepted by `homelabd` chat, for example:

```text
status
tasks
show task_123
delegate task_123 to codex fix the failing tests
approve app_123
```

Type `exit` or `quit` to leave. Use `-from` or `HOMELABCTL_FROM` to change the sender name recorded in chat logs:

```bash
go run ./cmd/homelabctl -from codex shell
```

For one-shot chat commands:

```bash
go run ./cmd/homelabctl message "status"
go run ./cmd/homelabctl status
go run ./cmd/homelabctl agents
```

`message`, `chat`, `say`, and chat-command shortcuts print the plain `reply` field by default. Add `-json` when the full response object is needed.

## Task Commands

Direct task commands use the typed HTTP endpoints and print pretty JSON:

```bash
go run ./cmd/homelabctl task new "Add dashboard regression tests"
go run ./cmd/homelabctl task new --agent workstation --workdir repo "Update this checkout"
go run ./cmd/homelabctl task list
go run ./cmd/homelabctl task show task_123
go run ./cmd/homelabctl task runs task_123
go run ./cmd/homelabctl task run task_123
go run ./cmd/homelabctl task review task_123
go run ./cmd/homelabctl task accept task_123
go run ./cmd/homelabctl task reopen task_123 "needs rework"
go run ./cmd/homelabctl task cancel task_123
go run ./cmd/homelabctl task retry task_123 codex "retry from the current workspace state"
```

The remote target flags are optional. Use `--agent <agent_id>` with `--workdir <workdir_id>` for a remote task in an advertised workdir, or `--workdir-path <path>` when the advertised path is the stable identifier. `--backend` overrides the backend that the remote agent should run.

Top-level aliases are available for common task actions:

```bash
go run ./cmd/homelabctl new "Fix stale running task recovery"
go run ./cmd/homelabctl run task_123
go run ./cmd/homelabctl review task_123
go run ./cmd/homelabctl accept task_123
go run ./cmd/homelabctl reopen task_123 "needs mobile UAT"
go run ./cmd/homelabctl cancel task_123
go run ./cmd/homelabctl retry task_123
go run ./cmd/homelabctl runs task_123
```

Some orchestrator actions, such as `delegate`, `refresh`, `diff`, `test`, and `delete`, are chat commands rather than dedicated HTTP endpoints. `homelabctl` sends those shortcuts through `/message`:

```bash
go run ./cmd/homelabctl delegate task_123 to codex "finish docs and tests"
go run ./cmd/homelabctl refresh task_123
go run ./cmd/homelabctl diff task_123
go run ./cmd/homelabctl delete task_123
```

## Remote Agent Commands

Remote machines use the `/agents` inventory. This is separate from the chat command `agents`, which lists external local worker backends such as `codex`, `claude`, and `gemini`.

```bash
go run ./cmd/homelabctl agent list
go run ./cmd/homelabctl agent show workstation
```

See `docs/remote-agents.md` for remote agent setup and polling details.

## Approval Commands

```bash
go run ./cmd/homelabctl approval list
go run ./cmd/homelabctl approval approve app_123
go run ./cmd/homelabctl approval deny app_123
```

Top-level `approve` and `deny` aliases are also available:

```bash
go run ./cmd/homelabctl approve app_123
go run ./cmd/homelabctl deny app_123
```

## Event Commands

Read the current UTC day's event log:

```bash
go run ./cmd/homelabctl events
```

Read a specific day or only the latest events:

```bash
go run ./cmd/homelabctl events 2026-04-26
go run ./cmd/homelabctl events -limit 20
go run ./cmd/homelabctl events -limit 20 2026-04-26
```

## Terminal Commands

The terminal commands wrap the same `/terminal/sessions` API used by the dashboard Terminal page.

Create a shell session:

```bash
go run ./cmd/homelabctl terminal start
go run ./cmd/homelabctl terminal start /home/lab/lab
```

Stream session output:

```bash
go run ./cmd/homelabctl terminal stream term_123
```

Send input:

```bash
go run ./cmd/homelabctl terminal send term_123 "git status --short"
go run ./cmd/homelabctl terminal input term_123 $'\003'
```

`send` appends a newline for command-style input. `input` sends the text exactly as provided.

Send signals or close the session:

```bash
go run ./cmd/homelabctl terminal interrupt term_123
go run ./cmd/homelabctl terminal signal term_123 terminate
go run ./cmd/homelabctl terminal close term_123
```

## API Coverage

`homelabctl` covers the current `homelabd` HTTP operator API:

- `GET /healthz`
- `POST /message`
- `GET /tasks`
- `POST /tasks`, including optional remote `target`
- `GET /tasks/{id}`
- `GET /tasks/{id}/runs`
- `POST /tasks/{id}/run`
- `POST /tasks/{id}/review`
- `POST /tasks/{id}/accept`
- `POST /tasks/{id}/reopen`
- `POST /tasks/{id}/cancel`
- `POST /tasks/{id}/retry`
- `GET /agents`
- `GET /agents/{id}`
- `GET /approvals`
- `POST /approvals/{id}/approve`
- `POST /approvals/{id}/deny`
- `GET /events?date=YYYY-MM-DD&limit=N`
- `POST /terminal/sessions`
- `GET /terminal/sessions/{id}/events`
- `POST /terminal/sessions/{id}/input`
- `POST /terminal/sessions/{id}/signal`
- `DELETE /terminal/sessions/{id}`

Run the CLI tests after changing the HTTP API or command surface:

```bash
go test ./cmd/homelabctl
```
