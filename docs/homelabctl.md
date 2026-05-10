# homelabctl

`homelabctl` is the supported command-line operator interface for `homelabd` HTTP mode. Use it instead of ad hoc `curl` for task, Assistant, Goal, workflow, approval, event, chat, terminal, and supervisor interactions.

Start `homelabd` in HTTP mode before using the CLI:

```bash
go run ./cmd/homelabd -mode http -config config.json
```

The default API address is `http://127.0.0.1:18080`. Override it with `-addr` or `HOMELABD_ADDR`:

```bash
go run ./cmd/homelabctl -addr http://127.0.0.1:18080 health
HOMELABD_ADDR=http://127.0.0.1:18080 go run ./cmd/homelabctl tasks
```

Healthd has its own API address. Override it for healthd commands with `-healthd-addr` or `HOMELABD_HEALTHD_ADDR`; the default is `http://127.0.0.1:18081`.

Supervisord also has its own API address. Override it for supervisor commands with `-supervisord-addr` or `HOMELABD_SUPERVISORD_ADDR`; the default is `http://127.0.0.1:18082`.

## Operator Rule

Keep this document, `cmd/homelabctl`, and the `homelabd`, `healthd`, and `supervisord` HTTP APIs in sync. When a new operator interaction is added to one of those APIs, add or update the matching `homelabctl` command and tests in the same change. If a workflow requires repeated chat, task, Assistant, Goal, approval, event, terminal, or supervisor interaction and `homelabctl` is not useful enough, extend the CLI rather than bypassing it.

## Interactive Shell

Use the shell for chat commands and natural operator requests:

```bash
go run ./cmd/homelabctl shell
```

Inside the shell, enter the same commands accepted by `homelabd` chat, for example:

```text
status
tasks
show task_123
remember prefer distilled lessons over copied phrasing
memories
goal keep the daily brief current and point out unanswered mail
goals
delegate task_123 to codex fix the failing tests
ux task_123 audit keyboard and mobile states
approve app_123
```

Type `exit` or `quit` to leave. Use `-from` or `HOMELABCTL_FROM` to change the sender name recorded in chat logs:

```bash
go run ./cmd/homelabctl -from codex shell
```

For one-shot chat commands:

```bash
go run ./cmd/homelabctl message "status"
go run ./cmd/homelabctl chat "status"
go run ./cmd/homelabctl chat clear chat_123
go run ./cmd/homelabctl chat clear --all
go run ./cmd/homelabctl remember "prefer concise validation summaries"
go run ./cmd/homelabctl memories
go run ./cmd/homelabctl status
go run ./cmd/homelabctl agents
go run ./cmd/homelabctl workspaces
```

`message`, `chat`, `say`, and chat-command shortcuts print the plain `reply` field by default. `chat clear <conversation_id>` calls the typed clear endpoint for one dashboard conversation, while `chat clear --all` clears all server-side chat transcript context. Add `-json` when the full response object is needed.
The JSON response includes `stats` for dashboard chat metadata when homelabd can measure it, currently `model_turns`, `tool_calls`, token counts, and total response `elapsed_ms`.

## Task Commands

Direct task commands use typed HTTP endpoints. Most print pretty JSON; `task diff` prints a review-friendly patch by default:

```bash
go run ./cmd/homelabctl task new "Add dashboard regression tests"
go run ./cmd/homelabctl task new --attach ./browser-context.json "Fix the bug shown in the context"
go run ./cmd/homelabctl task new --project remote1 "Build the remote1 feature"
go run ./cmd/homelabctl task new --local "Improve homelabd routing"
go run ./cmd/homelabctl task new --agent workstation --workdir repo "Update this checkout"
go run ./cmd/homelabctl task list
go run ./cmd/homelabctl task attention
go run ./cmd/homelabctl task show task_123
go run ./cmd/homelabctl task runs task_123
go run ./cmd/homelabctl task diff task_123
go run ./cmd/homelabctl task run task_123
go run ./cmd/homelabctl task review task_123
go run ./cmd/homelabctl task review-ui task_123
go run ./cmd/homelabctl task queue task_123 up
go run ./cmd/homelabctl task accept task_123
go run ./cmd/homelabctl task restart task_123
go run ./cmd/homelabctl task reopen task_123 "needs rework"
go run ./cmd/homelabctl task cancel task_123
go run ./cmd/homelabctl task retry task_123 codex "retry from the current workspace state"
go run ./cmd/homelabctl task delete task_123
go run ./cmd/homelabctl settings
go run ./cmd/homelabctl settings auto-merge on
```

