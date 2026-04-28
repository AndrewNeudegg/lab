# Agent Tool Reference

`homelabd` gives LLM agents a JSON tool surface for repository work, research, validation, workflows, and controlled operations. The Go runtime is the authority: agents propose tool calls, `homelabd` validates them against the registry and policy, records events, and only then executes them.

## Tool Call Shape

Orchestrator-style agents respond with one JSON object:

```json
{
  "message": "short user-facing status",
  "done": false,
  "tool_calls": [
    {
      "tool": "repo.search",
      "args": {
        "workspace": "/home/lab/lab/workspaces/task_123",
        "query": "StatusQueued",
        "context_lines": 2
      }
    }
  ]
}
```

Each `args` object must match the tool's schema. Empty args are treated as `{}`. Results are JSON and are stored in the event log as `tool.result`; denied calls are stored as `tool.call.denied`.

## Policy And Approval

Tools declare one of these risk levels:

- `read_only`: allowed when the actor is permitted to use the tool.
- `low`: allowed for trusted agents.
- `medium`: allowed automatically only when the args are scoped to a task workspace. The policy checks `workspace`, `dir`, or `path` for `workspaces/`.
- `high`: creates an approval request before execution.
- `critical`: reserved for tools that must include an explicit `target`; otherwise denied.

`policy.require_approval_for` in `config.json` can force approval for specific tool names regardless of risk. `shell.run_limited` also upgrades known destructive command arrays to high risk.

Default role access:

- `OrchestratorAgent`: task and workflow pseudo-tools, external delegation, memory read/proposal, text, internet, repo read/diff, git read/write/worktree, Go/Bun validation, limited and approved shell.
- `CoderAgent`: text, internet, repo read/write patch/diff, git read, Go/Bun validation, limited shell.
- `UXAgent`: same as `CoderAgent`, with UX/browser-UAT expectations in its prompt.
- `ResearchAgent`: text, internet, and memory proposals.
- `ReviewerAgent`: text, internet, repo read/search/diff, git read/merge check, Go/Bun validation.
- `OpsAgent`: `service.status` and `health.errors`.
- `homelabd`, `human`, and `policy`: all tools, still subject to runtime checks and approval handling.

## Repository Tools

Paths are repository-relative unless `workspace` is supplied. Absolute paths and `..` are rejected by repo path helpers.

- `repo.list`: required args: none. Optional args: `path`, `workspace`. Lists up to 500 entries and skips `.git`, `workspaces`, and `data`.
- `repo.read`: required args: `path`. Optional args: `workspace`. Reads one bounded text file. The default maximum is `limits.max_file_bytes`, currently 1 MiB in `config.example.json`.
- `repo.search`: required args: `query`. Optional args: `path`, `workspace`, `context_lines`, `max_results`. Searches plain substrings, not regex, and skips binary files. `context_lines` clamps to 0-8; `max_results` defaults to 100 and clamps to 200.
- `repo.write_patch`: required args: `workspace`, `patch`. Applies a unified diff with `git apply --whitespace=nowarn` inside an isolated task workspace. Medium risk.
- `repo.current_diff`: required args: `workspace`. Returns tracked diff plus untracked file diffs, excluding `.codex` metadata.
- `repo.apply_patch_to_main`: required args: `patch`. Optional args: `target`. Admin/approval path for applying a patch to the configured repo root. High risk.
- `repo.reset_workspace`: required args: `workspace`. Runs `git reset --hard HEAD` in a task workspace. Medium risk and not in normal agent role lists.

Common examples:

```json
{"tool":"repo.search","args":{"workspace":"/home/lab/lab/workspaces/task_123","path":"pkg","query":"RiskHigh","context_lines":2,"max_results":20}}
```

```json
{"tool":"repo.write_patch","args":{"workspace":"/home/lab/lab/workspaces/task_123","patch":"diff --git a/docs/example.md b/docs/example.md\nnew file mode 100644\n--- /dev/null\n+++ b/docs/example.md\n@@ -0,0 +1,2 @@\n+# Example\n+Text.\n"}}
```

## Git Tools

Git tools run `git -C <dir> ...`. Pathspec arrays reject absolute paths and `..`.

