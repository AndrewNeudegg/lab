# Remote Agents

`homelabd` acts as the control plane for local and remote workers. A remote machine runs `homelab-agent`, which phones home to the `homelabd` HTTP API, advertises the exact directories it can work in, polls only for tasks assigned to that agent, runs the configured external worker command in the selected directory, and reports the result back to the task store.

The design intentionally treats remote machines as worker nodes:

- agent identity is stable and explicit (`agent_id`, machine, service instance)
- heartbeats drive readiness and health
- tasks are routed by target queue, not by a global repo
- local `homelabd` worktrees are never created for remote tasks
- remote review never compares the remote checkout to `homelabd`'s local `main`

## Control Plane

Set a shared token on the API server. Agent mutation endpoints reject requests when no token is configured.

```sh
export HOMELABD_AGENT_TOKEN='replace-with-a-long-random-token'
go run ./cmd/homelabd -mode http
```

Relevant config:

```json
{
  "control_plane": {
    "agent_token_env": "HOMELABD_AGENT_TOKEN",
    "agent_stale_seconds": 30
  }
}
```

The dashboard task page lists registered agents and can create a task directly against an agent plus directory. CLI usage is also available. Use `--workdir` for an advertised workdir id such as `repo`; use `--workdir-path` only when you need to target an advertised path by full path instead of id.

```sh
homelabctl -addr http://127.0.0.1:18080 agent list
homelabctl -addr http://127.0.0.1:18080 task new --agent workstation --workdir repo "Update the service in this checkout"
```

The task page separates queues by execution target:

- `All queues`
- `Local homelabd`
- one queue per registered remote agent

When creating a remote task, the dashboard requires an explicit context confirmation that names the agent, machine, and full working directory path. If the target looks wrong, do not check that box. The API also resolves the selected workdir against the agent's advertised workdirs; an unknown workdir id/path is rejected instead of falling back to another checkout.

## Remote Worker

On a remote machine, run:

```sh
export HOMELABD_AGENT_TOKEN='same-token-as-control-plane'
go run ./cmd/homelab-agent \
  -api http://homelab:18080 \
  -id workstation \
  -name Workstation \
  -workdir repo=/home/me/project \
  -terminal-addr 0.0.0.0:18083 \
  -terminal-url http://workstation:18083
```

The agent uses the `external_agents` command for the assigned backend, defaulting to `codex`. It executes in the selected working directory and sends stdout/stderr back as the task result. The same backend `timeout_seconds` applies to remote task execution; omitted or zero values default to 18,000 seconds, or 5 hours. The default Codex backend disables Codex's own sandbox because local tasks already run in isolated worktrees and Codex otherwise may remount `.git` read-only, which prevents Git worktree metadata updates.

Remote agents do not need to run in this repository. Each advertised `workdir` can be a different checkout, a different project, or a non-git directory. `homelabd` stores the path as execution context only; it does not assume that remote path has the same HEAD, branch, or repository root as the control-plane checkout.

Set `remote_agent.workdirs` explicitly on each worker. If no workdirs are configured, `homelab-agent` falls back to the configured `repo.root`, which is useful for local development but too easy to point at the wrong tree on a real machine.

## Remote Testing

Remote agents validate in the selected remote workdir. They must not call the control-plane supervisor to restart production services, and they must not assume `127.0.0.1:5173` points at the operator's dashboard.

When a remote result needs to explain execution flow, state, dependencies, or machine context, include a Mermaid fenced diagram. The control-plane dashboard renders Mermaid in chat and docs and applies the shared light/dark brand palette documented in `docs/dashboard.md#markdown-diagrams-and-brand-colours`.

For dashboard task-page changes in a remote checkout, run:

```sh
nix develop -c bun run --cwd web uat:tasks
```

For broad dashboard shell, navigation, theme, terminal, docs, workflow, health, or supervisor changes, run:

```sh
nix develop -c bun run --cwd web uat:site
```

