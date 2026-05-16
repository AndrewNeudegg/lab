# Remote Agents

`homelabd` acts as the control plane for local and remote workers. A remote machine runs `homelab-agent`, which phones home to the `homelabd` HTTP API, advertises the exact directories it can work in, polls only for tasks assigned to that agent, runs the configured external worker command in the selected directory, and reports the result back to the task store.

The design intentionally treats remote machines as worker nodes:

- agent identity is stable and explicit (`agent_id`, machine, service instance)
- heartbeats drive readiness and health
- tasks are routed by target queue, not by a global repo
- Git-backed remote tasks use isolated task worktrees and merge approvals, matching local task mechanics
- remote review compares against the advertised remote checkout, never against `homelabd`'s local `main`

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

The dashboard task page lists registered project workspaces and can create work as `Auto route`, `Local homelabd`, or `Remote project`. CLI usage is also available. Use `workspace list` to see the same inventory that the coordinator uses. Use `--project` for a project-level auto route, `--agent` plus `--workdir` for a specific remote queue, or `--workdir-path` only when the advertised path is the stable identifier.

```sh
homelabctl -addr http://127.0.0.1:18080 agent list
homelabctl -addr http://127.0.0.1:18080 workspace list
homelabctl -addr http://127.0.0.1:18080 task new --project remote1 "Build the reporting widget"
homelabctl -addr http://127.0.0.1:18080 task new --agent workstation --workdir repo "Update the service in this checkout"
```

The task page separates queues by execution target:

- `All queues`
- `Local homelabd`
- one queue per registered remote agent

When creating a remote task, the dashboard shows the selected project, agent, machine, and full working directory path beside the project selector. If the target looks wrong, choose another project before creating the task. The API also resolves the selected workdir against the agent's advertised workdirs; an unknown workdir id/path is rejected instead of falling back to another checkout.

## Execution Targets

Every task and Goal can carry an execution target:

- `mode: "local"`: always use the local `homelabd` checkout. Use this for control-plane and self-improvement work.
- `mode: "remote"`: run in one advertised remote project workspace.
- `mode: "auto"`: let the coordinator choose. If one remote workspace is registered it is selected; if several exist, the goal text or `project_id` must clearly identify one. Ambiguous work is rejected instead of being sent to the wrong repository.

```mermaid
flowchart LR
  Request[Task or Goal action] --> Target{Target mode}
  Target -->|local| Local[Local homelabd task queue]
  Target -->|remote| Match[Validate advertised agent/workdir]
  Target -->|auto| Route[Route by project, labels, repo, and goal text]
  Route -->|homelabd/self-improvement| Local
  Route -->|single or clear remote match| Match
  Route -->|multiple unclear remotes| Ask[Reject with project selection prompt]
  Match --> Remote[Remote agent claim queue]
```

Remote workspaces include `project_id`, repository URL, branch, labels, and metadata. The coordinator copies that context into the task target, the remote assignment, and the worker instruction so a remote worker can name cross-project dependencies instead of guessing. If a task depends on another repository, the remote result should state the dependency, expected commit/version/API, and coordination order.

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

The agent uses the `external_agents` command for the assigned backend, defaulting to `codex`. For an advertised Git checkout that is reachable by the control plane, `homelabd` first creates an isolated worktree under a sibling `.homelabd-worktrees/<agent>/<project>/` directory and sends that task worktree to the remote agent. The advertised checkout must be clean before task creation or claim; dirty checkouts are blocked so prior uncommitted work cannot disappear into the next task baseline. For non-Git or inaccessible paths, the agent uses the advertised directory and the older evidence-only review path.

The agent executes in the selected task directory, sends stdout/stderr back as the task result, and captures the remote git working-tree patch after completion. The captured patch includes uncommitted tracked changes and untracked files, excluding ignored runtime state such as `.agent-*`. Before calling back to `homelabd`, the agent writes the completion payload to `data/remote-agent/pending-completions/<agent_id>/` under its configured `data_dir`. If `homelabd` is restarting, offline, or returns a transient network error, the remote agent keeps advertising that task as current and replays the saved completion before claiming new work. A successful callback deletes the pending completion file. While a remote task is `running`, the control plane must not recover it with a local worker simply because there is no in-memory local worker handle; the remote agent's heartbeat, timeout, and pending-completion replay own that lifecycle. The same backend `timeout_seconds` applies to remote task execution; omitted or zero values default to 3,600 seconds, or 1 hour. If the worker reaches that deadline, the remote task is recorded as `timed_out` rather than `blocked`. The task supervisor treats `timed_out` as a bounded automatic recovery candidate, requeues the same remote target with timeout-specific instructions, and only exposes a manual blocker after recovery attempts are exhausted. The default Codex backend disables Codex's own sandbox because tasks already run in isolated worktrees and Codex otherwise may remount `.git` read-only, which prevents Git worktree metadata updates.

