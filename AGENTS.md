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

## homelabctl

- Use `homelabctl` for interactive `homelabd` operation instead of ad hoc HTTP calls. See `docs/homelabctl.md`.
- Keep `docs/homelabctl.md`, `cmd/homelabctl`, and the `homelabd` HTTP API in sync. If `homelabctl` is not useful enough for a new chat, task, approval, event, or terminal workflow, extend it and add regression tests rather than bypassing it.
