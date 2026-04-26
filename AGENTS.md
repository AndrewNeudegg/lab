## Definition of Done

- Do not mark UI work complete from compile/unit checks alone. For changed pages, run a browser-level check against the live page or explicitly state that browser UAT was not possible.
- For dashboard task-page changes, run `nix develop -c bash -lc 'cd web && bun run uat:tasks'` against the running stack after restarting the dashboard.
- Browser UAT must exercise the user-reported interaction, not just page load. Check visible data, button state changes, selected item changes, and mobile viewport behavior when relevant.
- If the app is served by `supervisord`, restart the affected app before browser UAT so the test is hitting the new bundle.
- Add automated regression coverage for fixed bugs. Prefer extracting pure view/state logic into testable modules instead of only adding source-string assertions.
- A final handoff for UI changes must say which browser/UAT command ran and what interaction it verified.
- Documentation must be written and kept in sync with behaviour, commands, UI, configuration, tools, and workflows in the same change. If no docs update is needed, state why in the handoff.
- Documentation is for humans and LLMs. Keep it concise, use British spelling, and emphasise discoverability and usability with clear titles, searchable terms, related links, and current examples.
