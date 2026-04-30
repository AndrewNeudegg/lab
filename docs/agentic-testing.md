# Agentic Testing Workflow

Agent work must be testable without interrupting the operator's running dashboard or `homelabd` stack. Treat the supervised stack as production for this homelab: agents may read health and logs, but they must not stop or restart production `dashboard`, `homelabd`, `healthd`, or `supervisord` for validation.

## Isolation Model

Local tasks run in git worktrees under `repo.workspace_root`. Each task branch is reviewed and merged through `homelabd`; agents should not edit the main checkout directly.

On NixOS, `repo.root` and `repo.workspace_root` must be writable runtime paths, not store paths. `homelabd` creates worktrees before launching local workers, so the `homelabd` service needs write access to the repository Git common directory (`.git`, including `refs` and `worktrees`) and to `repo.workspace_root`. Worker sandboxes may be stricter after the worktree exists, but an outer sandbox must not bind `.git` read-only around the host-side worktree creation step.

Remote tasks run on the selected `homelab-agent` in the selected advertised workdir. The control plane records the result, but it does not create a local worktree, compare remote `HEAD`, or merge remote changes.

Browser UAT starts from the task worktree, not from the supervised dashboard. The default Playwright config derives a stable per-worktree port, starts Vite on `127.0.0.1`, refuses to reuse an existing server on that port, allows up to 90 seconds for first-route SSR warm-up, gives each test up to 60 seconds, and runs with one worker for reproducibility. Set `PLAYWRIGHT_WEB_SERVER_TIMEOUT`, `PLAYWRIGHT_TEST_TIMEOUT`, or `PLAYWRIGHT_EXPECT_TIMEOUT` when debugging unusually slow worker hardware.

## Standard Commands

Run these from the repository root or from the selected remote workdir:

```bash
nix develop -c make build
nix develop -c make test
nix develop -c bun run --cwd web check
nix develop -c bun run --cwd web build
nix develop -c bun run --cwd web test
```

`make test`, `make build`, and `make fmt` use the repository package list (`./cmd/... ./pkg/... ./constraints`) plus writable Go build and module caches under `/tmp`. This avoids Go traversing ignored runtime state such as `data/element/postgres` in long-running homelab checkouts and avoids read-only home cache failures in sandboxed agents.

For dashboard task-page changes, run the isolated UAT:

```bash
nix develop -c bun run --cwd web uat:tasks
```

`uat:tasks` runs Playwright against a local Vite server and mocked `homelabd` APIs. It verifies the mobile task queue, selected task detail, draft preservation, button state, visible data, and horizontal overflow without touching production services.

For broad dashboard shell, navigation, theme, terminal, docs, workflow, health, or supervisor changes, run the site-wide UAT:

```bash
nix develop -c bun run --cwd web uat:site
```

`uat:site` first runs the headless Chromium preflight, then visits every primary dashboard route on desktop and mobile. It mocks `homelabd`, `healthd`, and `supervisord`, exercises one meaningful workflow per page, checks page overflow, escaped text, and clipped controls, and attaches full-page screenshots for review.

Playwright waits up to 90 seconds for the isolated Vite server to answer `/chat`, which gives first-run route compilation time to prebundle browser-only libraries such as Mermaid. Browser specs default to 60 seconds and assertions use a 10-second default expect timeout; docs-route site UAT gets 120 seconds because Mermaid rendering plus full-page screenshots can take longer on slower worker images. Set `PLAYWRIGHT_WEB_SERVER_TIMEOUT_MS` when debugging unusually slow or fast worker images.

To check browser readiness without starting Vite, run:

```bash
nix develop -c bun run --cwd web browser:preflight
```

If the preflight reports missing shared libraries, run through `nix develop` or use a worker image with Playwright browser dependencies. If it reports sandbox or syscall errors such as `Operation not permitted`, move the browser UAT to a worker that permits headless Chromium or use a Playwright server/container designed for browser testing. Do not recover by restarting production services.

The old live diagnostic is still available for explicit operator use:

```bash
DASHBOARD_URL=http://127.0.0.1:5173/tasks nix develop -c bun run --cwd web uat:tasks:live
```

Do not use `uat:tasks:live` as an agent completion gate unless the operator explicitly asks for production verification.

## homelabd Review Gate

Local review runs Go tests, web type checks, web build, web unit tests, and isolated browser UAT when the diff touches browser-tested UI paths. Bun-backed review tools enter the repo's Nix dev shell when a `flake.nix` is present, matching worker commands such as `nix develop -c bun run --cwd web uat:site` and preserving Playwright browser libraries. Task-page-only diffs run `bun.uat.tasks`; shared UI, shell, route, Playwright, or browser tooling diffs run `bun.uat.site`. Failures block the task and do not restart a worker automatically. `homelabd` caps concurrent worker starts with `limits.max_concurrent_tasks`, default `2`, and external worker process groups are terminated when their parent run exits so isolated Vite or Playwright children do not keep consuming CPU after validation.

Remote review only acknowledges the remote result and moves the task to verification. The remote agent's final summary must state the exact validation commands, ports, URLs, and browser environment used on that remote machine.

## Browser Reliability

`uat:tasks`, `uat:docs`, `uat:site`, and `e2e` install the Playwright-managed Chromium build and run `browser:preflight` before running tests. The repo does not use `CHROME_BIN` by default because the system Chromium available in some agent environments can crash before the first page opens. To force a known-good system browser, set `HOMELAB_PLAYWRIGHT_USE_SYSTEM_CHROME=1` with `CHROME_BIN`, or set `PLAYWRIGHT_CHROMIUM_EXECUTABLE` directly.

Outside Nix, install Playwright browsers and OS dependencies with the official Playwright installer before running custom browser commands.

Keep Playwright specs deterministic:

- route or mock `homelabd`, `healthd`, and `supervisord` APIs unless the test is explicitly a live diagnostic
- use relative `page.goto('/route')` URLs so Playwright's `baseURL` selects the isolated server
- avoid fixed ports and external base URLs in tests; use `PLAYWRIGHT_PORT` only when debugging one run
- keep UI assertions user-centred: visible text, roles, enabled/disabled states, selected items, screenshots, and mobile overflow
- keep workers at one for dashboard UAT unless the suite has been made explicitly parallel-safe

## Related Links

- `AGENTS.md` for Definition of Done rules
- `docs/task-workflow.md` for review, approval, and verification states
- `docs/remote-agents.md` for remote worker semantics
- `docs/homelabctl.md` for operator commands
- Git worktree manual: https://git-scm.com/docs/git-worktree.html
- Playwright web server docs: https://playwright.dev/docs/test-webserver
- Playwright CI guidance: https://playwright.dev/docs/ci
- Playwright Docker and remote server guidance: https://playwright.dev/docs/docker
- Playwright visual comparison guidance: https://playwright.dev/docs/test-snapshots
- Supervisor process control docs: https://supervisor.readthedocs.io/en/stable/running.html
