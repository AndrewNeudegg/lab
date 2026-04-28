# Task Workflow

`homelabd` separates merge approval from final acceptance.

## States

- `queued`: task exists and is waiting for its execution queue. Local tasks wait for the local task supervisor; remote tasks wait for the selected `homelab-agent` queue. Next transition: `queued -> running`.
- `running`: a local in-memory worker or remote agent owns the task. Next transition: `running -> ready_for_review` when it finishes, or `running -> blocked` when it fails.
- `ready_for_review`: local work is staged in the task worktree, or a remote agent result has been recorded for human acknowledgement. Local next transition: `ready_for_review -> awaiting_approval` when checks and premerge pass, `ready_for_review -> conflict_resolution` when the task branch cannot reconcile with current `main`, or `ready_for_review -> blocked` for other failures. Remote next transition: `ready_for_review -> awaiting_verification` when review acknowledges the remote result.
- `conflict_resolution`: a local task branch conflicts with current `main` and needs manual fixes in the task worktree. Next transition: `conflict_resolution -> running` after delegation, `conflict_resolution -> ready_for_review` after manual resolution, or deletion/cancellation.
- `blocked`: review or execution stopped and no worker should be running automatically. Next transition: `blocked -> running` only after an explicit `delegate`, `run`, or `reopen`.
- `awaiting_approval`: checks and premerge passed and a merge approval exists. When approval is triggered, the Orchestrator first tries to reconcile the local task branch with current `main`. Next transition: `awaiting_approval -> awaiting_verification` after approved merge, `awaiting_approval -> conflict_resolution` if auto-rebase fails, `awaiting_approval -> blocked` for other merge failures, or `awaiting_approval -> running` when an operator explicitly retries or delegates more work. Starting that new run marks the old pending approvals stale.
- `awaiting_verification`: local task merge has landed in the main repo, or remote task review acknowledged the remote result. The human still needs to verify the running app or the named remote machine/directory. Next transition: `awaiting_verification -> done` via `accept`, or `awaiting_verification -> queued` via `reopen`.
- `done`: the human accepted the merged result. Terminal state.
- `cancelled`: work was intentionally stopped. Terminal state.

## Planning Gate

Every task record carries a durable reviewed plan before execution starts. The plan is stored in the task JSON under `plan` and is visible in the `/tasks` selected-task pane as a collapsible reviewed-plan section. The default planning gate derives the plan from task metadata, so local tasks, remote tasks, and legacy graph phases get distinct summaries, steps, and risks. It records:

- a concise task-, phase-, or target-specific plan summary
- ordered execution steps for the current task, legacy graph phase, or execution target
- known risks for that phase or target before work starts
- a reviewer note confirming the plan contains the required execution stages

`homelabd` writes `task.plan.created` and `task.plan.reviewed` events to the JSONL event log. If an older task has no reviewed plan, or only has the legacy generic default plan, `run` or `delegate` creates and reviews the current task-specific plan before assigning the worker.

Reviewing a task with no workspace diff moves it to `blocked` instead of leaving it `running`; the next action should be to rerun, delegate with clearer instructions, or delete the task.

Task records include run lifecycle timestamps. `started_at` is set when a task enters `running`, and `stopped_at` is set when it leaves `running` for review, approval, verification, blocked, failed, done, or cancelled states. Reopening or rerunning a task starts a new run and clears the previous `stopped_at`.

The review gate must not silently restart a worker. If checks or diff validation fail, the task stays `blocked`; if branch reconciliation fails, it moves to `conflict_resolution`. In either case, the failure reason is stored in the task result and task activity. A human or orchestrator command must explicitly choose the next action.

Approvals are single-use decisions tied to the task state at the time they were requested. A merge approval for a task that is no longer `awaiting_approval` is stale and must not run. Retrying, delegating, assigning, reopening, refreshing, cancelling, or accepting a task stales its pending approvals; a later review also stales older pending approvals before requesting a new merge approval. If more than one pending merge approval exists for legacy data, only the newest one can run. When a merge approval is approved, the Orchestrator automatically merges current `main` into the task worktree before executing the approved merge. If that reconciliation conflicts, the approval is marked `failed`, the task moves to `conflict_resolution`, and the operator gets conflict-resolution actions instead of a raw HTTP error.

## Task Records

New local development tasks are represented by one queued task record and one isolated worktree. The task keeps the original goal, reviewed plan, lifecycle timestamps, workspace path, and final result. `homelabd` no longer expands a new task into separate inspect, design, implement, test, docs, and review queue items.

Use `show <task_id>` to inspect the task, `run <task_id>` to start the built-in coder, `delegate <task_id> to codex` to use an external worker, and `accept <task_id>` after verifying the merged result. In the dashboard, `/tasks` exposes typed buttons for run, review, approval, accept, reopen, cancel, retry, and delete; those buttons call HTTP endpoints directly rather than sending task commands through chat. Long diagnostics such as worker output, activity, the reviewed plan, and the original prompt are collapsible so they remain available without dominating the decision flow.

