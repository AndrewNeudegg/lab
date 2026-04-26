# Task Workflow

`homelabd` separates merge approval from final acceptance.

## States

- `queued`: task exists and is waiting for the supervisor to assign a worker. Next transition: `queued -> running`.
- `running`: an agent or external worker owns the task. Next transition: `running -> ready_for_review` when it finishes, or `running -> blocked` when it fails.
- `ready_for_review`: work is staged in the task worktree, but the review gate has not passed. Next transition: `ready_for_review -> awaiting_approval` when checks and premerge pass, or `ready_for_review -> blocked` when they fail.
- `blocked`: review or execution stopped and no worker should be running automatically. Next transition: `blocked -> running` only after an explicit `delegate`, `run`, or `reopen`.
- `awaiting_approval`: checks and premerge passed and a merge approval exists. Next transition: `awaiting_approval -> awaiting_verification` after approved merge, or `awaiting_approval -> blocked` if merge fails.
- `awaiting_verification`: the merge has landed in the main repo, but the human has not verified the result in the running app yet. Next transition: `awaiting_verification -> done` via `accept`, or `awaiting_verification -> queued` via `reopen`.
- `done`: the human accepted the merged result. Terminal state.
- `cancelled`: work was intentionally stopped. Terminal state.

Reviewing a task with no workspace diff moves it to `blocked` instead of leaving it `running`; the next action should be to rerun, delegate with clearer instructions, or delete the task.

Task records include run lifecycle timestamps. `started_at` is set when a task enters `running`, and `stopped_at` is set when it leaves `running` for review, approval, verification, blocked, failed, done, or cancelled states. Reopening or rerunning a task starts a new run and clears the previous `stopped_at`.

The review gate must not silently restart a worker. If checks, diff validation, or premerge fail, the task stays `blocked` with the failure reason in the task result and task activity. A human or orchestrator command must explicitly choose the next action.

Approvals are single-use decisions tied to the task state at the time they were requested. A merge approval for a task that is no longer `awaiting_approval` is stale and must not run. If an approved merge fails because the branch has become unmergeable, the approval is marked `failed`, the task moves to `blocked`, and the operator gets rework actions instead of a raw HTTP error.

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

When an external coding worker finishes, `homelabd` automatically runs the review gate. The review gate runs project checks, verifies the task branch can merge cleanly into the current repository state, and only then creates a merge approval. A task branch that cannot merge cleanly is blocked with an explicit premerge failure; approval is not created, no worker is restarted implicitly, and the main repository must not be left in a conflicted state.

Final task summaries should include:

- changed files
- validation run
- how to use the change
- docs updated, or why no docs change was needed

## Git Agent Tools

Agents can inspect repository state with `git.status`, `git.diff`, `git.branch`, `git.describe`, `git.log`, and `git.show`.

Write workflow tools are available for explicit git operations:

- `git.commit` stages selected paths or all changes and creates a commit
- `git.revert` reverts a commit, optionally with `--no-commit`
- `git.merge` merges a branch or commit into the current branch

These write tools are high-risk and approval-gated by default. Task review still uses `git.merge_approved` for the normal reviewed-task merge path.

## Shell Agent Tools

`shell.run_limited` executes only allowlisted command arrays without shell expansion. Read-only or routine build/test commands remain low risk. Potentially destructive allowlisted commands, including `rm`, `rmdir`, `mv`, `cp`, `git clean`, `git reset`, `git restore`, `git rm`, and `git checkout -- <path>`, are classified as high risk by the tool policy and create an approval request before execution.

Review pending shell requests with `approvals`, then use `approve <approval_id>` or `deny <approval_id>`.

## Restart impact

Review now reports a restart impact line from the diff. Runtime, supervisor, healthd, and dashboard paths are mapped to their supervised components so the merge reply can carry a restart plan into final verification. Accept the task only after the named components have been restarted or verified as hot-reloaded.
