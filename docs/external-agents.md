# External Agent Backends

`homelabd` can delegate a task to a headless CLI worker such as Codex, Claude, or Gemini.
For local tasks, the CLI runs inside the task worktree, so any file edits stay isolated until the normal `review` and `approve` merge flow. For remote tasks, `homelab-agent` runs the same backend command in the selected advertised remote directory; the control plane records the result but does not create a local worktree or merge approval.

Built-in role agents are separate from external CLI backends. `UXAgent` is invoked with `ux <task_id> [instruction]` or `delegate <task_id> to ux ...`; it uses the local LLM provider and the same worktree/review flow, not an entry in `external_agents`.

## Commands

```text
agents
delegate <task_id> <backend> <instruction>
delegate <task_id> to ux <instruction>
codex <task_id> <instruction>
claude <task_id> <instruction>
gemini <task_id> <instruction>
ux <task_id> [instruction]
diff <task_id>
review <task_id>
```

Example:

```text
new add structured logging to backup service
codex task_20260424_001 inspect the backup service, make a minimal patch, and run relevant tests
ux task_20260424_001 audit accessibility, responsive behaviour, and browser UAT
diff task_20260424_001
review task_20260424_001
approve approval_...
```

The `agents` command lists external backend definitions, not remote machines. Remote machines are listed with `homelabctl agent list` or on the dashboard Tasks page.

`diff <task_id>` uses the dedicated task diff API. In chat it returns a compact summary; `homelabctl task diff <task_id>` prints the raw patch; the dashboard task record shows the same comparison in the highlighted `Changes vs main` panel with split and unified views.

## Configuration

The default config reads CLI commands from environment variables:

```text
CODEX_CMD=codex
CLAUDE_CMD=claude
GEMINI_CMD=gemini
```

`CODEX_CLI`, `CLAUDE_CLI`, and `GEMINI_CLI` are also accepted. The default Codex backend is configured for trusted task worktrees: it passes `--dangerously-bypass-approvals-and-sandbox` and `CODEX_UNSAFE_ALLOW_NO_SANDBOX=1` so Codex does not remount `.git` read-only inside the already-isolated worktree. `config.json` can override the command, args, environment, timeout, and description:

```json
{
  "external_agents": {
    "codex": {
      "enabled": true,
      "command": "codex",
      "args": ["--dangerously-bypass-approvals-and-sandbox", "exec", "--skip-git-repo-check"],
      "env": {
        "CODEX_UNSAFE_ALLOW_NO_SANDBOX": "1"
      },
      "timeout_seconds": 18000
    }
  }
}
```

The default external-agent timeout is `18000` seconds, or five hours. Set
`timeout_seconds` per backend to shorten or extend a specific CLI worker.

On NixOS, keep `repo.root` and `repo.workspace_root` in a normal writable path such as `/home/lab/lab` and `/home/lab/lab/workspaces`, not `/nix/store`. The `homelabd` process that creates local task worktrees must be able to write the repository's Git common directory (`.git`, including `refs` and `worktrees`) and the workspace root. If an outer service sandbox uses bubblewrap or systemd bind mounts, do not mount `.git` read-only for the host-side `homelabd` process; create the task worktree before entering any stricter worker sandbox.

## Safety Model

- `agent.list` is read-only.
- `agent.delegate` is medium risk and must be scoped to a local task workspace.
- External CLIs may modify the local task worktree, but they do not get approval to merge.
- The human approval gate still controls local merges through `review` and `approve`.
- Remote agents use these backend commands in their selected remote workdir. Remote review acknowledges the reported result and moves the task to verification; it does not compare or merge the remote checkout with the control-plane repo.
- Configure exact CLI args per backend; provider-specific headless flags can differ.
- Browser UAT must run against an isolated dev server from the task worktree or remote workdir. Agents must not restart the production `dashboard`, `homelabd`, `healthd`, or `supervisord` stack as part of validation.

For dashboard task-page changes, the standard completion gate is:

```bash
nix develop -c bun run --cwd web uat:tasks
```

For broad dashboard shell, navigation, theme, terminal, docs, workflow, health, or supervisor changes, use:

```bash
nix develop -c bun run --cwd web uat:site
```

These commands start Playwright and Vite in the worker checkout, use mocked APIs, and leave the supervised dashboard alone. If `browser:preflight` fails on a remote worker, fix the worker browser environment or move the task to a browser-capable worker rather than restarting production services. See `docs/agentic-testing.md`.

## Run Trace

Each delegation has a stable run id. `homelabd` passes it to the worker as
`HOMELABD_EXTERNAL_RUN_ID` and streams stdout/stderr chunks into task events as
`agent.delegate.output` while the worker is still running.

When the worker exits, `homelabd` writes a run artifact under `data/runs/<run_id>.json`
with the backend, command, workspace, final output, error text, duration, and status. The
task still moves through the normal ready-for-review, review, and approval flow.