List commands are intentionally lightweight. `task list`, `assistant list`, `knowledge list`, and `events` use summary payloads so old results, diffs, snapshots, source content, and event bodies do not make routine checks slow. Task and Assistant stores keep summary sidecars for list commands, the dashboard navbar uses `GET /tasks/attention` for badge counts, and `events --limit` uses the tail of the day's JSONL log. Use the corresponding `show` command, `task diff`, or a diagnostic `detail=full` API query when a complete record is required.

`task retry` preserves the previous task result as retry context and forces an immediate worker attempt. It is the normal recovery path for `timed_out` tasks after raising `external_agents.<backend>.timeout_seconds` or narrowing the instruction. The task supervisor also queues automatic recovery for `conflict_resolution` tasks and retryable blocked tasks. Worker starts are capped by `limits.max_concurrent_tasks`, so recovery waits instead of launching too many browser-UAT or build-heavy tasks at once. In both paths, `homelabd` prepares the isolated task worktree before starting the worker: a clean worktree is merged with current `main`, and any resulting conflicts are left for the worker to resolve.

`task review` normally runs after a local worker has moved the task to `ready_for_review`; the task supervisor starts that review automatically. It can also recheck a blocked task whose result starts with `ReviewerAgent checks failed:` after a test-infrastructure fix. It owns the task while checks run; concurrent run, retry, or delegation attempts are rejected, and a stale review result is ignored if the task state changes before checks finish. If the worker made no edits and its result starts with `No change required:`, review moves the task to `no_change_required`; use `task accept` to close it or `task reopen <reason>` to reject that conclusion and queue new work.

`task review-ui` runs the same merge-review gate under an explicit UI/UX command name. Browser-visible diffs must have a reviewed UI/UX brief, desktop and mobile browser UAT, desktop and mobile accessibility checks, and desktop and mobile screenshot or visual-baseline review before approval.

`task queue <task_id> <up|down>` reorders a local task inside the merge queue. Queue order is durable and conflict-aware: only the head task can review, approve, merge, and complete required restart gates. Use this command when operator priority changes; approving a non-head merge approval leaves it pending instead of bypassing earlier queued work.

`settings auto-merge <on|off>` toggles whether `homelabd` automatically grants merge approvals for the merge-queue head after review passes. Auto merge still respects the queue order, pre-merge reconciliation, required restart gates, and health checks. Leave it off when you want every merge approval to require an operator click.

`task restart` retries an enforced post-merge restart gate for a task in `awaiting_restart`. Review stores required supervised components from the diff, approval moves the merged task into `awaiting_restart`, and `homelabd` blocks `accept` until `supervisord` has restarted each component and its configured health URL has returned 2xx.

The target flags are optional. With no target, `homelabd` auto-routes: self-improvement stays local, a single registered remote workspace is selected, and multiple unclear remotes are rejected until a project is named. Use `--local` to force the control-plane checkout, `--project <project_id>` to let the coordinator route to a named project workspace, or `--agent <agent_id>` with `--workdir <workdir_id>` for a specific remote queue. Use `--workdir-path <path>` when the advertised path is the stable identifier. `--backend` overrides the backend that the remote agent should run.

List project workspaces before creating remote work:

```bash
go run ./cmd/homelabctl workspace list
go run ./cmd/homelabctl projects
```

Use `--attach <path>` one or more times to include local evidence on a new task. Text-like files also include a bounded text preview so workers can read the context from the prompt; all attached files remain visible on the task record.

Top-level aliases are available for common task actions:

```bash
go run ./cmd/homelabctl new "Fix stale running task recovery"
go run ./cmd/homelabctl run task_123
go run ./cmd/homelabctl ux task_123 "audit accessibility, responsive layout, and interaction states"
go run ./cmd/homelabctl review task_123
go run ./cmd/homelabctl queue task_123 down
go run ./cmd/homelabctl accept task_123
go run ./cmd/homelabctl restart task_123
go run ./cmd/homelabctl reopen task_123 "needs mobile UAT"
go run ./cmd/homelabctl cancel task_123
go run ./cmd/homelabctl retry task_123
go run ./cmd/homelabctl delete task_123
go run ./cmd/homelabctl runs task_123
go run ./cmd/homelabctl diff task_123
```