- `git.status`: required args: `dir`. Runs `git status --short`.
- `git.diff`: required args: `dir`. Optional args: `base`, `head`, `staged`, `stat`, `name_status`, `context_lines`, `paths`. Adds the requested diff flags, refs, and safe pathspecs.
- `git.branch`: required args: `dir`. Shows the current branch.
- `git.describe`: required args: `dir`. Optional args: `ref`, `tags`, `dirty`, `always`. Wraps `git describe`.
- `git.log`: required args: `dir`. Optional args: `ref`, `max_count`, `paths`. Shows one-line decorated history. `max_count` defaults to 20 and clamps to 100.
- `git.show`: required args: `dir`. Optional args: `ref`, `stat`, `name_status`. Defaults `ref` to `HEAD`.
- `git.commit`: required args: `dir`, `message`. Optional args: `all`, `allow_empty`, `paths`. Stages selected paths or all changes, then commits. High risk.
- `git.revert`: required args: `dir`, `commit`. Optional args: `no_commit`, `mainline`. High risk.
- `git.merge`: required args: `dir`, `branch`. Optional args: `no_ff`, `squash`, `no_commit`, `message`. High risk.
- `git.merge_check`: required args: `branch`, `target`. Checks a branch can merge into the configured repo root without modifying the worktree.
- `git.worktree_create`: required args: `task_id`. Creates an isolated task worktree and branch.
- `git.worktree_remove`: required args: `workspace`. Optional args: `force`. Removes an isolated worktree. Medium risk.
- `git.merge_approved`: required args: `branch`, `target`. Optional args: `workspace`, `message`. Internal approved merge path. Commits workspace changes if supplied, checks merge readiness, requires a clean target, then merges with `--no-ff`. High risk.

Normal task review uses `git.merge_approved`; agents should not merge task branches directly.

## Shell Tools

Both shell tools execute command arrays directly with no shell expansion.

- `shell.run_limited`: required args: `dir`, `command`. Only allowlisted commands run without approval. The default timeout is `limits.max_shell_seconds`, currently 60 seconds in `config.example.json`.
- `shell.run_approved`: required args: `dir`, `command`, `target`. Runs after policy approval when a command is high risk or outside the limited allowlist.

`shell.run_limited` allows read-only inspection commands such as `pwd`, `cat`, `ls`, `wc`, `head`, `tail`, `grep`, `rg`, and `find` when their path arguments are relative and do not traverse upwards. It rejects `find` execution or write actions such as `-exec`, `-delete`, and `-fprint`, and rejects `rg --pre` preprocessors.

Allowlisted validation commands include:

```text
go test ./cmd/... ./pkg/... ./constraints
go build ./cmd/... ./pkg/... ./constraints
go fmt ./cmd/... ./pkg/... ./constraints
make test
make build
make fmt
bun run --cwd web check
bun run --cwd web build
bun run --cwd web test
bun run --cwd web browser:preflight
bun run --cwd web uat:tasks
bun run --cwd web uat:site
```

The same Bun commands are allowed through `nix develop -c ...`. Destructive commands such as `rm`, `mv`, `cp`, `git clean`, `git reset`, `git restore`, `git rm`, and `git checkout -- <path>` are high risk and require approval.

## Validation Tools

These tools are convenience wrappers around common project checks. They set writable caches for Go and Nix where needed.

- `test.run`: required args: `dir`. Alias for the repository Go test suite.
- `go.test`: required args: `dir`. Runs `go test ./cmd/... ./pkg/... ./constraints`.
- `go.build`: required args: `dir`. Runs `go build ./cmd/... ./pkg/... ./constraints`.
- `go.fmt`: required args: `dir`. Runs `go fmt ./cmd/... ./pkg/... ./constraints`; this can modify files.
- `bun.check`: required args: `dir`. Runs `bun install` then `bun run check`, or falls back through Nix.
- `bun.build`: required args: `dir`. Runs `bun install` then `bun run build`, or falls back through Nix.
- `bun.test`: required args: `dir`. Runs `bun install` then `bun run test`, or falls back through Nix.
- `bun.uat.tasks`: required args: `dir`. Runs isolated dashboard task-page Playwright UAT. Minimum timeout is two minutes.
- `bun.uat.site`: required args: `dir`. Runs isolated site-wide Playwright UAT. Minimum timeout is four minutes.

For UI work, browser UAT must use the task worktree or remote workdir, not the production dashboard. See `docs/agentic-testing.md`.

## Internet And Research Tools

Internet tools are read-only for local state but they do make outbound network requests.

- `internet.search`: required args: `query`. Optional args: `source`, `provider`, `max_results`, `time_range`, `language`. `source` is `web`, `academic`, or `all`; default is `web`. `provider` is `auto`, `searxng`, `brave`, `tavily`, or `duckduckgo`. `max_results` defaults to 8 and clamps to 20. Academic search uses OpenAlex.
- `internet.research`: required args: `query`. Optional args: `source`, `depth`, `provider`, `time_range`, `language`, `max_searches`, `max_sources`, `fetch`, `trusted_domains`. It plans subqueries, searches, deduplicates URLs, optionally fetches top pages, and returns an evidence bundle. `depth` defaults to `standard`; `max_searches` clamps to 8 and `max_sources` to 20.
- `internet.fetch`: required args: `url`. Optional args: `max_chars`. Fetches public HTTP(S) URLs only. Private hosts are rejected. Reads at most 2 MiB from the response and returns extracted text clamped to 500-20,000 characters, default 12,000.

Research depth defaults:

