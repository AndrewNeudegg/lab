# Dashboard

The dashboard has these primary operator surfaces:

- `/chat`: global conversation, broad direction, planning, and general commands.
- `/tasks`: task queue, selected-task record, task actions, and task-scoped activity.
- `/docs`: searchable documentation library generated from Markdown files in `./docs`.
- `/terminal`: browser terminal backed by a homelabd shell session for direct operator commands.
- `/supervisord`: supervised application status and start, stop, restart controls.
- `/healthd`: healthd service status, system utilization, checks, SLOs, and notifications.

Do not collapse these into one surface. Chat and tasks represent different mental models.
Healthd is also deliberately separate: its API is served by the `healthd` Go service, not by `homelabd`.

## Navigation

Use the shared responsive navbar on every dashboard page.

- Desktop and tablet: show primary destinations inline because visible navigation is more discoverable than hidden navigation.
- Mobile: collapse destinations behind a labelled `Menu` hamburger button to preserve content width.
- Always include text labels. The hamburger glyph is a space-saving cue, not the only signifier.
- Keep top-level destinations flat: `Chat`, `Tasks`, `Docs`, `Terminal`, `Supervisor`, and `Health`.
- Show active page state with `aria-current="page"` and visible styling.

## Documentation Library

The `/docs` page imports every Markdown file under `./docs` into the dashboard. It shows a searchable document catalogue, selected document content, heading anchors, and an on-page table of contents. Keep document titles specific and include the terms operators and LLM agents are likely to search for.

## Research Inputs

- Apple split-view guidance: keep navigation and detail panes visibly related, preserve the current selection, and avoid forcing split panes into compact mobile widths.
- Android and Material responsive guidance: use list-detail on wide screens, then adapt to one stacked destination on compact screens.
- Material navigation guidance: use drawers for compact layouts and keep primary navigation destinations consistent across layouts.
- NN/g menu guidance: visible navigation performs better for discoverability; hidden hamburger navigation should be reserved for constrained space.
- Nielsen Norman usability heuristics: always expose system status, speak the operator's language, and keep clear exits for wrong actions.
- Atlassian/Jira issue views: work-item detail pages have top-level issue actions and an activity feed containing changes, comments, history, and related updates.
- Slack threads and incident-command tools: conversations need explicit context boundaries; task or incident timelines prevent important work from being buried in a global chat scroll.
- Atlassian dashboard and status guidance: centralize task visibility, make bottlenecks obvious, use semantic colour roles, and pair colour with text.
- GitHub pull request diffs: review should compare topic-branch changes against the base branch, offer unified and split views, show additions in green and deletions in red, and use three-dot comparison to focus on what the task branch introduces.
- GitLab merge request reviews: the changes view is the primary review surface, with review status and merge checks kept close to the diff.
- CodeMirror and Monaco diff APIs: mature web diff viewers support hidden unchanged regions, gutters, syntax-aware deleted text, inline change highlighting, and unified or side-by-side review modes.

Sources:

- https://developer.apple.com/design/human-interface-guidelines/split-views
- https://developer.apple.com/design/human-interface-guidelines/sidebars
- https://developer.android.com/develop/ui/views/layout/build-responsive-navigation
- https://m1.material.io/patterns/navigation-drawer.html
- https://m1.material.io/layout/structure.html
- https://media.nngroup.com/media/articles/attachments/PDF_Menu-Design-Checklist.pdf
- https://www.nngroup.com/articles/ten-usability-heuristics/
- https://www.nngroup.com/articles/visibility-system-status/
- https://developer.atlassian.com/cloud/jira/platform/issue-view/
- https://support.atlassian.com/jira-software-cloud/docs/what-are-the-different-types-of-activity-on-an-issue/
- https://slack.com/help/articles/115000769927-Use-threads-to-organize-discussions
- https://www.atlassian.com/incident-management/postmortem/timelines
- https://docs.aws.amazon.com/incident-manager/latest/userguide/tracking.html
- https://docs.firehydrant.com/docs/incident-timeline
- https://atlassian.design/foundations/color
- https://atlassian.design/components/lozenge/
- https://www.atlassian.com/agile/project-management/task-management-dashboard
- https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/proposing-changes-to-your-work-with-pull-requests/about-comparing-branches-in-pull-requests
- https://docs.gitlab.com/user/project/merge_requests/reviews/
- https://codemirror.net/docs/ref/#merge.unifiedMergeView
- https://microsoft.github.io/monaco-editor/typedoc/interfaces/editor.IDiffEditorConstructionOptions.html

## Layout Rationale

Every visible component must answer one of these questions:

For `/tasks`, every visible component must answer one of these questions:

