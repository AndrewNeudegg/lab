# Task Workflow

`homelabd` separates merge approval from final acceptance.

## States

- `queued`: task exists and is waiting for its execution queue. Local tasks wait for the local task supervisor; remote tasks wait for the selected `homelab-agent` queue. Next transition: `queued -> running`.
- `running`: a local in-memory worker or remote agent owns the task. Next transition: `running -> ready_for_review` when it finishes, or `running -> blocked` when it fails.
- `ready_for_review`: local work is staged in the task worktree, or a remote agent result has been recorded for human acknowledgement. The task supervisor runs review automatically for local tasks in this state. Local next transition: `ready_for_review -> awaiting_approval` when checks and premerge pass, `ready_for_review -> conflict_resolution` when the task branch cannot reconcile with current `main`, or `ready_for_review -> blocked` for other failures. Remote next transition: `ready_for_review -> awaiting_verification` when review acknowledges the remote result.
- `conflict_resolution`: a local task branch conflicts with current `main` and needs fixes in the task worktree. The task supervisor queues automatic recovery with the preferred worker, up to three attempts with a short cooldown. Next transition: `conflict_resolution -> running` through automatic recovery, retry, or delegation; `conflict_resolution -> ready_for_review` after manual resolution; or deletion/cancellation.
- `blocked`: review or execution stopped. Retryable review-check, premerge, rebase, and merge failures are automatically requeued by the task supervisor with bounded attempts. Dependency blocks and exhausted automatic recovery remain blocked for an operator decision. Next transition: `blocked -> running` through automatic recovery, `retry`, `delegate`, `run`, or `reopen`.
- `awaiting_approval`: checks and premerge passed and a merge approval exists. When approval is triggered, the Orchestrator first tries to reconcile the local task branch with current `main`. If an `awaiting_approval` task has no pending merge approval, the task supervisor requeues review so a fresh approval can be produced. Next transition: `awaiting_approval -> awaiting_restart` when the reviewed diff requires supervised component restarts, `awaiting_approval -> awaiting_verification` when no restart is required, `awaiting_approval -> conflict_resolution` if auto-rebase fails, `awaiting_approval -> blocked` for other merge failures, or `awaiting_approval -> running` when a worker retry starts. Starting that new run marks the old pending approvals stale.
- `awaiting_restart`: the merge has landed and `homelabd` is restarting required supervised components through `supervisord`. The task cannot be accepted while this gate is pending or running. Each component must restart and pass its configured health URL before the task moves to verification; a failed restart leaves the task in `awaiting_restart` with `restart_status=failed`, `restart_current`, and `restart_last_error` so the operator can retry with `restart <task_id>` or `homelabctl task restart <task_id>`.
- `awaiting_verification`: local task merge has landed in the main repo, or remote task review acknowledged the remote result. The human still needs to verify the running app or the named remote machine/directory. Next transition: `awaiting_verification -> done` via `accept`, or `awaiting_verification -> queued` via `reopen`.
- `done`: the human accepted the merged result. Terminal state.
- `cancelled`: work was intentionally stopped. Terminal state.

## Planning Gate

Every task record carries a durable reviewed plan before execution starts. The plan is stored in the task JSON under `plan` and is visible in the `/tasks` selected-task pane as a collapsible reviewed-plan section. The planning gate keeps the default inspect, change, validate, and handoff shape, then grounds local task plans with a lightweight repository scan. It searches the task title, goal, and acceptance criteria against source files, docs, and tests so the worker sees likely starting points before editing. Remote tasks and legacy graph phases still get target- or phase-specific plans. It records:

- a concise task-, phase-, or target-specific plan summary, including likely repo paths when the scan finds them
- ordered execution steps for the current task, legacy graph phase, or execution target
- likely code, docs, tests, and validation commands for local repository work
- known risks for that phase, target, or repo scan before work starts
- a reviewer note confirming the plan contains the required execution stages

