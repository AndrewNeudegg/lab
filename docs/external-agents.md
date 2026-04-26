# External Agent Backends

`homelabd` can delegate a task to a headless CLI worker such as Codex, Claude, or Gemini.
The CLI runs inside the task worktree, so any file edits stay isolated until the normal
`review` and `approve` merge flow.

## Commands

```text
agents
delegate <task_id> <backend> <instruction>
codex <task_id> <instruction>
claude <task_id> <instruction>
gemini <task_id> <instruction>
diff <task_id>
review <task_id>
```

Example:

```text
new add structured logging to backup service
codex task_20260424_001 inspect the backup service, make a minimal patch, and run relevant tests
diff task_20260424_001
review task_20260424_001
approve approval_...
```

`diff <task_id>` uses the dedicated task diff API. In chat it returns a compact summary; `homelabctl task diff <task_id>` prints the raw patch; the dashboard task record shows the same comparison in the highlighted `Changes vs main` panel with split and unified views.

## Configuration

The default config reads CLI commands from environment variables:

```text
CODEX_CMD=codex
CLAUDE_CMD=claude
GEMINI_CMD=gemini
```

`CODEX_CLI`, `CLAUDE_CLI`, and `GEMINI_CLI` are also accepted. `config.json` can override
the command, args, timeout, and description:

```json
{
  "external_agents": {
    "codex": {
      "enabled": true,
      "command": "codex",
      "args": ["exec", "--skip-git-repo-check"],
      "timeout_seconds": 900
    }
  }
}
```

## Safety Model

- `agent.list` is read-only.
- `agent.delegate` is medium risk and must be scoped to a task workspace.
- External CLIs may modify the task worktree, but they do not get approval to merge.
- The human approval gate still controls merges through `review` and `approve`.
- Configure exact CLI args per backend; provider-specific headless flags can differ.

## Run Trace

Each delegation has a stable run id. `homelabd` passes it to the worker as
`HOMELABD_EXTERNAL_RUN_ID` and streams stdout/stderr chunks into task events as
`agent.delegate.output` while the worker is still running.

When the worker exits, `homelabd` writes a run artifact under `data/runs/<run_id>.json`
with the backend, command, workspace, final output, error text, duration, and status. The
task still moves through the normal ready-for-review, review, and approval flow.
