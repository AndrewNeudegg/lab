# Dashboard

`/chat` is the homelabd task console. Its job is not to be a generic chat app; it is the operator surface for supervising many concurrent agent tasks.

## Research Inputs

- Apple split-view guidance: keep navigation and detail panes visibly related, preserve the current selection, and avoid forcing split panes into compact mobile widths.
- Android and Material responsive guidance: use list-detail on wide screens, then adapt to one stacked destination on compact screens.
- Nielsen Norman usability heuristics: always expose system status, speak the operator's language, and keep clear exits for wrong actions.
- Atlassian/Jira issue views: work-item detail pages have top-level issue actions and an activity feed containing changes, comments, history, and related updates.
- Slack threads and incident-command tools: conversations need explicit context boundaries; task or incident timelines prevent important work from being buried in a global chat scroll.
- Atlassian dashboard and status guidance: centralize task visibility, make bottlenecks obvious, use semantic color roles, and pair color with text.

Sources:

- https://developer.apple.com/design/human-interface-guidelines/split-views
- https://developer.android.com/develop/ui/views/layout/build-responsive-navigation
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

1. What needs my attention?
2. What is running?
3. What task am I looking at?
4. What is the safest next action?
5. What happened on this task?
6. What did the agent last say globally?
7. How do I instruct the system?

If a component does not answer one of those questions, it should not be in the primary surface.

## Component Placement

- Left pane: task queue. It is the navigation model, because the operator supervises work by task rather than by chat transcript.
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
- Command panel: visually separated from the task record. It is explicitly global because freeform messages are not task-scoped unless the command names a task.
- Prompt shortcuts: inside the command panel because they are global operator prompts, not task-detail actions.
- Composer: inside the command panel with copy that explains task-specific work must name a task ID.

## Status Semantics

- Red: failed or blocked. Needs intervention.
- Amber: ready for review, awaiting approval, or awaiting verification. Needs a human decision.
- Blue: queued or running. Work is active.
- Green: done. No action required unless the result is wrong.
- Gray: unknown or neutral state.

Do not rely on color alone. Always show the status text next to the colored indicator.

## Mobile Behavior

On compact screens the layout stacks:

1. Task queue first, capped to the top portion of the viewport.
2. Selected-task record below it.
3. Global command panel below the selected-task record.

The split view is not forced into a narrow screen because that makes task names, task details, and command output harder to read.