1. What needs my attention?
2. What is running?
3. What task am I looking at?
4. What is the safest next action?
5. What happened on this task?

For `/chat`, every visible component must answer one of these questions:

1. What did I tell homelabd?
2. What did homelabd say back?
3. Which generated command can I safely click?
4. Where do I go to inspect task state?

If a component does not answer one of those questions, it should not be in the primary surface.

## Component Placement

- `/tasks` left pane: task queue. It is the navigation model, because the operator supervises work by task rather than by chat transcript.
- Top-left header: system identity, sync freshness, and manual sync. This answers whether the view is current. The `Synced` timestamp includes seconds so a manual reload is visible even when repeated within the same minute.
- Triage buttons: `Needs action`, `Running`, and `All`. The page opens on `Needs action` because this page is primarily an operator console; `All` remains one tap away for audit and search. The buttons double as counts and filters so the operator can shift attention without extra controls.
- Search field: below triage because search is secondary; first the operator needs to see urgent work, then find specific work.
- Task list: appears immediately after search. Rows are the main navigation and must stay fast to scan even when approvals or remote queues are present.
- Decision block: pending approvals use direct approve/deny buttons. Approval actions must call the typed approval endpoints, not route through chat.
- Task rows: coloured dot plus text status. Colour gives scan speed; text keeps it accessible and unambiguous.
- Right pane: selected task record. It is not a chat transcript and has no task chat composer. Selecting a different task changes the record, summary, result, action buttons, diff, worker trace, and activity timeline.
- Manual `Sync` refreshes tasks, approvals, events, and remote agents first, then refreshes selected-task worker runs and the local diff without blocking the queue from becoming current.
- Task sync failures are shown inside the task pane. The queue must never make a failed `/api/tasks` request look like a real empty result.
- Selected task title: use a compact summary derived from the task input so long prompts do not dominate the top of the record.
- Task summary: ID, status, owner, started time, runtime, and update time. This answers what object is selected and how long it has been running before asking the operator to act.
- Primary action: one emphasized button derived from task state. The UI should not make the operator infer the next command from raw status.
- Secondary actions: direct task endpoint buttons such as retry, reopen, stop, delete, or deny approval. Do not build task-page buttons by sending chat messages or natural-language commands to `/message`.
- Retry and reopen forms: short, task-scoped inputs for optional retry instruction or reopen reason. These are not chat; they are structured payloads sent to typed task endpoints.
- Workspace path: shown only for selected tasks because it is supporting implementation context, not queue-level navigation.
- Remote execution context: shown as a warning-coloured block for remote tasks. It must repeat machine, agent, backend, and full directory path because remote tasks may run outside this repo and a wrong target can damage the wrong checkout.
- Changes vs main: task-scoped diff review loaded from `GET /tasks/{task_id}/diff`. It shows the branch comparison, summary counts, changed-file navigation, split/unified toggles, line numbers, addition/deletion colour, wrapped long lines, and inline changed-text highlights. Use this before review, conflict-resolution delegation, or approval.
- Result block: shown only when a task has a stored result.
- Worker trace: groups external worker output by run id and combines live `agent.delegate.output` events with completed artifacts from `data/runs`.
- Task activity: event-log timeline filtered to the selected task. This is the task-scoped history equivalent to issue activity or incident timelines.
- Reviewed plan: shown directly above the original input so the operator can see the task-, phase-, or target-specific execution path before rereading the full prompt.
- Original input: shown below task activity, preserving the full task goal text for reference after the timeline.
- `/chat` page: single global transcript and composer. It does not show selected task detail because selecting tasks and typing chat commands are separate jobs.
- Cross-page links: `/chat` links to `/tasks`, and `/tasks` links back to `/chat`, so the operator can switch modes deliberately.

## Status Semantics

- Queued: the task exists and is waiting in its execution queue. Local tasks have isolated worktrees and wait for the local task supervisor; remote tasks wait for the selected `homelab-agent`.
- Running: an in-memory local worker or a remote agent is active.
- Red: failed, blocked, or conflict resolution. Needs intervention.
- Amber: ready for review, awaiting approval, or awaiting verification. Needs a human decision.
- Blue: queued or running. Work is active.
- Green: done. No action required unless the result is wrong.
- Gray: unknown or neutral state.

Do not rely on colour alone. Always show the status text next to the coloured indicator.

## Task Supervisor

`homelabd` owns task liveness; the UI should not make the operator babysit worker state.

