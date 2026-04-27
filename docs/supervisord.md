# supervisord

`supervisord` is the local process supervisor for homelab runtime components.

It owns three jobs:

- start configured applications in `start_order`
- stop and restart them with graceful `SIGTERM` before forced `SIGKILL`
- publish its own heartbeat to `healthd`

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

This gives daemons time to flush logs, close listeners, persist state, and release work before restart or stop.
