# Dashboard

The dashboard has two primary operator surfaces:

- `/chat`: global conversation, broad direction, planning, and general commands.
- `/tasks`: task queue, selected-task record, task actions, and task-scoped activity.
- `/healthd`: healthd service status, system utilization, checks, SLOs, and notifications.

Do not collapse these into one surface. Chat and tasks represent different mental models.
Healthd is also deliberately separate: its API is served by the `healthd` Go service, not by `homelabd`.

## Navigation

Use the shared responsive navbar on every dashboard page.

- Desktop and tablet: show primary destinations inline because visible navigation is more discoverable than hidden navigation.
- Mobile: collapse destinations behind a labelled `Menu` hamburger button to preserve content width.
- Always include text labels. The hamburger glyph is a space-saving cue, not the only signifier.
- Keep top-level destinations flat: `Chat`, `Tasks`, and `Health`.
- Show active page state with `aria-current="page"` and visible styling.

## Research Inputs

- Apple split-view guidance: keep navigation and detail panes visibly related, preserve the current selection, and avoid forcing split panes into compact mobile widths.
- Android and Material responsive guidance: use list-detail on wide screens, then adapt to one stacked destination on compact screens.
- Material navigation guidance: use drawers for compact layouts and keep primary navigation destinations consistent across layouts.
- NN/g menu guidance: visible navigation performs better for discoverability; hidden hamburger navigation should be reserved for constrained space.
- Nielsen Norman usability heuristics: always expose system status, speak the operator's language, and keep clear exits for wrong actions.
- Atlassian/Jira issue views: work-item detail pages have top-level issue actions and an activity feed containing changes, comments, history, and related updates.
- Slack threads and incident-command tools: conversations need explicit context boundaries; task or incident timelines prevent important work from being buried in a global chat scroll.
- Atlassian dashboard and status guidance: centralize task visibility, make bottlenecks obvious, use semantic color roles, and pair color with text.

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
- Top-left header: system identity, sync freshness, and manual sync. This answers whether the view is current.
- Triage buttons: `Needs action`, `Running`, and `All`. They double as counts and filters so the operator can shift attention without extra controls.
- Search field: below triage because search is secondary; first the operator needs to see urgent work, then find specific work.
- Decision block: pending approvals appear before the task list because they are human-blocked work.
- Task rows: colored dot plus text status. Color gives scan speed; text keeps it accessible and unambiguous.
- Right pane: selected task record. It is not a chat transcript. Selecting a different task changes the record, summary, result, and activity timeline.
- Task summary: ID, status, owner, and update time. This answers what object is selected before asking the operator to act.
- Primary action: one emphasized button derived from task state. The UI should not make the operator infer the next command from raw status.
- Secondary actions: show, delegate, delete, or reopen. These are useful but lower priority than the primary action.
- Next-step panel: explains why the primary action is recommended. This is the guardrail against blind clicking.
- Workspace path: shown only for selected tasks because it is supporting implementation context, not queue-level navigation.
- Result block: shown only when a task has a stored result.
- Task activity: event-log timeline filtered to the selected task. This is the task-scoped history equivalent to issue activity or incident timelines.
- `/chat` page: single global transcript and composer. It does not show selected task detail because selecting tasks and typing chat commands are separate jobs.
- Cross-page links: `/chat` links to `/tasks`, and `/tasks` links back to `/chat`, so the operator can switch modes deliberately.

## Status Semantics

- Red: failed or blocked. Needs intervention.
- Amber: ready for review, awaiting approval, or awaiting verification. Needs a human decision.
- Blue: queued or running. Work is active.
- Green: done. No action required unless the result is wrong.
- Gray: unknown or neutral state.

Do not rely on color alone. Always show the status text next to the colored indicator.

## Mobile Behavior

On compact screens `/tasks` stacks:

1. Task queue first, capped to the top portion of the viewport.
2. Selected-task record below it.
3. Global command panel below the selected-task record.

The split view is not forced into a narrow screen because that makes task names, task details, and command output harder to read.

On compact screens `/chat` remains a single-column conversation because there is no task-detail pane on that page.

## Healthd Runtime

Run healthd as its own process:

```bash
./run.sh build-healthd ./.bin/healthd
./.bin/healthd
```

The default healthd API address is `127.0.0.1:18081`. During dashboard development, Vite proxies `/healthd-api/*` to that process. A `500 Internal Server Error` from `/healthd` usually means the dashboard is running but `healthd` is not listening on `127.0.0.1:18081`.

`homelabd` sends a heartbeat to `POST /healthd/processes/heartbeat` when healthd is enabled, then repeats every `healthd.process_heartbeat_interval_seconds`. Healthd lists announced processes in the `/healthd` snapshot and turns stale heartbeats into `process:<name>` check failures after `healthd.process_timeout_seconds`, so the Health page shows `homelabd` alongside configured HTTP checks and future monitored processes.