- New tasks start as `queued`, not `running`, until a worker is actually assigned.
- The task supervisor periodically scans the durable task store and starts queued work with the preferred external worker, currently `codex` when configured.
- On boot, persisted `running` tasks are recovered because in-memory worker state cannot survive a process restart.
- During normal operation, stale `running` tasks with no in-memory owner are retried after `limits.task_stale_seconds`.
- Supervisor activity is logged with `slog` and appended to the event log using `task.supervisor.*` or `task.recovery.*` events.
- Remote-targeted tasks are not picked up by the local task supervisor. They stay in the queue for the selected `homelab-agent`, and review does not compare the remote checkout against the control-plane repo.

## Task Queues

The Tasks page separates work by execution queue:

- `All queues` shows every task.
- `Local homelabd` shows tasks that run in local homelabd worktrees.
- Each remote agent gets its own queue, named by agent display name and machine.

Remote task creation is deliberately explicit. The "New task target" panel shows the selected agent, machine, and full directory path, and the create button remains disabled until the context confirmation checkbox is checked. Treat that checkbox as the final guard against running an agent in the wrong checkout. The API rejects unknown workdir ids or paths for registered agents, so a stale UI selection should fail instead of silently falling back.

The `Needs action` tab shows tasks that need an operator decision, failed work, review, approval, verification, or conflict resolution. Legacy task graph parent records or child phases blocked only by an earlier graph phase are hidden from this tab, but remain visible in `All` and search for auditability.

Remote task detail pages repeat the execution context in an amber "Remote execution context" block. Verify that machine and path before asking for follow-up work.

Local tasks use isolated local worktrees. Remote tasks do not create local worktrees and do not compare their repository state against the control-plane repo.

## Mobile Behavior

On compact screens `/tasks` stacks:

1. A sticky `Queue` / `Task` switch sits below the navbar.
2. `Queue` shows filters, search, task rows, approvals, execution queues, and new-task creation.
3. `Task` shows the selected task record, direct action buttons, diff, worker trace, activity, plan, and original input.

The split view is not forced into a narrow screen because that makes task names, task details, and command output harder to read. Selecting a row switches to `Task`; the `Queue` tab and the record header's `Queue` button return to the list. The Tasks page does not render a global command panel on mobile.

On compact screens `/chat` remains a single-column conversation because there is no task-detail pane on that page.

On compact screens `/terminal` keeps the xterm viewport as the primary scroll area and places large control-key buttons below it. Include controls for keys commonly missing or awkward on Android keyboards, including `Ctrl-C`, `Ctrl-D`, `Ctrl-Z`, `Tab`, `Esc`, and arrow keys.

## Terminal Runtime

The Terminal page uses homelabd HTTP endpoints under `/terminal/sessions`, proxied by the dashboard as `/api/terminal/sessions` during development. Creating a session starts the user's shell in the homelabd working directory inside a Linux PTY. The browser renders the session with xterm.js, connects terminal bytes over `GET /terminal/sessions/{id}/ws`, and sends terminal resize updates with `POST /terminal/sessions/{id}/resize`.

Do not strip ANSI or terminal control sequences in the dashboard. The PTY byte stream is intentionally passed to xterm.js so colours, cursor movement, prompts, tab completion, and full-screen CLI programs behave like a real terminal. Keyboard input should go directly into the xterm viewport, not through a separate command composer.

The Terminal page has a session target picker. `homelabd local` opens a PTY on the control plane. Online remote agents appear when their heartbeat metadata includes `terminal_base_url`; choosing one starts the session through that agent's browser-reachable terminal API.

This is an operator shell. Run it only where the homelabd HTTP API is already trusted, because anyone who can reach the endpoint can execute commands as the homelabd process user.

## Healthd Runtime

Run healthd as its own process:

```bash
./run.sh build-healthd ./.bin/healthd
./.bin/healthd
```

The default healthd API address is `127.0.0.1:18081`. During dashboard development, Vite proxies `/healthd-api/*` to that process. A `500 Internal Server Error` from `/healthd` usually means the dashboard is running but `healthd` is not listening on `127.0.0.1:18081`.

`homelabd` sends a heartbeat to `POST /healthd/processes/heartbeat` when healthd is enabled, then repeats every `healthd.process_heartbeat_interval_seconds`. Healthd lists announced processes in the `/healthd` snapshot and turns stale heartbeats into `process:<name>` check failures after `healthd.process_timeout_seconds`, so the Health page shows `homelabd` alongside configured HTTP checks and future monitored processes.

Remote agents are also represented as healthd processes. `homelabd` forwards accepted remote-agent heartbeats as `remote-agent:<agent_id>` with type `remote_agent`, machine metadata, service instance identity, current task id, advertised workdir count, and a TTL based on `control_plane.agent_stale_seconds`.
