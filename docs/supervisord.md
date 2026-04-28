# supervisord

`supervisord` is the local process supervisor for homelab runtime components.

It owns three jobs:

- start configured applications in `start_order`
- stop and restart them with graceful `SIGTERM` before forced `SIGKILL`
- publish its own heartbeat to `healthd`

Do not use `supervisord` restarts as an agent test harness. Agent browser UAT must start an isolated dev server from the task worktree; the production `dashboard`, `homelabd`, `healthd`, and `supervisord` processes should only be restarted after an explicit operator decision. See `docs/agentic-testing.md`.

## Run

```sh
go run ./cmd/supervisord
```

By default the control API listens on `127.0.0.1:18082`.

For the local development stack, prefer the wrapper commands:

```sh
./run.sh stack-start
./run.sh stack-restart
./run.sh stack-stop
```

`stack-start` starts `supervisord`, adopts any already-running `healthd`, `homelabd`, or dashboard process, then starts any missing apps. Dashboard adoption first looks for the process listening on port `5173`, which lets a running UI be brought back under supervisor control without stopping the terminal page. `stack-restart` gracefully stops apps in reverse order, restarts `supervisord`, then starts apps in dependency order. `stack-stop` gracefully stops the apps and then stops `supervisord`.

If an app is marked `desired=running`, `supervisord` treats that as an invariant. On startup and on each health interval it reconciles stopped or failed desired-running apps by starting them again, so a dashboard restart cannot leave the UI down indefinitely after a successful `SIGTERM`. When a running app fails its health check, `supervisord` restarts the tracked process group instead of clearing the PID and launching a second copy.

Start, stop, and restart requests are serialised per app. Repeated API calls while an app is starting, stopping, or restarting update the desired state but do not launch a second copy of the same app.

## Configuration

Add supervised apps under `supervisord.apps` in `config.json`:

```json
{
  "supervisord": {
    "addr": "127.0.0.1:18082",
    "healthd_url": "http://127.0.0.1:18081",
    "heartbeat_interval_seconds": 5,
    "shutdown_timeout_seconds": 10,
    "state_path": "data/supervisord/state.json",
    "log_dir": "data/supervisord/logs",
    "working_dir": ".",
    "restart_command": "go",
    "restart_args": ["run", "./cmd/supervisord"],
    "apps": [
      {
        "name": "dashboard",
        "type": "web",
        "command": "bun",
        "args": ["run", "dev", "--", "--host", "0.0.0.0", "--port", "5173", "--strictPort"],
        "working_dir": "web/dashboard",
        "start_order": 30,
        "auto_start": true,
        "restart": "on_failure",
        "health_url": "http://127.0.0.1:5173/chat",
        "shutdown_timeout_seconds": 10
      }
    ]
  }
}
```

Run `supervisord` from the same environment that should run the apps. In development that means `nix develop`, because the flake provides `go`, `bun`, and the agent CLIs. The supervisor config should not contain `nix develop` wrappers; it should describe the app process itself.

`restart_command` and `restart_args` describe how `supervisord` restarts itself. In development this should re-run `go run ./cmd/supervisord` from the existing dev shell so code changes are picked up. In production it should point at the installed `supervisord` binary or a service manager wrapper.

`log_dir` stores per-app output logs. Each managed app writes `<app>.stdout.log` and `<app>.stderr.log`; stderr lines are also queued and pushed to `healthd` as application errors. The default is `data/supervisord/logs`.

Restart policies:

- `never`: do not restart after exit.
- `on_failure`: restart when the app exits non-zero.
- `always`: restart after any unexpected exit while desired state is `running`.

The dashboard dev server must use Vite `--strictPort` on port `5173`. If that port is already held by an orphaned process, the supervised dashboard should fail fast instead of silently starting on `5174` or another fallback port that the health check and browser do not use.

## API

```text
GET  /supervisord
GET  /supervisord/apps
POST /supervisord/restart
POST /supervisord/stop
POST /supervisord/apps/<name>/start
POST /supervisord/apps/<name>/stop
POST /supervisord/apps/<name>/restart
POST /supervisord/apps/<name>/adopt  {"pid":1234}
```

The dashboard uses these endpoints through `/supervisord`.

For interactive operation, prefer `homelabctl` over raw HTTP calls:

```sh
go run ./cmd/homelabctl supervisor status
go run ./cmd/homelabctl supervisor apps
go run ./cmd/homelabctl supervisor restart homelabd
go run ./cmd/homelabctl supervisor restart dashboard
go run ./cmd/homelabctl supervisor app adopt dashboard 1234
```

Use `go run ./cmd/homelabctl supervisor restart` only when the explicit intent is to restart `supervisord` itself. Add `-supervisord-addr` or `HOMELABD_SUPERVISORD_ADDR` when the control API is not on `http://127.0.0.1:18082`.

## Error Capture

Application stderr is treated as the error stream. `supervisord` appends it to the app's `*.stderr.log`, keeps the latest stderr line on the app status as `last_error`, and sends recent lines to `healthd` when `healthd_url` is configured.

Review recent captured errors through healthd:

```sh
go run ./cmd/homelabctl healthd errors
go run ./cmd/homelabctl errors -limit 20 dashboard
```

Agents can use the read-only `health.errors` tool to inspect the same data before creating follow-up tasks for root-cause fixes.

## Remote Agent App

The default config includes a `homelab-agent` app with `type: "agent"` and `auto_start: false`. This is a template for machines that should run remote work:

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

Enable it only on machines that should accept remote tasks. Set the machine's `remote_agent.workdirs` to the exact directories that may receive work, set `HOMELABD_AGENT_TOKEN`, and then change `auto_start` to `true`. Leave it disabled on the control-plane machine unless that machine should also execute remote-targeted tasks.

The agent template intentionally has no `health_url`. Its health signal is the remote-agent heartbeat accepted by `homelabd` and forwarded to healthd as `remote-agent:<agent_id>`. If the agent process is supervised on a different machine, make sure that machine can reach the control-plane `http.addr` and that the token environment matches `control_plane.agent_token_env`.

Before self-restart or stop, `supervisord` writes `state_path`. On boot it reloads that state and adopts still-running child PIDs, so managed applications continue running across supervisor replacement and remain stoppable or restartable from the UI. Existing unmanaged processes can be adopted explicitly by PID; `./run.sh stack-start` uses this during development to bring already-running `healthd`, `homelabd`, and dashboard processes under supervisor control.

## Graceful Shutdown Contract

Apps should handle `SIGTERM` and exit promptly. `supervisord` sends `SIGTERM` to the app process group, waits the configured shutdown timeout, then sends `SIGKILL` if the app is still running.

For managed apps, `supervisord` also tracks descendants discovered under the app PID and the app process group. If the launcher exits but a child process keeps running, `supervisord` force-kills the remaining tracked children before it reports the app stopped or starts a replacement. This prevents stale dashboard dev-server children from keeping port `5173` bound across restarts.

Adopted apps are stopped by PID and tracked descendants rather than by the caller's whole process group, so adopting an existing listener does not kill unrelated shell jobs that happen to share a terminal process group.

This gives daemons time to flush logs, close listeners, persist state, and release work before restart or stop, while still ensuring a restart does not race the old process for the same port.