`task diff` and its top-level `diff` alias call `GET /tasks/{task_id}/diff`. Plain output starts with a compact file/addition/deletion summary, prints the diff source and any warning, then prints the raw patch. Reviewed local tasks and new remote tasks return immutable task diff snapshots. If no snapshot exists, the response is marked as a live branch or remote worktree fallback so operators know it may not match historical task state. Add `-json` to inspect the structured response used by the dashboard diff viewer, including `source`, `snapshot`, `captured_at`, and `sha256`.

Some orchestrator actions, such as `delegate`, `ux`, `refresh`, and `test`, are chat commands rather than dedicated HTTP endpoints. `homelabctl` sends those shortcuts through `/message`:

```bash
go run ./cmd/homelabctl delegate task_123 to codex "finish docs and tests"
go run ./cmd/homelabctl ux task_123 "run a UX pass with research, regression tests, and browser UAT"
go run ./cmd/homelabctl refresh task_123
go run ./cmd/homelabctl test task_123
```

`ux <task_id> [instruction]` runs the built-in `UXAgent` in the task worktree. Use it for UI, interaction, accessibility, responsive layout, and visual-state work that should be backed by current UX research and browser-level verification.

Plain `homelabctl message` output prints the reply and any suggested chat buttons as numbered choices. Type the desired label back into chat to follow that branch. Use `-json` when a script needs the raw `/message` response, including the optional `buttons` array.

Agent UI validation must not restart production services. For focused desktop/mobile accessibility and visual checks, workers and reviewers use `nix develop -c bun run --cwd web uat:ui`; for dashboard task-page changes, use `nix develop -c bun run --cwd web uat:tasks`; for broad dashboard shell, navigation, theme, terminal, docs, workflow, health, or supervisor changes, use `nix develop -c bun run --cwd web uat:site`. These commands start an isolated Playwright/Vite server from the task worktree and mock production APIs. See `docs/agentic-testing.md`.

When `homelabd` review invokes `bun.check`, `bun.build`, `bun.test`, `bun.uat.ui`, `bun.uat.tasks`, or `bun.uat.site`, the tool enters the repo's Nix dev shell whenever `flake.nix` is available. This keeps automated review aligned with the documented worker commands and avoids browser-library drift in supervised processes.

## Assistant Commands

Assistant commands wrap proactive run records, durable Goals, signal feedback, and decision archive state. Active runs are the default so agents can keep the decision queue short without deleting receipts:

```bash
go run ./cmd/homelabctl assistant list
go run ./cmd/homelabctl assistant list --archived
go run ./cmd/homelabctl assistant list --all
go run ./cmd/homelabctl assistant show arun_123
go run ./cmd/homelabctl assistant archive arun_123 "no longer required"
go run ./cmd/homelabctl assistant restore arun_123
go run ./cmd/homelabctl assistant signals
go run ./cmd/homelabctl assistant signal sig_chat useful
go run ./cmd/homelabctl assistant signal sig_chat create-task "follow up"
go run ./cmd/homelabctl goal create --title "Daily brief" --kind routine --mode guided --cadence daily --success "brief posted" "Keep the daily brief current and point out unanswered mail"
go run ./cmd/homelabctl goal create --title "Build reporting" --kind build --mode autopilot --target remote --project remote1 --budget-tasks 4 "Build the reporting workflow end to end"
go run ./cmd/homelabctl goals
go run ./cmd/homelabctl goal show goal_123
go run ./cmd/homelabctl goal edit goal_123 --objective "Build the reporting workflow and export review evidence" --budget-tasks -1
go run ./cmd/homelabctl goal check goal_123
go run ./cmd/homelabctl goal autopilot start --budget-tasks 4 goal_123
go run ./cmd/homelabctl goal autopilot pause goal_123
go run ./cmd/homelabctl goal autopilot resume --budget-tasks 8 goal_123
go run ./cmd/homelabctl goal autopilot stop goal_123
go run ./cmd/homelabctl goal pause goal_123
go run ./cmd/homelabctl goal archive goal_123
go run ./cmd/homelabctl goal note goal_123 "Waiting for mail connector credentials"
go run ./cmd/homelabctl goal watch goal_123 "Produce the morning brief"
```

