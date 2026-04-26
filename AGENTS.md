## Definition of Done

- Do not mark UI work complete from compile/unit checks alone. For changed pages, run a browser-level check against the live page or explicitly state that browser UAT was not possible.
- For dashboard task-page changes, run `nix develop -c bash -lc 'cd web && bun run uat:tasks'` against the running stack after restarting the dashboard.
- Browser UAT must exercise the user-reported interaction, not just page load. Check visible data, button state changes, selected item changes, and mobile viewport behavior when relevant.
- If the app is served by `supervisord`, restart the affected app before browser UAT so the test is hitting the new bundle.
- Add automated regression coverage for fixed bugs. Prefer extracting pure view/state logic into testable modules instead of only adding source-string assertions.
- A final handoff for UI changes must say which browser/UAT command ran and what interaction it verified.

## homelabctl

- Use `homelabctl` for interactive `homelabd` operation instead of ad hoc HTTP calls. See `docs/homelabctl.md`.
- Keep `docs/homelabctl.md`, `cmd/homelabctl`, and the `homelabd` HTTP API in sync. If `homelabctl` is not useful enough for a new chat, task, approval, event, or terminal workflow, extend it and add regression tests rather than bypassing it.
