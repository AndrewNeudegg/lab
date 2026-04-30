# Diagramming And Brand Colours

Chat and docs render Markdown `mermaid` code fences as diagrams. Use them when a small visual model makes a workflow, state machine, dependency graph, architecture, or handoff easier for a human or agent to understand.

```mermaid
flowchart LR
  Goal[Task goal] --> Plan[Reviewed plan]
  Plan --> Patch[Minimal patch]
  Patch --> Checks[Checks and UAT]
  Checks --> Handoff[Concise handoff]
```

## When To Diagram

- Prefer diagrams for state transitions, machine-to-machine flows, approval gates, retry paths, and multi-step operator workflows.
- Keep diagrams compact: one idea, meaningful labels, and no decorative branches.
- Use the same domain words as the surrounding docs or task summary so search and future agents can connect the diagram to the text.
- If Mermaid syntax fails, the dashboard shows the source block as a fallback; fix the syntax rather than replacing the diagram with an image.

## Brand Palette

The dashboard renderer applies the homelabd palette automatically to Mermaid diagrams. Do not add ad hoc Mermaid theme directives or custom colours unless they match these values.

Light palette:

- Background `#f8fafc`, surface `#ffffff`, primary `#2563eb`, secondary `#0f766e`
- Success `#16a34a`, warning `#d97706`, danger `#dc2626`
- Text `#172033`, muted text `#64748b`, border `#cbd5e1`

Dark palette:

- Background `#0f172a`, surface `#111827`, primary `#60a5fa`, secondary `#2dd4bf`
- Success `#4ade80`, warning `#fbbf24`, danger `#f87171`
- Text `#e2e8f0`, muted text `#94a3b8`, border `#334155`

## Authoring Rules

- Write diagrams as fenced blocks labelled `mermaid`.
- Let the renderer supply light and dark colours. Avoid embedded `init` blocks unless the task explicitly requires a Mermaid option, and then keep colours aligned with the palette above.
- Use `flowchart` for task or system flows, `stateDiagram-v2` for lifecycles, `sequenceDiagram` for interactions, and `graph` for dependencies.
- Include a short text explanation before or after the diagram so the meaning remains available to search, screen readers, and plain-text tools.

Related: `AGENTS.md`, `docs/chat-commands.md`, `docs/dashboard.md`, and `docs/task-workflow.md`.