`assistant archive` calls `PATCH /assistant/runs/<run_id>` with `archived: true`, records `homelabctl` as the actor, preserves the run, and moves it out of the active Assistant UI. `assistant restore` clears the archive metadata and returns the run to the active decision space. Use this when an agent has established that an old recommendation is no longer useful and no task, snooze, or dismissal is needed.

`assistant signals` calls `GET /assistant/signals` for the current unresolved signal inbox. `assistant signal` calls `PATCH /assistant/signals/<fingerprint>` with `useful`, `dismiss`, `snooze`, or `create_task`, so operators and agents can feed the Assistant learning loop or create a bounded follow-up task without ad hoc HTTP calls. `create_task` records the created task on the signal and settles matching active run recommendations with the same fingerprint so duplicate recommendations leave the active queue. Assistant-owned surfaces such as `assistant`, `chat`, `tasks`, `dashboard`, `healthd`, and `supervisord` create local control-plane tasks unless the action carries an explicit remote target. `useful` records positive feedback and clears the current inbox item until a later new sighting reopens it.

Goals are durable operator desires rather than one-off tasks. A Goal stores the objective, type, execution mode, target, details, success criteria, constraints, autonomy, cadence, watches, linked tasks, progress notes, run assessments, and, for Autopilot, a supervisor plan. Goal types are `build`, `routine`, `watch`, and `maintenance`. `guided` mode keeps the human in the loop: `goal check` starts a Goal-scoped Assistant run through `POST /assistant/goals/<goal_id>/check` and the operator decides whether to create or accept work. `autopilot` mode lets the Goal create one bounded linked task at a time on its selected local or remote target, use the normal review, merge, restart, and acceptance gates where applicable, and continue until its task limit, runtime limit, blocker, no-progress stop, completion, or stop command is reached. `--budget-tasks` is the API field for the Autopilot task limit; pass a positive integer for a bounded Goal or `-1` for unlimited work. Unlimited work still stops on open questions, blocked phases, policy limits, failed gates, or explicit stop commands. `goal show <goal_id> -json` includes `goal.plan`, `blocker_trace`, `decisions`, and `task_reports`, so operators and agents can see the selected phase, the current blocker, the latest supervisor decision, the structured worker report, and the independent reviewer decision for each accepted linked task. `goal edit` calls `PATCH /assistant/goals/<goal_id>` and can change existing Goal text, details, target, mode, cadence, autonomy, and task limit. Editing an exhausted Autopilot Goal to a task limit that allows more work resumes the Goal and the harness reconciles it against the new objective. Changing the objective, details, type, success criteria, or constraints revises the durable plan before the next task is selected. `goal autopilot start|pause|resume|stop` calls `POST /assistant/goals/<goal_id>/autopilot/<action>`. `goal watch` records something the Assistant should keep an eye on, and accepted Goal-linked tasks report progress back to the Goal with a `GOAL_REPORT:` JSON line containing summary, advancement, phase completion, changed files, validation, follow-ups, blockers, and questions. The harness treats that line as a claim: it separately reviews diff files, validation, and Goal alignment before marking progress. Build Goals also tell workers to maintain a feature or parity matrix in the target repo so each new task can close a concrete slice instead of broadly “continuing”. Chat accepts the same lifecycle through natural phrases such as `goal keep invoices reconciled` or `my goal is to keep the daily brief ready`.

## Knowledge Space Commands

Knowledge Space commands wrap the same typed HTTP API used by `/knowledge`, so operators can seed, inspect, and research spaces without ad hoc HTTP calls:

