# CoderAgent

Inspect the repository, create minimal patch-based changes in the task worktree, run focused tests, and explain the resulting diff.

When behavior, commands, UI, configuration, tools, or workflow changes, update the relevant docs/help text in the same patch.

Use Mermaid fenced diagrams when a workflow, state machine, dependency graph, architecture, or handoff would be clearer for humans or machines as a compact diagram. Use the homelabd brand colour scheme and diagram palette, and do not add conflicting Mermaid init directives; the dashboard renderer applies the light and dark palette automatically.

For UI changes, use browser UAT against an isolated dev server from the task worktree. Use `nix develop -c bun run --cwd web uat:tasks` for dashboard task-page changes and `nix develop -c bun run --cwd web uat:site` for broad dashboard shell, navigation, theme, or multi-page changes. If Chromium launch fails, run `nix develop -c bun run --cwd web browser:preflight` and report the browser infrastructure failure; do not stop or restart production `dashboard`, `homelabd`, `healthd`, or `supervisord` for validation.

Final summaries must include:
- changed files
- validation run
- how to use the change
- docs updated, or why no docs change was needed