`homelabd` writes `task.plan.created` and `task.plan.reviewed` events to the JSONL event log. If an older task has no reviewed plan, or only has the legacy generic or pre-scan default plan, `run` or `delegate` creates and reviews the current repo-aware plan before assigning the worker. The scan is a starting point only; workers must still inspect callers, imports, generated files, UI flows, and task state before editing.

Reviewing a task with no workspace diff moves it to `blocked` instead of leaving it `running`; the next action should be to rerun, delegate with clearer instructions, or delete the task.

Task records include run lifecycle timestamps. `started_at` is set when a task enters `running`, and `stopped_at` is set when it leaves `running` for review, approval, verification, blocked, failed, done, or cancelled states. Reopening or rerunning a task starts a new run and clears the previous `stopped_at`.

The review gate records failure state and then exits; it does not restart a worker while ReviewerAgent owns the task. If checks or diff validation fail, the task stays `blocked`; if branch reconciliation fails, it moves to `conflict_resolution`. In either case, the failure reason is stored in the task result and task activity. The task supervisor owns follow-up recovery after review releases the task. When recovery starts, either automatically or through `retry`/`delegate`, `homelabd` preserves the previous failure context in the worker prompt and task result. For rebase or merge-conflict states, it also attempts to merge current `main` into a clean task worktree before starting the worker, leaving real conflict files in place for the worker to resolve.

Approvals are single-use decisions tied to the task state at the time they were requested. A merge approval for a task that is no longer `awaiting_approval` is stale and must not run. Retrying, delegating, assigning, reopening, refreshing, cancelling, or accepting a task stales its pending approvals; a later review also stales older pending approvals before requesting a new merge approval. If more than one pending merge approval exists for legacy data, only the newest one can run. When a merge approval is approved, the Orchestrator automatically merges current `main` into the task worktree before executing the approved merge. If that reconciliation conflicts, the approval is marked `failed`, the task moves to `conflict_resolution`, and the task supervisor queues automatic conflict recovery instead of returning a raw HTTP error. If an operator clicks a failed merge approval later, `homelabd` treats that as a recovery request: it queues automatic recovery or requeues review rather than reporting a dead approval as granted.

## Task Records

New local development tasks are represented by one queued task record and one isolated worktree. The task keeps the original goal, reviewed plan, lifecycle timestamps, workspace path, and final result. The durable `title` is generated through the LLM-backed `text.summarize` tool with an 84-character task-pane limit, while `goal` preserves the full user input for execution context. Task creation chat replies use that summarised title as a dashboard link to `/tasks?task=<task_id>`. If the summariser cannot run, task creation continues with a clipped extractive fallback title. `homelabd` no longer expands a new task into separate inspect, design, implement, test, docs, and review queue items.

Task records may also include `attachments`. Dashboard help reports use this for `browser-context.json` and screenshots; chat/task creation can attach uploaded files. Attachments store name, media type, byte size, optional text preview, and optional data URL content. The `/tasks` selected-task pane shows attachments in `State and context`, and worker prompts include attachment names plus text previews so evidence is not lost outside the UI.

Use `show <task_id>` to inspect the task, `run <task_id>` to start the built-in coder, `retry <task_id> codex <instruction>` to force a recovery attempt with preserved failure context, `delegate <task_id> to codex` to use an external worker directly, `restart <task_id>` to retry a failed post-merge restart gate, and `accept <task_id>` after verification is available. In the dashboard, `/tasks` exposes typed buttons for run, review, approval, post-merge restart retry, accept, reopen, cancel, retry, and delete; those buttons call HTTP endpoints directly rather than sending task commands through chat. Automatic recovery attempts are shown in the selected task state so an operator can follow the rebase queue without babysitting every retry. Long diagnostics such as worker output, activity, the reviewed plan, restart status, and the original prompt are collapsible so they remain available without dominating the decision flow.

Older task records may still contain graph metadata from the previous workflow:

- `parent_id`: parent task for a legacy child phase.
- `depends_on`: task IDs that must be accepted before a legacy phase can run.
- `blocked_by`: currently unresolved dependency task IDs.
- `graph_phase`: legacy phase name such as `root`, `inspect`, `design`, `implement`, `test`, `docs`, or `review`.
- `acceptance_criteria`: durable checklist items for the task or legacy phase.

## Verification Commands

Use `accept <task_id>` after checking that the merged change works. If the task is `awaiting_restart`, acceptance is rejected until the required restart gate has completed and the task has moved to `awaiting_verification`.

Use `reopen <task_id> <reason>` when the merged change needs more work, for example:

```text
reopen 28493611 needs rework
```

Reopening moves the task back to `queued` and preserves the reason in the task result.

For command-line operation, use `homelabctl` rather than raw HTTP calls:

```bash
go run ./cmd/homelabctl status
go run ./cmd/homelabctl task show <task_id>
go run ./cmd/homelabctl task new --attach ./browser-context.json "Fix the bug shown in the attachment"
go run ./cmd/homelabctl task runs <task_id>
go run ./cmd/homelabctl task diff <task_id>
go run ./cmd/homelabctl task review <task_id>
go run ./cmd/homelabctl approve <approval_id>
go run ./cmd/homelabctl task restart <task_id>
go run ./cmd/homelabctl task accept <task_id>
go run ./cmd/homelabctl task reopen <task_id> "needs rework"
go run ./cmd/homelabctl task delete <task_id>
```

See `docs/homelabctl.md` for the full CLI command surface and the rule that new operator workflows must keep the CLI up to date.

## Agentic Testing

Agent validation must not interrupt the production dashboard or `homelabd` stack. Browser UAT runs from the task worktree with an isolated Playwright/Vite server; agents must not restart `dashboard`, `homelabd`, `healthd`, or `supervisord` to prove their changes.

For dashboard task-page changes, use:

```bash
nix develop -c bun run --cwd web uat:tasks
```

For broad dashboard shell, navigation, theme, terminal, docs, workflow, health, or supervisor changes, use:

```bash
nix develop -c bun run --cwd web uat:site
```

Both commands use mocked APIs and a per-worktree port, so concurrent local or remote agents do not share a dashboard process. The review gate runs task-page UAT for task-page-only diffs and site-wide UAT for shared UI, shell, route, Playwright, or browser tooling diffs. See `docs/agentic-testing.md` for the full SDLC and browser reliability notes.

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

If a restart-required task is still `awaiting_restart` after `homelabd` restarts, recovery continues the post-merge restart gate from the durable task record. A `homelabd` self-restart is recorded before the process exits; the next process marks that component complete and continues with any remaining components.

Remote tasks are excluded from local restart recovery. A remote task stays in its target queue for the selected `homelab-agent`, and a running remote task is completed or failed only by that agent's completion callback.

Recovery decisions are written to the JSONL event log as `task.recovery.*` events and to the daemon logs with structured `slog` fields including task ID, short ID, title, workspace, strategy, and backend.

## Agent Completion Expectations

When a task changes user-facing behavior, commands, UI, configuration, tools, or workflow, the worker should update relevant docs or help text in the same patch.

Agents should include Mermaid fenced diagrams when a compact state machine, dependency graph, architecture map, or handoff diagram would improve human or machine understanding. Use the homelabd brand diagram palette documented in `AGENTS.md` and avoid diagram-level Mermaid init directives that override the shared theme.