```bash
go run ./cmd/homelabctl knowledge list
go run ./cmd/homelabctl knowledge show kspace_123
go run ./cmd/homelabctl knowledge create --objective "Compare sources" --created-by "operator" "Cheese examples"
go run ./cmd/homelabctl knowledge update kspace_123 --title "Cheese corpus" --objective "Compare source-grounded cheese notes"
go run ./cmd/homelabctl knowledge source add kspace_123 --file docs/knowledge-space.md "Knowledge Space docs"
go run ./cmd/homelabctl knowledge source add kspace_123 --kind note --content "Brief source text" "Operator note"
go run ./cmd/homelabctl knowledge source add kspace_123 --kind email --content "$(cat exported-email.txt)" "Customer thread"
go run ./cmd/homelabctl knowledge source add kspace_123 --url https://example.com/research
go run ./cmd/homelabctl knowledge source delete kspace_123 ksrc_123
go run ./cmd/homelabctl knowledge query kspace_123 --limit 5 "evidence review"
go run ./cmd/homelabctl knowledge ask kspace_123 "What does the corpus say about evidence?"
go run ./cmd/homelabctl knowledge research kspace_123 --mode brief "What does the space show?"
go run ./cmd/homelabctl knowledge research-run kspace_123 --depth standard --scope "stored corpus" "Create a briefing"
go run ./cmd/homelabctl knowledge research-run kspace_123 --discover --depth deep "Research current evidence patterns"
go run ./cmd/homelabctl knowledge research-run resume kspace_123 krun_123
go run ./cmd/homelabctl knowledge delete kspace_123
```

`knowledge update` edits a space title, objective, or description. `knowledge source delete` removes a source from the active corpus and deletes its source snapshot; saved report artefacts remain historical. `knowledge delete` removes the space JSON record, source snapshots, retrieval index, and run workspaces. `knowledge source add` accepts `--file PATH` for Markdown, docs, PDFs already converted to text, notes, email exports, and connected-resource exports, and stores the file path as the source reference when `--uri` is not supplied. Use `--file -` to read source text from standard input. Use `--url URL` to let `homelabd` fetch, extract, snapshot, and model-analyse an HTML, plain-text, JSON/XML-like, or PDF URL server-side. PDF URL ingestion runs configured Poppler `pdftotext` first, then OCRs scanned/image-only pages with configured `pdftoppm` and `tesseract`; missing extraction dependencies are reported as ingestion failures. `knowledge query`, `knowledge ask`, `knowledge research`, and `knowledge research-run` accept repeated `--source <source_id>` flags when retrieval should be limited to selected sources. Retrieval uses indexed source-section chunk metadata, persists that metadata at `data/knowledge/indexes/<space_id>/chunks.json`, and returns evidence trace metadata including source section, retrieval method, lexical score, semantic score, and source summary. `knowledge ask` also persists the grounded answer as an `ask` report artefact on the space. `knowledge research-run --discover` searches web and academic sources with fetched pages, favours the apparent language of the objective with English fallback, filters off-topic, adult or streaming-site, and non-software source-code results before they enter the candidate pool, evaluates source usefulness, imports accepted candidates as URL sources, records rejected or failed candidates, asks the model whether coverage is sufficient, and runs follow-up searches when gaps remain. Academic-intent objectives use academic discovery first, prefer open-access PDF locations, follow public repository PDF links before falling back to landing-page text, and snapshot full extracted source text rather than depth-truncated excerpts. Per-run workspaces include `state.json`, `events.jsonl`, `sources.json`, `candidates.json`, `loops.json`, `coverage.json`, `evidence.json`, and `report.json` as those artefacts become available. `homelabd` resumes queued or in-progress research runs after restart, preserving existing plans and accepted discovery sources where possible. `knowledge research-run resume <space_id> <run_id>` explicitly resumes a failed run in place: it keeps the same run ID and workspace, reuses saved plans, accepted sources, evidence, loops, candidates, and usage, clears the error, appends a `resumed` event, and starts from the safest persisted stage. It rejects runs that are not failed. `knowledge ask`, `knowledge research`, and `knowledge research-run` require the configured `homelabd` language model provider; provider calls honour retry-after hints and use the configured default-provider chain before surfacing an error. `knowledge research-run` returns a queued run and the space should be polled for completion.

## Workflow Commands

Workflow commands use typed HTTP endpoints for durable LLM/tool workflows:

```bash
go run ./cmd/homelabctl workflow new "Research bundle: Find current sources"
go run ./cmd/homelabctl workflow list
go run ./cmd/homelabctl workflows
go run ./cmd/homelabctl workflow show workflow_123
go run ./cmd/homelabctl workflow run workflow_123
```

