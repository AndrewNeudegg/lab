# Chat Commands

`homelabd` accepts short operational commands and natural development requests from chat.

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

External coding agents can edit files in task worktrees, but the runtime owns git state. If a task branch becomes too stale to rebase cleanly, refresh it from chat:

```text
refresh 793f04ec
delegate 793f04ec to codex implement the task again from current main
```

`refresh <task_id>` resets the task worktree branch to the current repository `main` commit and leaves the task blocked for explicit redelegation. Use it when repeated review or approval attempts report premerge conflicts from old branch state.

`approve <approval_id>` still executes a pending approval. For merge approvals, the Orchestrator first attempts to reconcile the task branch with current `main`; conflicts move the task to `conflict_resolution` for manual fixes and no merge is applied.