Older task records may still contain graph metadata from the previous workflow:

- `parent_id`: parent task for a legacy child phase.
- `depends_on`: task IDs that must be accepted before a legacy phase can run.
- `blocked_by`: currently unresolved dependency task IDs.
- `graph_phase`: legacy phase name such as `root`, `inspect`, `design`, `implement`, `test`, `docs`, or `review`.
- `acceptance_criteria`: durable checklist items for the task or legacy phase.

## Verification Commands

Use `accept <task_id>` after checking that the merged change works.

Use `reopen <task_id> <reason>` when the merged change needs more work, for example:

```text
reopen 28493611 needs rework
```

Reopening moves the task back to `queued` and preserves the reason in the task result.

For command-line operation, use `homelabctl` rather than raw HTTP calls:

```bash
go run ./cmd/homelabctl status
go run ./cmd/homelabctl task show <task_id>
go run ./cmd/homelabctl task runs <task_id>
go run ./cmd/homelabctl task diff <task_id>
go run ./cmd/homelabctl task review <task_id>
go run ./cmd/homelabctl approve <approval_id>
go run ./cmd/homelabctl task accept <task_id>
go run ./cmd/homelabctl task reopen <task_id> "needs rework"
go run ./cmd/homelabctl task delete <task_id>
```

See `docs/homelabctl.md` for the full CLI command surface and the rule that new operator workflows must keep the CLI up to date.

## Diff Review

Use `diff <task_id>` or `homelabctl task diff <task_id>` when an operator asks what a task branch changes relative to current `main`. The HTTP endpoint is `GET /tasks/{task_id}/diff`; it returns the raw patch, file stats, branch labels, refs, and per-file summaries.

The dashboard `/tasks` selected-task record renders the same data in the `Changes vs main` panel. It uses a three-dot task-branch comparison for git worktrees, matching pull-request review semantics: the diff focuses on what the task branch introduces since it diverged from `main`. The panel provides changed-file navigation plus split and unified views with line numbers, wrapped long lines, and inline changed-text highlights.

Natural questions like `what is the diff between c01777ee and main?` are handled by the Orchestrator as program commands. The reply gives a compact summary and points to the dashboard or `homelabctl task diff`; it should not fall back to an LLM that lacks repository access.

## Restart Recovery

On startup, `homelabd` scans durable task records. Any task still marked `running` is treated as interrupted in-memory work and is automatically resumed:

- tasks assigned to `codex`, `claude`, or `gemini` restart on the same external backend
- tasks assigned to `CoderAgent` restart through the built-in coder loop
- tasks assigned to `UXAgent` restart through the built-in UX loop so UI research, tests, and browser-UAT expectations are preserved
- tasks assigned to `OrchestratorAgent` prefer `codex` when it is configured, otherwise they use `CoderAgent`

If a legacy task is still marked `running` but already has a granted `git.merge_approved` approval, recovery treats the merge as landed and moves the task to `awaiting_verification` instead of starting another worker.

Remote tasks are excluded from local restart recovery. A remote task stays in its target queue for the selected `homelab-agent`, and a running remote task is completed or failed only by that agent's completion callback.

Recovery decisions are written to the JSONL event log as `task.recovery.*` events and to the daemon logs with structured `slog` fields including task ID, short ID, title, workspace, strategy, and backend.

## Agent Completion Expectations

When a task changes user-facing behavior, commands, UI, configuration, tools, or workflow, the worker should update relevant docs or help text in the same patch.

When an external coding worker finishes a local task, `homelabd` automatically runs the review gate. The review gate runs project checks, verifies the task branch can merge cleanly into the current repository state, stales any older pending approvals for the task, and only then creates a merge approval. A task branch that cannot merge cleanly moves to `conflict_resolution` with an explicit premerge failure; approval is not created, no worker is restarted implicitly, and the main repository must not be left in a conflicted state.

When a remote agent finishes, `homelabd` records the remote result and moves the task to `ready_for_review`. Reviewing a remote task acknowledges the result and moves it to `awaiting_verification`; it does not run local project checks, compare local and remote `HEAD`, create a merge approval, or touch the control-plane checkout.

External worker completions are ignored once the task has advanced to merge approval, merged verification, done, or cancelled. Remote completion callbacks are accepted only while the matching remote task is still `running`. This prevents a stale background worker from moving an already merged or accepted task back to review.

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

Local review reports a restart impact line from the diff. Runtime, supervisor, healthd, and dashboard paths are mapped to their supervised components so the merge reply can carry a restart plan into final verification. Accept the task only after the named components have been restarted or verified as hot-reloaded. Remote review cannot infer restart impact from the control-plane diff; verify the named remote machine and directory directly before accepting.