`workflow run` starts draft/completed workflows and resumes the latest `waiting` run so wait conditions can be re-checked. Built-in wait checks cover `homelabd health is reachable` and `healthd reports healthy`; timer waits complete after `timeout_seconds`.

Use workflows when repeatable logic should be created, estimated, monitored, and invoked outside one chat turn. See `docs/workflows.md`.

## Remote Agent Commands

Remote machines use the `/agents` inventory. This is separate from the chat command `agents`, which lists external local worker backends such as `codex`, `claude`, and `gemini`. Built-in role agents such as `UXAgent` are invoked with commands like `ux task_123`.

```bash
go run ./cmd/homelabctl agent list
go run ./cmd/homelabctl agent show workstation
```

See `docs/remote-agents.md` for remote agent setup and polling details.

## Approval Commands

```bash
go run ./cmd/homelabctl approval list
go run ./cmd/homelabctl approval approve app_123
go run ./cmd/homelabctl approval deny app_123
go run ./cmd/homelabctl approval edit app_123 '{"goal":"corrected args"}'
```

Top-level `approve` and `deny` aliases are also available:

```bash
go run ./cmd/homelabctl approve app_123
go run ./cmd/homelabctl deny app_123
```

`approval edit` only works while the approval is pending. The replacement payload is the tool args object itself, not an approval wrapper; `homelabd` wraps it as `{"args":...}` for the HTTP API, validates it against the tool schema, and records `approval.edited` with old/new argument hashes.

## Event Commands

Read the current UTC day's event log:

```bash
go run ./cmd/homelabctl events
```

Read a specific day or only the latest events:

```bash
go run ./cmd/homelabctl events 2026-04-26
go run ./cmd/homelabctl events -limit 20
go run ./cmd/homelabctl events -limit 20 2026-04-26
```

## Healthd Error Commands

Read recent application errors captured from supervised app stderr logs:

```bash
go run ./cmd/homelabctl healthd errors
go run ./cmd/homelabctl errors -limit 20 dashboard
go run ./cmd/homelabctl -healthd-addr http://127.0.0.1:18081 healthd errors -source supervisord
```

The command calls `GET /healthd/errors` on the healthd service. It is useful before creating a root-cause task with `homelabctl task new ...`.

## Supervisor Commands

Supervisor commands call the `supervisord` control API directly. Use them for explicit operator restarts after merge or maintenance work instead of raw HTTP calls:

```bash
go run ./cmd/homelabctl supervisor status
go run ./cmd/homelabctl supervisor apps
go run ./cmd/homelabctl supervisor restart homelabd
go run ./cmd/homelabctl supervisor restart dashboard
go run ./cmd/homelabctl supervisor app adopt dashboard 1234
```

The short app forms map to the app endpoints:

```bash
go run ./cmd/homelabctl supervisor start healthd
go run ./cmd/homelabctl supervisor stop dashboard
go run ./cmd/homelabctl supervisor restart dashboard
```

Running `supervisor restart` or `supervisor stop` without an app name targets `supervisord` itself:

```bash
go run ./cmd/homelabctl supervisor restart
go run ./cmd/homelabctl supervisor stop
```

Use `-supervisord-addr` or `HOMELABD_SUPERVISORD_ADDR` when the control API is not on `http://127.0.0.1:18082`.

## Terminal Commands

The terminal commands wrap the same `/terminal/sessions` API used by the dashboard Terminal page. Starting a session runs `./run.sh shell` first when the target working directory contains an executable `run.sh`; otherwise it opens the configured interactive shell.

Create a shell session:

```bash
go run ./cmd/homelabctl terminal start
go run ./cmd/homelabctl terminal start /home/lab/lab
```

Show session metadata and reattach homelabd to an existing persistent terminal session:

```bash
go run ./cmd/homelabctl terminal show term_123
```

Stream session output. The underlying event stream emits SSE ids, a retry hint, and keepalive comments so clients can resume with `GET /terminal/sessions/{id}/events?after=N` or the `Last-Event-ID` header after a disconnect:

```bash
go run ./cmd/homelabctl terminal stream term_123
```

Send input:

```bash
go run ./cmd/homelabctl terminal send term_123 "git status --short"
go run ./cmd/homelabctl terminal input term_123 $'\003'
```

`send` appends a newline for command-style input. `input` sends the text exactly as provided.

