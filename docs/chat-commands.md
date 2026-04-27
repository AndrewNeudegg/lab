# Chat Commands

`homelabd` accepts short operational commands and natural development requests from chat.

From a terminal, use `homelabctl shell` for interactive chat-command operation, or `homelabctl message <text>` for a single message. The CLI is documented in `docs/homelabctl.md` and should be kept current with this command surface.

## Reflection

Use reflection when you want one improvement from the recent interaction and a follow-up task you can action:

```text
reflect on our recent interaction and suggest one improvement
```

The reply includes a `new <goal>` command. In the dashboard this appears as a suggested action button, so you can create the follow-up task directly from the reflection result.

## Task Creation

Use explicit task wording when you want a new durable task instead of a status summary:

```text
new add structured logging to the backup service
task: Add Playwright end-to-end tests for the chat and task components
create a task to fix running task recovery after homelabd restarts
```

`homelabd` treats `new`, `task:`, `create a task to ...`, and similar creation phrases as task creation even when the goal text mentions words like `running`, `active tasks`, or `in progress`.

New local development tasks create one queued task record and one isolated worktree. The task supervisor starts an available worker automatically, or you can run it explicitly:

```text
run <task_id>
delegate <task_id> to codex
```

## UX Agent

Use `UXAgent` when a task changes a page, component, interaction, or visual state and needs a dedicated usability pass:

```text
ux task_123
ux task_123 check the mobile queue and touch targets
delegate task_123 to ux audit the empty, loading, keyboard, and mobile states
```

`UXAgent` works in the same isolated task worktree as `CoderAgent`, but its prompt requires current UX and accessibility research, focused UI changes, automated regression coverage, and browser-level UAT for changed UI. It should consult sources such as WCAG 2.2, WAI-ARIA APG, official framework or design-system docs, and reputable usability research before making UX decisions.

## Remote Agent Tasks

Remote machines are managed outside chat through the task API, dashboard, or `homelabctl`:

```text
homelabctl -addr http://127.0.0.1:18080 agent list
homelabctl -addr http://127.0.0.1:18080 agent show workstation
homelabctl -addr http://127.0.0.1:18080 task new --agent workstation --workdir repo "Update this checkout"
```

`--workdir` names an advertised workdir id. `--workdir-path` may be used for a full advertised path. Unknown workdir ids or paths are rejected so remote tasks do not silently run in a different checkout.

The chat command `agents` lists external CLI backends such as `codex`, `claude`, and `gemini`; it does not list built-in role agents such as `UXAgent`, and it is not the remote-machine inventory. Use the dashboard task queue filters or `homelabctl agent list` for remote agents.

## Search

Use repo search when you want to inspect local code:

```text
search orchestrator
```

Use web search when you need current external information:

```text
web current SvelteKit adapter-auto production deployment guidance
search the web for current SvelteKit adapter-auto production deployment guidance
search internet for Bun workspace package.json docs
```

Web search runs through the `internet.search` tool. Academic wording such as `academic`, `scholarly`, or `papers` searches scholarly sources.

Use research when a quick search result is not enough and the agent needs a source bundle to reason from:

```text
research current SvelteKit adapter production guidance
deep research local LLM agent web research architecture
research academic papers on deep research agents
```

Research runs through `internet.research`. It creates fan-out queries, searches web and/or academic sources, deduplicates URLs, fetches bounded text from top public pages, and returns follow-up queries. If `BRAVE_SEARCH_API_KEY` or `TAVILY_API_KEY` is set, `internet.search` and `internet.research` use those stronger search backends before falling back to DuckDuckGo. `HOMELABD_SEARCH_PROVIDER=brave|tavily|duckduckgo` can force a provider.

## Task Worktree Recovery

External coding agents can edit files in local task worktrees, but the runtime owns git state. If a local task branch becomes too stale to rebase cleanly, refresh it from chat:

```text
refresh 793f04ec
delegate 793f04ec to codex implement the task again from current main
```

`refresh <task_id>` resets the task worktree branch to the current repository `main` commit and leaves the task blocked for explicit redelegation. Use it when repeated review or approval attempts report premerge conflicts from old branch state.

`approve <approval_id>` still executes a pending approval. For merge approvals, the Orchestrator first attempts to reconcile the task branch with current `main`; conflicts move the task to `conflict_resolution` for manual fixes and no merge is applied.

Remote tasks do not have a control-plane task worktree; use `reopen <task_id> <reason>` to queue follow-up work for the same remote target.
