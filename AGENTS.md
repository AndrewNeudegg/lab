## Definition of Done

- Do not mark UI work complete from compile/unit checks alone. For changed pages, run a browser-level check against a served page from the changed worktree or explicitly state that browser UAT was not possible.
- Browser UAT for agent work must use an isolated dev server from the task worktree, not the production dashboard or production `supervisord` stack.
- For dashboard task-page changes, run `nix develop -c bun run --cwd web uat:tasks`. This starts a per-worktree Playwright/Vite server on an isolated port and uses mocked `homelabd` APIs.
- For broad dashboard shell, navigation, theme, terminal, docs, workflow, health, or supervisor changes, run `nix develop -c bun run --cwd web uat:site`. It covers every primary page on desktop and mobile, exercises workflows, checks overflow/clipping, and attaches screenshots.
- If headless Chromium cannot launch, run `nix develop -c bun run --cwd web browser:preflight` and treat a sandbox/dependency failure as infrastructure to fix or move to a browser-capable worker. Do not substitute production restarts.
- Browser UAT must exercise the user-reported interaction, not just page load. Check visible data, button state changes, selected item changes, and mobile viewport behavior when relevant.
- Do not stop or restart production `dashboard`, `homelabd`, `healthd`, or `supervisord` for agent validation. If a supervised service truly needs a restart after merge, report the restart impact and leave it for explicit operator verification.
- Add automated regression coverage for fixed bugs. Prefer extracting pure view/state logic into testable modules instead of only adding source-string assertions.
- A final handoff for UI changes must say which browser/UAT command ran and what interaction it verified.
- Documentation must be written and kept in sync with behaviour, commands, UI, configuration, tools, and workflows in the same change. If no docs update is needed, state why in the handoff.
- Documentation is for humans and LLMs. Keep it concise, use British spelling, and emphasise discoverability and usability with clear titles, searchable terms, related links, and current examples.

## UI/UX Agent Work

- Do not start UI implementation from an open-ended prompt. Before editing, write or preserve a compact UI/UX brief that names the operator goal, primary workflow, changed surfaces, relevant existing patterns, desktop and mobile layout approach, and expected loading, empty, error, disabled, long-content, and success states.
- Reuse the dashboard's existing components, interaction patterns, CSS variables, semantic colour roles, spacing, typography, and documentation before inventing new UI. If a design source, component story, screenshot baseline, or Figma reference exists, inspect it before changing code.
- Optimise operational surfaces for repeated use: dense but readable layouts, visible system status, clear next actions, consistent navigation, reversible or recoverable actions, and plain-language errors. Avoid decorative layouts that make task state, health state, terminal state, documentation, or workflow state harder to scan.
- Add or update regression coverage for the exact interaction and state that changed. Coverage must include role/text assertions, keyboard or focus checks when the surface is interactive, desktop and mobile layout assertions, desktop and mobile accessibility checks, and desktop and mobile visual comparisons for deterministic surfaces.
- Browser UAT for UI work must include a screenshot review of the changed state on both desktop and mobile. Deterministic visual states must use visual baselines on both desktop and mobile; volatile states must use attached or inspected screenshots plus explicit layout assertions on both desktop and mobile.
- The final handoff for UI work must state the design source or existing pattern used, the browser/UAT command run, the interaction exercised, the viewport coverage, the accessibility or visual checks performed, and any residual UI risk.

See `docs/ui-ux-agent-work.md` for the full UI/UX workflow, review checklist, and required automation.

## homelabctl

- Use `homelabctl` for interactive `homelabd` operation instead of ad hoc HTTP calls. See `docs/homelabctl.md`.
- Keep `docs/homelabctl.md`, `cmd/homelabctl`, and the `homelabd` HTTP API in sync. If `homelabctl` is not useful enough for a new chat, task, approval, event, or terminal workflow, extend it and add regression tests rather than bypassing it.

## Diagrams And Brand Colours

- Use Mermaid fenced diagrams when a workflow, state machine, dependency graph, architecture, or handoff would be clearer for humans or machines as a compact diagram.
- Keep diagrams small, label edges clearly, and avoid decorative complexity.
- Use the homelabd brand diagram palette for generated visuals. Light: background `#f8fafc`, surface `#ffffff`, primary `#2563eb`, secondary `#0f766e`, success `#16a34a`, warning `#d97706`, danger `#dc2626`, text `#172033`, muted `#64748b`, border `#cbd5e1`. Dark: background `#0f172a`, surface `#111827`, primary `#60a5fa`, secondary `#2dd4bf`, success `#4ade80`, warning `#fbbf24`, danger `#f87171`, text `#e2e8f0`, muted `#94a3b8`, border `#334155`.
- Dashboard Markdown renders Mermaid code fences in chat and docs with that palette and locks theme overrides, so do not add conflicting Mermaid init directives.
