# Task Workflow

`homelabd` separates merge approval from final acceptance.

## States

- `running`: an agent or external worker is actively working.
- `ready_for_review`: work is staged in the task worktree and can be reviewed.
- `awaiting_approval`: checks passed and a merge approval exists.
- `awaiting_verification`: the merge has landed in the main repo, but the human has not verified the result in the running app yet.
- `done`: the human accepted the merged result.
- `blocked`: work needs intervention or rework.
- `cancelled`: work was intentionally stopped.

Reviewing a task with no workspace diff moves it to `blocked` instead of leaving it `running`; the next action should be to rerun, delegate with clearer instructions, or delete the task.

Task records include run lifecycle timestamps. `started_at` is set when a task enters `running`, and `stopped_at` is set when it leaves `running` for review, approval, verification, blocked, failed, done, or cancelled states. Reopening or rerunning a task starts a new run and clears the previous `stopped_at`.

## Verification Commands

Use `accept <task_id>` after checking that the merged change works.

Use `reopen <task_id> <reason>` when the merged change needs more work, for example:

```text
reopen 28493611 needs rework
```

Reopening moves the task back to `running` and preserves the reason in the task result.

## Restart Recovery

On startup, `homelabd` scans durable task records. Any task still marked `running` is treated as interrupted in-memory work and is automatically resumed:

- tasks assigned to `codex`, `claude`, or `gemini` restart on the same external backend
- tasks assigned to `CoderAgent` restart through the built-in coder loop
- tasks assigned to `OrchestratorAgent` prefer `codex` when it is configured, otherwise they use `CoderAgent`

Recovery decisions are written to the JSONL event log as `task.recovery.*` events and to the daemon logs with structured `slog` fields including task ID, short ID, title, workspace, strategy, and backend.

## Agent Completion Expectations

When a task changes user-facing behavior, commands, UI, configuration, tools, or workflow, the worker should update relevant docs or help text in the same patch.

Final task summaries should include:

- changed files
- validation run
- how to use the change
- docs updated, or why no docs change was needed
