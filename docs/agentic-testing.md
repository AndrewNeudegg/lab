# Agentic Testing Workflow

Agent work must be testable without interrupting the operator's running dashboard or `homelabd` stack. Treat the supervised stack as production for this homelab: agents may read health and logs, but they must not stop or restart production `dashboard`, `homelabd`, `healthd`, or `supervisord` for validation.

## Isolation Model

Local tasks run in git worktrees under `repo.workspace_root`. Each task branch is reviewed and merged through `homelabd`; agents should not edit the main checkout directly.

Remote tasks run on the selected `homelab-agent` in the selected advertised workdir. The control plane records the result, but it does not create a local worktree, compare remote `HEAD`, or merge remote changes.

Browser UAT starts from the task worktree, not from the supervised dashboard. The default Playwright config derives a stable per-worktree port, starts Vite on `127.0.0.1`, refuses to reuse an existing server on that port, and runs with one worker for reproducibility.

## Standard Commands

Run these from the repository root or from the selected remote workdir:

```bash
nix develop -c go test ./...
nix develop -c bun run --cwd web check
nix develop -c bun run --cwd web build
nix develop -c bun run --cwd web test
```

For dashboard task-page changes, run the isolated UAT:

```bash
nix develop -c bun run --cwd web uat:tasks
```

`uat:tasks` runs Playwright against a local Vite server and mocked `homelabd` APIs. It verifies the mobile task queue, selected task detail, draft preservation, button state, visible data, and horizontal overflow without touching production services.

The old live diagnostic is still available for explicit operator use:

```bash
DASHBOARD_URL=http://127.0.0.1:5173/tasks nix develop -c bun run --cwd web uat:tasks:live
```

Do not use `uat:tasks:live` as an agent completion gate unless the operator explicitly asks for production verification.

## homelabd Review Gate

Local review runs Go tests, web type checks, web build, web unit tests, and task-page UAT when the diff touches task-page or shared web UI paths. Failures block the task and do not restart a worker automatically.

Remote review only acknowledges the remote result and moves the task to verification. The remote agent's final summary must state the exact validation commands, ports, and URLs used on that remote machine.

## Browser Reliability

`uat:tasks`, `uat:docs`, and `e2e` install the Playwright-managed Chromium build before running tests. The repo does not use `CHROME_BIN` by default because the system Chromium available in some agent environments can crash before the first page opens. To force a known-good system browser, set `HOMELAB_PLAYWRIGHT_USE_SYSTEM_CHROME=1` with `CHROME_BIN`, or set `PLAYWRIGHT_CHROMIUM_EXECUTABLE` directly.

Outside Nix, install Playwright browsers and OS dependencies with the official Playwright installer before running custom browser commands.

Keep Playwright specs deterministic:

- route or mock `homelabd`, `healthd`, and `supervisord` APIs unless the test is explicitly a live diagnostic
- use relative `page.goto('/route')` URLs so Playwright's `baseURL` selects the isolated server
- avoid fixed ports and external base URLs in tests; use `PLAYWRIGHT_PORT` only when debugging one run
- keep UI assertions user-centred: visible text, roles, enabled/disabled states, selected items, and mobile overflow

## Related Links

- `AGENTS.md` for Definition of Done rules
- `docs/task-workflow.md` for review, approval, and verification states
- `docs/remote-agents.md` for remote worker semantics
- `docs/homelabctl.md` for operator commands
- Git worktree manual: https://git-scm.com/docs/git-worktree.html
- Playwright web server docs: https://playwright.dev/docs/test-webserver
- Playwright CI guidance: https://playwright.dev/docs/ci
- Supervisor process control docs: https://supervisor.readthedocs.io/en/stable/running.html
