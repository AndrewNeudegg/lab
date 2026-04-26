# Chat Commands

`homelabd` accepts short operational commands and natural development requests from chat.

## Reflection

Use reflection when you want one improvement from the recent interaction and a follow-up task you can action:

```text
reflect on our recent interaction and suggest one improvement
```

The reply includes a `new <goal>` command. In the dashboard this appears as a suggested action button, so you can create the follow-up task directly from the reflection result.

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