Both commands start a Playwright-managed Vite server on that remote machine, choose a per-worktree port unless `PLAYWRIGHT_PORT` is set, and mock `homelabd` APIs. `uat:site` also mocks `healthd` and `supervisord`, covers every primary page on desktop and mobile, and attaches screenshots. The remote completion summary should include the command, the generated local URL when relevant, and whether Chromium came from `CHROME_BIN` or a Playwright browser install. If `browser:preflight` fails because the remote sandbox cannot launch Chromium, report that infrastructure failure instead of touching production services.

Use `uat:tasks:live` only when the operator explicitly asks the remote machine to verify a running dashboard URL.

## Remote Terminals

`homelab-agent` can optionally expose the same PTY terminal API as `homelabd`. Set `remote_agent.terminal_addr` or pass `-terminal-addr` to bind the local terminal server, and set `remote_agent.terminal_public_url` or `-terminal-url` to the browser-reachable base URL that should be advertised in heartbeats.

The dashboard Terminal page always offers `homelabd local`. Online remote agents are added to the session target picker when their heartbeat metadata contains `terminal_base_url`. Selecting a remote target and pressing the adjacent `+` button starts the PTY on that remote agent, not on the control plane.

Treat remote terminal URLs as trusted operator endpoints. They execute commands as the `homelab-agent` process user on the remote machine.

## healthd Integration

Every accepted remote-agent heartbeat is forwarded by `homelabd` into `healthd` as a process heartbeat named `remote-agent:<agent_id>` with type `remote_agent`. The process metadata includes:

- `agent_id`
- `machine`
- `service.name=homelab-agent`
- `service.instance.id=<agent_id>`
- advertised workdir count
- current task id, when one is running
- capability list

This makes remote agents visible on the Health page alongside `homelabd`, `supervisord`, and other processes. `GET /agents` marks an agent offline after `control_plane.agent_stale_seconds`; healthd receives the same value as the process heartbeat TTL and turns stale heartbeats into critical process check failures. Health forwarding is best-effort and does not block task scheduling if healthd is unavailable.

## supervisord Integration

The default supervisor config includes a non-autostart `homelab-agent` app template:

```json
{
  "name": "homelab-agent",
  "type": "agent",
  "command": "go",
  "args": ["run", "./cmd/homelab-agent"],
  "working_dir": ".",
  "auto_start": false,
  "restart": "always"
}
```

On machines where you want `supervisord` to keep the remote worker alive, set that app's `working_dir`, `remote_agent` config, and token environment, then set `auto_start` to `true`. The app does not need a `health_url`; health is reported by `homelab-agent` heartbeats that `homelabd` forwards to healthd.

Keep `auto_start` false on the control-plane machine unless it should also act as a worker.

## API Shape

- `GET /agents` lists known remote agents for the UI.
- `GET /agents/{id}` returns one registered agent.
- `POST /agents/{id}/heartbeat` registers or refreshes an agent. `POST /agents` also accepts a heartbeat body with `id`.
- `POST /agents/{id}/claim` claims the next queued task targeted to that agent.
- `POST /agents/{id}/tasks/{task_id}/complete` records completion.
- `POST /tasks` accepts an optional `target` object with `mode: "remote"`, `agent_id`, `workdir_id` or advertised `workdir`, and `backend`.
- `POST /tasks/{task_id}/assign` retargets a non-terminal task to a remote agent and advertised workdir.

Remote tasks intentionally skip the local task supervisor. The selected remote agent owns execution until it reports completion or failure.

## Review Semantics

Local tasks still use local worktrees, local checks, diff review, and merge approval.

Remote tasks do not. For a remote task:

1. The remote agent runs the worker in the selected remote directory.
2. The remote agent reports output and validation back to `homelabd`.
3. `review <task>` acknowledges the remote result and moves the task to `awaiting_verification`.
4. Human verification happens against the named remote machine/directory.
5. `accept <task>` closes it, or `reopen <task> <reason>` queues more remote work.

No local merge approval is created for remote tasks because the control plane cannot prove that the remote checkout corresponds to its own repo.
