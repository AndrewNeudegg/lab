# UI Pattern Catalogue

Use this catalogue as the required source for dashboard UI work before creating new controls, layouts, or visual states. It is intentionally compact so agents can find and reuse existing patterns quickly.

## Navigation

Use the shared responsive navbar on every dashboard page.

- Desktop: show labelled top-level destinations inline.
- Mobile: use the labelled `Menu` button, primary mobile navigation, and scrim.
- Keep `Help` visible when the page can create contextual bug tasks.
- Use `aria-current="page"` plus visible active styling.
- Expose attention counts in both badge text and accessible labels.

Reference: `web/shared/src/lib/Navbar.svelte`

## List-Detail Records

Use list-detail for operator records such as tasks, workflows, knowledge spaces, and terminal sessions.

- Desktop: keep list and selected detail visible together when the workflow benefits from comparison.
- Mobile: show one primary workflow at a time and provide a clear return control.
- Preserve URL-addressable selection state.
- Keep rows dense, with status text plus semantic colour.
- Keep long titles and IDs wrapped inside their row, never overflowing the viewport.

References: `/tasks`, `/knowledge`, `/workflows`, `/terminal`

## Actions And Decisions

Use direct, typed controls for task and system actions.

- Primary actions must be visible near the decision context.
- Destructive actions must stay secondary.
- Disabled controls must have visible disabled state and accessible names.
- State-changing actions must return clear success, failed, or pending feedback.
- Running, queued, and restart-gated work must be labelled as system-owned state, not as a user decision.

References: task decision panel, merge queue controls, supervisor app controls

## Status And Feedback

Every status indicator must pair colour with text.

- Green: healthy, complete, accepted, or connected.
- Amber: pending review, approval, restart, warning, reconnecting, or queued.
- Red: failed, timed out, blocked, conflict, or critical.
- Blue: active work or selected state.
- Use pulsing only for system-owned active refresh or active work.
- Keep errors in plain language with a recovery path.

Reference: `web/shared/src/lib/tasks.ts`

## Panels And Disclosures

Use disclosures for secondary detail that operators still need during review.

- Keep primary status, decision, and selected record summary visible before disclosures.
- Use disclosures for worker runs, task activity, reviewed plans, original input, changes, and long diagnostics.
- Disclosure summaries must include enough text to decide whether to open them.
- Long diagnostics must wrap or scroll within their own region.

Reference: `/tasks` selected-task detail

## Forms

Use short, task-scoped forms for structured input.

- Labels must remain visible.
- Submit buttons must reflect disabled, pending, success, and error states.
- Keep helper copy short and operational.
- Validate target context before remote or destructive actions.
- Preserve typed text through layout changes.

References: chat composer, task retry/reopen forms, remote task target panel

## Empty, Loading, And Error States

Every changed UI surface must provide these states.

- Loading: show what is being fetched or prepared.
- Empty: explain what is absent and the next available action.
- Error: show what failed, whether retry is possible, and where to inspect details.
- Stale data: show reconnecting or last-sync state without hiding existing data.
- Long content: wrap or scroll inside the owning region.

Reference: `docs/dashboard.md#Layout Rationale`

## Desktop And Mobile Checks

Every browser-visible UI change must pass both desktop and mobile checks.

- Desktop viewport: primary workflow visible, no clipped controls, no horizontal overflow.
- Mobile viewport: pinned navbar does not cover content, one workflow is clear, controls remain reachable.
- Accessibility: axe checks run on both desktop and mobile.
- Visual review: stable visual baselines run on both desktop and mobile; volatile pages attach or inspect screenshots on both.

Commands:

```bash
nix develop -c bun run --cwd web uat:ui
nix develop -c bun run --cwd web uat:tasks
nix develop -c bun run --cwd web uat:site
```

## Related Links

- `docs/ui-ux-agent-work.md` for the mandatory UI/UX agent workflow
- `docs/dashboard.md` for dashboard surface rationale
- `docs/agentic-testing.md` for isolated browser UAT