Send signals or close the session:

```bash
go run ./cmd/homelabctl terminal interrupt term_123
go run ./cmd/homelabctl terminal signal term_123 terminate
go run ./cmd/homelabctl terminal close term_123
```

## API Coverage

`homelabctl` covers the current `homelabd` HTTP operator API:

- `GET /healthz`
- `POST /message`, returning `reply`, `source`, optional `buttons`, and optional interaction `stats`
- `POST /chat/clear`
- `GET /tasks` (`detail=full` returns complete list records for diagnostics)
- `GET /tasks/attention`
- `POST /tasks`, including optional remote `target`
- `GET /tasks/{id}`
- `GET /tasks/{id}/runs`
- `GET /tasks/{id}/diff`
- `POST /tasks/{id}/run`
- `POST /tasks/{id}/review`
- `POST /tasks/{id}/merge-queue`
- `POST /tasks/{id}/accept`
- `POST /tasks/{id}/restart`
- `POST /tasks/{id}/reopen`
- `POST /tasks/{id}/cancel`
- `POST /tasks/{id}/retry`
- `POST /tasks/{id}/delete`
- `GET /assistant/runs` (`detail=full` returns complete list records for diagnostics)
- `POST /assistant/runs`
- `GET /assistant/runs/{id}`
- `PATCH /assistant/runs/{id}`
- `POST /assistant/runs/{id}/actions/{action_id}`
- `GET /assistant/signals`
- `POST /assistant/signals`
- `PATCH /assistant/signals/{fingerprint}`
- `GET /assistant/goals`
- `POST /assistant/goals`
- `GET /assistant/goals/{id}`
- `PATCH /assistant/goals/{id}`
- `POST /assistant/goals/{id}/check`
- `POST /assistant/goals/{id}/autopilot/start`
- `POST /assistant/goals/{id}/autopilot/pause`
- `POST /assistant/goals/{id}/autopilot/resume`
- `POST /assistant/goals/{id}/autopilot/stop`
- `POST /assistant/goals/{id}/watches`
- `POST /assistant/goals/{id}/notes`
- `GET /knowledge/spaces` (`detail=full` returns complete list records for diagnostics)
- `POST /knowledge/spaces`
- `GET /knowledge/spaces/{id}`
- `PATCH /knowledge/spaces/{id}`
- `DELETE /knowledge/spaces/{id}`
- `POST /knowledge/spaces/{id}/sources`
- `DELETE /knowledge/spaces/{id}/sources/{source_id}`
- `POST /knowledge/spaces/{id}/query`
- `POST /knowledge/spaces/{id}/ask`
- `POST /knowledge/spaces/{id}/research`
- `POST /knowledge/spaces/{id}/research-runs`
- `POST /knowledge/spaces/{id}/research-runs/{run_id}/resume`
- `GET /workflows`
- `POST /workflows`
- `GET /workflows/{id}`
- `POST /workflows/{id}/run`
- `GET /agents`
- `GET /agents/{id}`
- `GET /approvals`
- `POST /approvals/{id}/approve`
- `POST /approvals/{id}/deny`
- `POST /approvals/{id}/edit`
- `GET /events?date=YYYY-MM-DD&limit=N` (`detail=full` returns complete payload bodies for diagnostics)
- `POST /terminal/sessions`
- `GET /terminal/sessions/{id}`
- `GET /terminal/sessions/{id}/events`, including optional `after=N` resume support
- `POST /terminal/sessions/{id}/input`
- `POST /terminal/sessions/{id}/signal`
- `DELETE /terminal/sessions/{id}`

Healthd commands call the separate healthd API rather than `homelabd`:

- `GET /healthd/errors?limit=N&source=SOURCE&app=APP`

Supervisor commands call the separate `supervisord` API rather than `homelabd`:

- `GET /supervisord`
- `GET /supervisord/apps`
- `POST /supervisord/restart`
- `POST /supervisord/stop`
- `POST /supervisord/apps/{name}/start`
- `POST /supervisord/apps/{name}/stop`
- `POST /supervisord/apps/{name}/restart`
- `POST /supervisord/apps/{name}/adopt`

Run the CLI tests after changing the HTTP API or command surface:

```bash
go test ./cmd/homelabctl
```