For tasks completed by an older agent that lost the callback before writing a pending completion file, use `homelabctl task recover-remote <task_id>` only when the remote workdir is accessible from the control plane. Pass `--base-ref` with the previous remote task snapshot tree or commit whenever possible so the recovered diff is task-scoped instead of a broad live checkout fallback.

Use `external_agents.<backend>.wrapper_command` and `wrapper_args` on each remote agent when a repository needs a custom shell before the CLI starts. The wrapper is agent-local configuration, not goal text. It receives `HOMELABD_WORKSPACE` and the backend command as arguments, so one machine can run a Nix-aware wrapper, another can use a VM bootstrap script, and a third can execute the CLI directly. A NixOS remote can use a repository script such as `/home/lab/remote1/scripts/agent-env`:

```json
{
  "external_agents": {
    "codex": {
      "command": "/nix/store/.../bin/codex",
      "args": ["--dangerously-bypass-approvals-and-sandbox", "exec", "--skip-git-repo-check"],
      "wrapper_command": "/home/lab/remote1/scripts/agent-env",
      "wrapper_args": []
    }
  }
}
```

Remote agents do not need to run in this repository. Each advertised `workdir` can be a different checkout, a different project, or a non-git directory. For Git checkouts that are reachable from the control plane, `homelabd` treats the advertised path as that project's merge target and uses the same worktree, branch, review, merge approval, and verification lifecycle as local tasks. It still does not assume that remote path has the same HEAD, branch, or repository root as the control-plane checkout.

## Per-Agent Review Automation

Remote tasks are controlled per agent. Configure each agent's policy from `/tasks` or with:

```bash
go run ./cmd/homelabctl settings remote-agent remote1 auto-review on
go run ./cmd/homelabctl settings remote-agent remote1 auto-merge on
go run ./cmd/homelabctl settings remote-agent remote1 auto-review-merge on
```

For Git-backed remote tasks, `auto-review` lets the supervisor run the normal merge-queue review when that agent's task reaches `ready_for_review`, and `auto-merge` grants the resulting merge approval after checks pass. The task still moves to `awaiting_verification` before final acceptance, matching local tasks. For legacy evidence-only remotes, `auto-review` runs the independent remote review and `auto-merge` accepts the reviewed result because there is no reachable Git target to merge.

Set `remote_agent.workdirs` explicitly on each worker. If no workdirs are configured, `homelab-agent` falls back to the configured `repo.root`, which is useful for local development but too easy to point at the wrong tree on a real machine.

```json
{
  "remote_agent": {
    "workdirs": [
      {
        "id": "remote1",
        "path": "/home/lab/remote1",
        "label": "Remote 1",
        "project_id": "remote1",
        "repo_url": "git@example.com:remote1.git",
        "branch": "main",
        "labels": ["uat", "node"]
      }
    ]
  }
}
```

Remote workers may include Mermaid fenced diagrams in reported results or docs when a workflow, state machine, architecture, sequence, or user journey would be clearer visually. Chat and dashboard docs render those diagrams with the homelabd brand theme and strip Mermaid init directives. Do not add Mermaid `init` blocks or hard-code unrelated colours; use the palette in `docs/chat-commands.md` when explicit semantic styling is unavoidable.

## Remote Testing

Remote agents validate in the selected remote workdir. They must not call the control-plane supervisor to restart production services, and they must not assume `127.0.0.1:5173` points at the operator's dashboard.

When remote handoffs describe multi-machine flows, state transitions, or verification paths, include a concise Mermaid diagram if it improves understanding. Diagrams must use the homelabd brand palette from `docs/diagramming-and-brand-colours.md`; the dashboard applies those colours automatically to Mermaid fences.

For focused UI/UX review in a remote checkout, run:

```sh
nix develop -c bun run --cwd web uat:ui
```

For dashboard task-page changes in a remote checkout, run:

```sh
nix develop -c bun run --cwd web uat:tasks
```

For broad dashboard shell, navigation, theme, terminal, docs, workflow, health, or supervisor changes, run:

```sh
nix develop -c bun run --cwd web uat:site
```