When an external coding worker finishes a local task, `homelabd` automatically runs the review gate. Review normally runs for `ready_for_review` tasks and temporarily owns the task while checks run, so a retry or worker cannot mutate the same workspace underneath ReviewerAgent. A blocked task whose result starts with `ReviewerAgent checks failed:` can be reviewed again after a test-infrastructure fix without starting another worker. If the task changes state during a long review, the review result is ignored and no approval or block state is written. The review gate runs project checks, verifies the task branch can merge cleanly into the current repository state, stales any older pending approvals for the task, and only then creates a merge approval. Bun checks use the repo's Nix dev shell when a `flake.nix` is present, so ReviewerAgent gets the same Bun, Playwright, Chromium, and shared-library environment as the documented worker commands. Check failures name the failing tool, for example `bun.uat.site`, and keep the failing output tail visible. A task branch that cannot merge cleanly moves to `conflict_resolution` with an explicit premerge failure; approval is not created, recovery is handed to the task supervisor after review releases the task, and the main repository must not be left in a conflicted state. That recovery prepares the isolated worktree, not the main repository, so the next worker sees the unresolved files instead of a clean but still-stale branch.

When a remote agent finishes, `homelabd` records the remote result and moves the task to `ready_for_review`. Reviewing a remote task acknowledges the result and moves it to `awaiting_verification`; it does not run local project checks, compare local and remote `HEAD`, create a merge approval, or touch the control-plane checkout.

External worker completions are ignored once the task has advanced to merge approval, merged verification, done, or cancelled. Remote completion callbacks are accepted only while the matching remote task is still `running`. This prevents a stale background worker from moving an already merged or accepted task back to review.

Final task summaries should include:

- changed files
- validation run
- how to use the change
- docs updated, or why no docs change was needed

## Repository Agent Tools

Agents inspect code with `repo.list`, `repo.read`, `repo.search`, and `repo.current_diff`. `repo.search` is the default code-search tool: it returns repository-relative paths, matched line numbers, and bounded grep-like context. Use `workspace` for task worktrees, `path` to narrow scope, `context_lines` to tune surrounding lines, and `max_results` to keep prompts small.

Coder and UX agents create or edit files in isolated task worktrees with `repo.write_patch`. The patch is a unified diff against repository-relative paths, so it can add new files and modify existing files without touching the live checkout.

## Git Agent Tools

Agents can inspect repository state with `git.status`, `git.diff`, `git.branch`, `git.describe`, `git.log`, and `git.show`.

Write workflow tools are available for explicit git operations:

- `git.commit` stages selected paths or all changes and creates a commit
- `git.revert` reverts a commit, optionally with `--no-commit`
- `git.merge` merges a branch or commit into the current branch

These write tools are high-risk and approval-gated by default. Task review still uses `git.merge_approved` for the normal reviewed-task merge path.

## Shell Agent Tools

`shell.run_limited` executes only allowlisted command arrays without shell expansion. Read-only inspection and search commands such as `pwd`, `ls`, `find`, `grep`, `rg`, `cat`, `wc`, `head`, and `tail` are available for task worktrees, along with routine build/test commands. `find` execution hooks and `rg --pre` preprocessors are rejected. Potentially destructive allowlisted commands, including `rm`, `rmdir`, `mv`, `cp`, `git clean`, `git reset`, `git restore`, `git rm`, and `git checkout -- <path>`, are classified as high risk by the tool policy and create an approval request before execution.

Review pending shell requests with `approvals`, then use `approve <approval_id>` or `deny <approval_id>`.

## Restart Impact

Local review reports a restart impact line from the diff. Runtime, supervisor, healthd, and dashboard paths are mapped to their supervised components and stored on the task as `restart_required`, with the same list copied into the merge approval payload. After approval, `homelabd` moves the task to `awaiting_restart`, calls `supervisord` restart endpoints for each required component, waits for configured health URLs to return 2xx, and only then moves the task to `awaiting_verification`. `accept` is blocked while the restart gate is pending, running, or failed.

Restarting a supervised app also applies its configured runtime preparation. The default dashboard app runs `bun install --frozen-lockfile` from `web/` before Vite starts, so dependency or lockfile changes from an approved merge are applied to the live checkout before health checks can pass.

`supervisord` treats non-2xx app health checks as failed, so a dashboard that starts returning 500s after a merge is restarted instead of remaining up in a broken Vite SSR state. Remote review cannot infer restart impact from the control-plane diff; verify the named remote machine and directory directly before accepting.