- `quick`: two searches, four sources, no page fetch by default, 1,500 characters per source if fetch is forced.
- `standard`: four searches, eight sources, page fetch enabled, 3,000 characters per fetched source.
- `deep`: eight searches, sixteen sources, page fetch enabled, 6,000 characters per fetched source.

Use `internet.fetch` on promising result URLs before relying on page details, and prefer official, primary, standards, maintainer, or scholarly sources when sources disagree.

## Text Tools

- `text.correct`: required args: `text`. Optional args: `mode`, `locale`, `max_variants`. Deterministic spelling/light grammar correction. `mode` is `spelling`, `grammar`, `search_query`, or `all`; default is `all`. `locale` is `en-US` or `en-GB`; default is `en-US`. Input is limited to 2,000 characters; variants clamp to 1-8.
- `text.summarize`: required args: `text`. Optional args: `purpose`, `max_characters`. Summarises into a compact label with the configured LLM provider, or an extractive fallback. `purpose` is `task_title` or `generic`; default max is 84 characters and the range is 12-200. Input is limited to 8,000 characters.

`text.correct` is useful before web search for typo-prone user text. Preserve exact code symbols, names, and quoted strings when precision matters.

## Task And Workflow Tools

`task.create`, `task.run`, and `workflow.*` are pseudo-tools handled by the Orchestrator, not registered package tools.

- `task.create`: required args: `goal`. Optional args: `target`. Creates a durable local task or remote-target task. `target` may include `mode`, `agent_id`, `machine`, `workdir_id`, `workdir`, and `backend`.
- `task.run`: required args: `task_id`. Starts `CoderAgent` on an existing task after the planning gate.
- `task.list`: required args: none. Lists durable task records, newest first.
- `workflow.create`: required args: `name`. Optional args: `description`, `goal`, `steps`. Creates a durable workflow. Steps may be `llm`, `tool`, `wait`, or `workflow`.
- `workflow.list`: required args: none. Lists workflows with status and estimates.
- `workflow.show`: required args: `workflow_id`. Shows one workflow, its steps, latest run, and estimate.
- `workflow.run`: required args: `workflow_id`. Runs or resumes a workflow through normal policy checks. Tool steps that need approval pause the workflow.

Workflow step arguments use the fields documented in `docs/workflows.md`: `name`, `kind`, `prompt`, `tool`, `args`, `workflow_id`, `condition`, `timeout_seconds`, and `depends_on`.

## External Agent Tools

- `agent.list`: required args: none. Lists configured external CLI backends such as Codex, Claude, or Gemini. It does not list remote machines.
- `agent.delegate`: required args: `backend`, `task_id`, `workspace`, `instruction`. Optional args: `run_id`. Runs an external CLI backend in the supplied task workspace. Medium risk and expected to stay workspace-scoped.

External CLI backends may modify a task worktree, but they cannot approve or merge their own work. See `docs/external-agents.md`.

## Memory Tools

- `memory.read`: required args: `name`. Reads a markdown memory file.
- `memory.propose_write`: required args: `name`, `content`. Writes a proposal file; it does not commit memory.
- `memory.commit_write`: required args: `name`, `content`, `target`. Commits memory after approval. High risk and not in normal agent role lists.

## Health And Service Tools

- `health.errors`: required args: none. Optional args: `limit`, `app`, `source`. Reads recent application errors from healthd. `limit` supports 1-500.
- `service.status`: required args: `service`. Returns a placeholder `unknown` status in v0.1.
- `service.restart`: required args: `service`, `target`. Registered but disabled in v0.1; returns an error. High risk.
- `service.reload_config`: required args: `service`, `target`. Registered but disabled in v0.1; returns an error. High risk.

Agents must not restart production `dashboard`, `homelabd`, `healthd`, or `supervisord` for validation. Use supervised service commands only for explicit operator maintenance after merge.

## Known Limits

- The registry is built at `homelabd` startup in `cmd/homelabd/main.go`; changing a tool implementation requires restarting `homelabd` after merge.
- Agents have a bounded number of tool calls per turn: `limits.max_tool_calls_per_turn`, default 12. Task-running agents get double that budget.
- Repository tools are intentionally simple and bounded; use `repo.search` for deterministic substring search, not semantic search or regex.
- Internet tools depend on configured providers and public network reachability. Fallbacks can change result quality.
- Browser UAT commands start isolated servers from the task worktree and mocked APIs. Live production diagnostics are separate operator actions.

## Related Links

- `docs/task-workflow.md` for task states, review, approval, and verification.
- `docs/workflows.md` for durable workflow syntax and examples.
- `docs/external-agents.md` for Codex, Claude, Gemini, and UX delegation semantics.
- `docs/agentic-testing.md` for isolated browser UAT and validation rules.
- `docs/homelabctl.md` for operator commands that wrap HTTP APIs.
- `pkg/tool/policy.go` for the default role allow-lists and risk decisions.
- `pkg/tools/*` for concrete tool schemas and runtime behaviour.