These commands start a Playwright-managed Vite server on that remote machine, choose a per-worktree port unless `PLAYWRIGHT_PORT` is set, and mock `homelabd` APIs. `uat:ui` runs desktop and mobile accessibility plus visual-baseline checks, and `uat:site` also mocks `healthd` and `supervisord`, covers every primary page on desktop and mobile, and attaches screenshots. The remote completion summary should include the command, the generated local URL when relevant, and whether Chromium came from `CHROME_BIN` or a Playwright browser install. If `browser:preflight` fails because the remote sandbox cannot launch Chromium, report that infrastructure failure instead of touching production services.

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
- `GET /workspaces` lists the project workspace inventory derived from remote-agent heartbeats.
- `GET /agents/{id}` returns one registered agent.
- `POST /agents/{id}/heartbeat` registers or refreshes an agent. `POST /agents` also accepts a heartbeat body with `id`.
- `POST /agents/{id}/claim` claims the next queued task targeted to that agent.
- `POST /agents/{id}/tasks/{task_id}/complete` records completion. Remote agents should report `completed`, `no_change_required`, `failed`, `blocked`, or `timed_out`. Duplicate completion posts for an already-recorded remote task are idempotent so an agent can delete a replayed pending completion after a coordinator restart.
- `POST /tasks` accepts an optional `target` object with `mode: "auto"`, `"local"`, or `"remote"`, plus `project_id`, `agent_id`, `workdir_id`, advertised `workdir`, `repo_url`, `branch`, labels, and `backend`.
- `POST /tasks/{task_id}/assign` retargets a non-terminal task to a remote agent and advertised workdir.

Remote tasks intentionally skip the local worker supervisor. The selected remote agent owns execution until it reports completion or failure. Git-backed remote tasks still enter the shared review/merge queue after completion so their task branch is committed, checked, and merged like local work.

## Review Semantics

Local tasks use isolated worktrees, checks, diff review, and merge approval. Git-backed remote tasks use the same mechanism, but the merge target is the advertised remote checkout rather than the control-plane repository. For a Git-backed remote task:

1. `homelabd` refuses to start if the advertised checkout is dirty.
2. `homelabd` creates `homelabd/<task_id>` in an isolated worktree outside the checkout.
3. The remote agent runs the worker in that task worktree.
4. The remote agent reports output, validation, and the task-scoped diff back to `homelabd`.
5. `review <task>` commits the task worktree, reconciles it with the advertised checkout `HEAD`, runs configured checks, stores an immutable diff snapshot, and creates a merge approval.
6. Approving the merge merges the task branch into the advertised checkout and moves the task to `awaiting_verification`.
7. `accept <task>` closes it, or `reopen <task> <reason>` queues more remote work.

Legacy remote tasks for non-Git or inaccessible directories keep the evidence-only path: the remote agent runs in the advertised directory, reports a captured diff when available, and `review <task>` can move the task to `awaiting_verification`, `no_change_required`, or `blocked` without a merge approval.

For Autopilot Goals, Git-backed remote tasks use the same review and merge queue as local tasks, then the Goal policy can accept the verified task after merge. Legacy evidence-only remote review still uses the independent-review transition. If a remote Goal task times out before a complete result, Autopilot keeps ownership and queues automatic timeout recovery on the same remote agent instead of turning the Goal into a human blocker. If a remote Goal task is already blocked by `ReviewerAgent could not verify remote Goal progress:`, Autopilot re-runs review before exposing the blocker to the operator; recovered validation or parser fixes let the Goal continue without a manual decision. If the blocked remote report produced actionable open gaps and no operator questions, the supervisor treats the task as agent-owned repair evidence, marks it consumed, clears it from the current Goal slot, and creates the next `gap_fix` task. The next `accept_task` gate can then close ordinary successful tasks or pause/block according to the Goal policy. A worker `GOAL_REPORT` is treated as a claim, not proof: the stored Goal task report includes a reviewer decision such as `verified_progress`, `needs_validation`, `misaligned`, `insufficient_evidence`, or `no_change`.

Use `homelabctl task diff <task_id>` or the dashboard `Changes vs main` panel to inspect the task diff snapshot. Reviewed Git-backed remote tasks store the same immutable branch-vs-target snapshot as local tasks, so the historical patch remains visible after merge. Legacy remote completions store the task-scoped snapshot reported by the agent when available. The structured diff response includes `source`, `snapshot`, `captured_at`, and `sha256` provenance. If a completion predates task-scoped snapshots and the workdir path is still accessible from the control plane, `GET /tasks/{task_id}/diff` may compute a live remote working-tree fallback and marks it with a warning. If neither source is available, the task still shows the remote result and validation, but the diff panel tells the operator that no remote diff is available.

Build Goals should maintain a durable feature or parity matrix in the target repository. The worker prompt asks the agent to create or update that matrix, select one concrete slice, and report the files, validation, remaining gaps, blockers, and questions in the final `GOAL_REPORT:` line. This gives the supervisor a product scorecard instead of a stream of broad “continue building” tasks.
