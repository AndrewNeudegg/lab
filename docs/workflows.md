# Workflows

Workflows are durable LLM, tool, wait, and workflow-chain steps. They let `homelabd` encode repeatable logic outside a chat turn while keeping cost, status, and latest run output visible.

The first implementation executes steps sequentially. `depends_on` is stored on steps for future fan-in and fan-out support, but it is not scheduled as a graph yet.

## Dashboard

Open `/workflows` to create and monitor workflows.

- `Active` shows draft, running, waiting, and approval-waiting workflows.
- `All` shows historical completed, failed, and cancelled workflows as well.
- Each row shows status text, short id, and the estimated runtime in minutes.
- The detail pane shows LLM calls, tool calls, waits, estimated runtime, step definitions, and the latest run output.

The `New workflow` form accepts optional step lines:

```text
llm | Plan approach | Decide the next action from the workflow goal
tool | Search docs | internet.search | {"query":"agent workflow design"}
wait | Health gate | healthd reports healthy | 300
workflow | Chain existing workflow | workflow_123
```

Leaving steps empty creates one LLM planning step from the workflow goal.

## Chat Commands

```text
workflows
workflow new Research bundle: Find current sources and summarise risk
workflow show <workflow_id>
workflow run <workflow_id>
```

Short workflow ids are accepted when they are unambiguous.

## LLM Tool Surface

The Orchestrator exposes these pseudo-tools to the LLM:

- `workflow.create`: create a durable workflow definition with LLM, tool, wait, or workflow steps.
- `workflow.list`: inspect existing workflows, status, and cost estimates.
- `workflow.show`: inspect one workflow before deciding whether to reuse it.
- `workflow.run`: execute a workflow through the normal policy-bound tool path.

Tool steps run through `OrchestratorAgent` policy checks. A step that needs approval pauses the workflow as `awaiting_approval`; a wait step pauses as `waiting` with its condition recorded.

## HTTP And CLI

`homelabd` HTTP mode exposes:

- `GET /workflows`
- `POST /workflows`
- `GET /workflows/{id}`
- `POST /workflows/{id}/run`

Use `homelabctl` instead of ad hoc HTTP calls:

```bash
go run ./cmd/homelabctl workflow new "Research bundle: Find current sources"
go run ./cmd/homelabctl workflow list
go run ./cmd/homelabctl workflow show workflow_123
go run ./cmd/homelabctl workflow run workflow_123
```
