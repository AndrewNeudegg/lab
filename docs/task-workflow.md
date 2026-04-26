# Task Workflow

`homelabd` separates merge approval from final acceptance.

## States

- `queued`: task exists and is waiting for the supervisor to assign a worker. Next transition: `queued -> running`.
- `running`: an agent or external worker owns the task. Next transition: `running -> ready_for_review` when it finishes, or `running -> blocked` when it fails.
- `ready_for_review`: work is staged in the task worktree, but the review gate has not passed. Next transition: `ready_for_review -> awaiting_approval` when checks and premerge pass, `ready_for_review -> conflict_resolution` when the task branch cannot reconcile with current `main`, or `ready_for_review -> blocked` for other failures.
- `conflict_resolution`: the task branch conflicts with current `main` and needs manual fixes in the task worktree. Next transition: `conflict_resolution -> running` after delegation, `conflict_resolution -> ready_for_review` after manual resolution, or deletion/cancellation.
- `blocked`: review or execution stopped and no worker should be running automatically. Next transition: `blocked -> running` only after an explicit `delegate`, `run`, or `reopen`.
- `awaiting_approval`: checks and premerge passed and a merge approval exists. When approval is triggered, the Orchestrator first tries to reconcile the task branch with current `main`. Next transition: `awaiting_approval -> awaiting_verification` after approved merge, `awaiting_approval -> conflict_resolution` if auto-rebase fails, or `awaiting_approval -> blocked` for other merge failures.
- `awaiting_verification`: the merge has landed in the main repo, but the human has not verified the result in the running app yet. Next transition: `awaiting_verification -> done` via `accept`, or `awaiting_verification -> queued` via `reopen`.
- `done`: the human accepted the merged result. Terminal state.
- `cancelled`: work was intentionally stopped. Terminal state.

## Planning Gate

Every task record carries a durable reviewed plan before execution starts. The plan is stored in the task JSON under `plan` and is also visible in the `/tasks` selected-task pane above the original input. The default planning gate records:

- a concise plan summary
- ordered execution steps covering inspection, minimal workspace change, validation, and handoff
- known risks before inspection completes
- a reviewer note confirming the plan contains the required execution stages

`homelabd` writes `task.plan.created` and `task.plan.reviewed` events to the JSONL event log. If an older task has no reviewed plan, `run` or `delegate` creates and reviews one before assigning the worker.

Reviewing a task with no workspace diff moves it to `blocked` instead of leaving it `running`; the next action should be to rerun, delegate with clearer instructions, or delete the task.

Task records include run lifecycle timestamps. `started_at` is set when a task enters `running`, and `stopped_at` is set when it leaves `running` for review, approval, verification, blocked, failed, done, or cancelled states. Reopening or rerunning a task starts a new run and clears the previous `stopped_at`.

The review gate must not silently restart a worker. If checks or diff validation fail, the task stays `blocked`; if branch reconciliation fails, it moves to `conflict_resolution`. In either case, the failure reason is stored in the task result and task activity. A human or orchestrator command must explicitly choose the next action.

Approvals are single-use decisions tied to the task state at the time they were requested. A merge approval for a task that is no longer `awaiting_approval` is stale and must not run. When a merge approval is approved, the Orchestrator automatically merges current `main` into the task worktree before executing the approved merge. If that reconciliation conflicts, the approval is marked `failed`, the task moves to `conflict_resolution`, and the operator gets conflict-resolution actions instead of a raw HTTP error.

## Task Graphs

New development tasks are represented as a graph. The root task keeps the original goal and durable acceptance criteria. `homelabd` creates six child phase tasks under that root:

1. `inspect`
2. `design`
3. `implement`
4. `test`
5. `docs`
6. `review`

The first child phase is `queued`. Later phases start as `blocked` with `depends_on` and `blocked_by` pointing at the previous phase. A worker can only run a child phase when all dependency tasks are `done`. If an operator tries to run or delegate a blocked phase early, homelabd records the blockers and refuses execution.

Use `accept <child_task_id>` after verifying a phase result. Accepting a child marks its acceptance criteria as accepted and releases any dependent child whose blockers are now done. When all child phases are accepted, the parent task is marked `done`.

Task records now include:

- `parent_id`: parent graph task, when this task is a child phase.
- `depends_on`: task IDs that must be accepted before this task can run.
- `blocked_by`: currently unresolved dependency task IDs.
- `graph_phase`: `root`, `inspect`, `design`, `implement`, `test`, `docs`, or `review`.
- `acceptance_criteria`: durable checklist items for the task or phase.

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
```

See `docs/homelabctl.md` for the full CLI command surface and the rule that new operator workflows must keep the CLI up to date.

## Diff Review

Use `diff <task_id>` or `homelabctl task diff <task_id>` when an operator asks what a task branch changes relative to current `main`. The HTTP endpoint is `GET /tasks/{task_id}/diff`; it returns the raw patch, file stats, branch labels, refs, and per-file summaries.

The dashboard `/tasks` selected-task record renders the same data in the `Changes vs main` panel. It uses a three-dot task-branch comparison for git worktrees, matching pull-request review semantics: the diff focuses on what the task branch introduces since it diverged from `main`. The panel provides changed-file navigation plus split and unified views with line numbers and inline changed-text highlights.

Natural questions like `what is the diff between c01777ee and main?` are handled by the Orchestrator as program commands. The reply gives a compact summary and points to the dashboard or `homelabctl task diff`; it should not fall back to an LLM that lacks repository access.

## Restart Recovery

On startup, `homelabd` scans durable task records. Any task still marked `running` is treated as interrupted in-memory work and is automatically resumed:

- tasks assigned to `codex`, `claude`, or `gemini` restart on the same external backend
- tasks assigned to `CoderAgent` restart through the built-in coder loop
- tasks assigned to `OrchestratorAgent` prefer `codex` when it is configured, otherwise they use `CoderAgent`

Recovery decisions are written to the JSONL event log as `task.recovery.*` events and to the daemon logs with structured `slog` fields including task ID, short ID, title, workspace, strategy, and backend.

## Agent Completion Expectations

When a task changes user-facing behavior, commands, UI, configuration, tools, or workflow, the worker should update relevant docs or help text in the same patch.

When an external coding worker finishes, `homelabd` automatically runs the review gate. The review gate runs project checks, verifies the task branch can merge cleanly into the current repository state, and only then creates a merge approval. A task branch that cannot merge cleanly moves to `conflict_resolution` with an explicit premerge failure; approval is not created, no worker is restarted implicitly, and the main repository must not be left in a conflicted state.

External worker completions are ignored once the task has advanced to merge approval, merged verification, done, or cancelled. This prevents a stale background worker from moving an already merged or accepted task back to review.

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
