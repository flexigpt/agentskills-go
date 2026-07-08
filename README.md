# AgentSkills Runtime for Go

[![License: MIT](https://img.shields.io/badge/License-MIT-brightgreen.svg)](https://opensource.org/licenses/MIT)
[![lint](https://github.com/flexigpt/agentskills-go/actions/workflows/lint.yml/badge.svg?branch=main)](https://github.com/flexigpt/agentskills-go/actions/workflows/lint.yml)
[![test](https://github.com/flexigpt/agentskills-go/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/flexigpt/agentskills-go/actions/workflows/test.yml)

Runtime for [AgentSkills](https://agentskills.io/specification) in Go with pluggable backend providers for skill lifecycle. Includes a bundled filesystem-backed provider.

## Table of contents <!-- omit in toc -->

- [Overview](#overview)
- [Features](#features)
- [Supported SKILL.md extensions](#supported-skillmd-extensions)
- [Prompt format](#prompt-format)
- [Consumer responsibilities](#consumer-responsibilities)
- [Filesystem skill provider](#filesystem-skill-provider)
  - [Quickstart](#quickstart)
  - [Security notes](#security-notes)
- [End-to-end examples](#end-to-end-examples)
- [Development](#development)
- [License](#license)

## Overview

An AgentSkill is a directory or location containing a `SKILL.md` file with YAML frontmatter.

This library is built around progressive disclosure of skills for LLM sessions:

- the runtime maintains a catalog of known skills
- the catalog exposes metadata for discovery
- a session can activate specific skills
- only active skills disclose their full `SKILL.md` body into the prompt

That keeps the base prompt smaller while still allowing the model to discover and load additional skills when needed.

## Features

- Runtime for managing:
  - skill catalog
  - session-scoped active skills
- Provider abstraction via `spec.SkillProvider`
- Reference provider:
  - `providers/fsskillprovider`
- Tool integration via [`llmtools-go`](https://github.com/flexigpt/llmtools-go):
  - `skills-load`
  - `skills-unload`
  - `skills-readresource`
  - `skills-runscript`
- Prompt generation APIs for:
  - available skills
  - active skills
  - combined session prompt output
- FlexiGPT SKILL.md extensions:
  - `insert: instructions | user-message`; `instructions` is the default
  - named string `arguments` with optional defaults; `$name` and `{{name}}` substitution is done only for declared args

## Supported SKILL.md extensions

This runtime supports normal Agent Skills-style `SKILL.md` files and a small extension
for prompt-template use cases.

The supported semantic frontmatter fields are:

- `name`: required skill name
- `description`: required discovery text
- `insert`: optional insertion hint, either `instructions` or `user-message`
- `arguments`: optional list of named string arguments

Missing `insert` means `instructions`.

Use `insert: instructions` for normal skills whose body should be injected into
instruction/context material. This is the default.

Use `insert: user-message` when the skill body is a user-message template. These skills are
not advertised in the normal LLM-facing skills prompt and cannot be loaded into a
session with `skills-load`. Hosts should render them with `Runtime.RenderSkill` and
place the rendered text in the user message area.

The runtime preserves the full parsed frontmatter in `RawFrontmatter`, but it does
not assign behavior to other fields. Wrappers can inspect or use those fields if
they want compatibility with another client.

Example frontmatter:

```yaml
name: summarize-text
description: Summarizes pasted text. Use when the user wants a concise summary.
insert: user-message
arguments:
  - name: text
    description: Text to summarize.
  - name: tone
    description: Summary tone.
    default: concise
```

The body may use `$name`, `{{name}}`, or `{{ name }}` placeholders. Only declared
arguments are substituted. Unknown placeholders are left unchanged and reported as
warnings. Runtime variables such as `${CLAUDE_SESSION_ID}` are not expanded.

Claude Code style dynamic command expansion is not supported. The runtime never
runs commands from `SKILL.md` during import, render, activation, or prompt generation.

## Prompt format

Prompt output is structured plain text intended for LLM consumption.

It is deliberately not XML. Instead, it uses explicit start and end delimiters plus labeled fields so the model can interpret the structure clearly without paying the overhead of XML encoding.

Current behavior:

- available skills are sorted by prompt-visible `name`, then `location`
- available skills include only `insert: instructions` skills
- active skills preserve session active order
- empty sections render as `(none)`
- when both sections are requested together, the runtime wraps them in a combined `<<<SKILLS_PROMPT>>> ... <<<END_SKILLS_PROMPT>>>` block

Typical shapes look like this.

Available skills:

```text
<<<AVAILABLE_SKILLS>>>
name: hello-skill
location: /abs/path/to/hello-skill
description: Says hello
---
name: my-skill
location: /abs/path/to/my-skill
description: My Skill
<<<END_AVAILABLE_SKILLS>>>
```

Active skills:

```text
<<<ACTIVE_SKILLS>>>
name: hello-skill
body:
# Hello Skill

Use this skill when the user wants a greeting.
<!-- SKILL SEPARATOR -->
name: my-skill
body:
# My Skill

Use this skill when the user wants to deal with me.
<<<END_ACTIVE_SKILLS>>>
```

## Consumer responsibilities

This library does not decide how your chat product stores, displays, or executes
skills. Consumers and wrappers should make those decisions explicitly.

- If `RenderSkill` returns `Insert == instructions`, put the rendered text in your
  instruction/context area.
- If `RenderSkill` returns `Insert == user-message`, put the rendered text in your
  user message composer/body.
- If a skill body contains command examples or Claude-style dynamic command text,
  this runtime leaves the body as text. It does not execute or sanitize it.
- If you expose `skills-runscript`, treat it as a separate tool capability governed
  by your product policy. The filesystem provider keeps script execution disabled
  by default.
- If you need tags, enable/disable state, built-in state, source URIs, revisions, or
  trust policy, keep them in your wrapper/store layer rather than in `SKILL.md`.
- If you need compatibility fields from other clients, read `RawFrontmatter`; this
  runtime only gives behavior to `name`, `description`, `insert`, and `arguments`.

## Filesystem skill provider

### Quickstart

Create a runtime with the filesystem provider:

```go
fsp, _ := fsskillprovider.New() // RunScript disabled by default

rt, _ := agentskills.New(
  agentskills.WithProvider(fsp),
)
```

Add a skill to the catalog:

```go
rec, err := rt.AddSkill(ctx, spec.SkillDef{
  Type:     "fs",
  Name:     "hello-skill",
  Location: "/abs/path/to/hello-skill",
})
_ = rec
_ = err
```

Build the available-skills prompt for discovery only:

```go
prompt, _ := rt.SkillsPrompt(ctx, &agentskills.SkillFilter{
  Activity: spec.SkillActivityInactive, // without SessionID, treated as all known/inactive skills
})
_ = prompt
```

Create a session with initial active skills:

```go
sid, active, err := rt.NewSession(ctx,
  agentskills.WithSessionActiveSkills([]spec.SkillDef{rec.Def}),
)
_ = sid
_ = active
_ = err
```

Build the active-skills prompt for that session:

```go
activePrompt, _ := rt.SkillsPrompt(ctx, &agentskills.SkillFilter{
  SessionID: sid,
  Activity:  spec.SkillActivityActive,
})
_ = activePrompt
```

Render a skill for a chat UI:

```go
rendered, err := rt.RenderSkill(ctx, agentskills.RenderSkillParams{
  Def: rec.Def,
  Arguments: map[string]string{
    "text": "Long pasted content...",
    "tone": "concise",
  },
})
_ = rendered
_ = err
```

If `rendered.Insert` is `spec.SkillInsertUserMessage`, place `rendered.Text` in the user message area.
If it is `spec.SkillInsertInstructions`, place it in instruction/context material.

Build a combined prompt for a session:

```go
prompt, _ := rt.SkillsPrompt(ctx, &agentskills.SkillFilter{
  SessionID: sid,
  Activity:  spec.SkillActivityAny,
})
_ = prompt
```

Create a tool registry for an LLM session:

```go
reg, _ := rt.NewSessionRegistry(ctx, sid)
_ = reg
```

The registry includes:

- `skills-load`
- `skills-unload`
- `skills-readresource`
- `skills-runscript`

### Security notes

The filesystem provider is intentionally thin and relies on `llmtools-go` for most of the operational sandboxing boundaries.

- `skills-readresource` uses `llmtools-go/fstool` and is scoped to the skill root with:
  - `allowedRoots = [skillRoot]`
  - `workBaseDir = skillRoot`
- `skills-runscript` uses `llmtools-go/exectool` and is scoped similarly
- script execution is disabled by default in the filesystem provider
- enabling script execution is a host decision and is separate from SKILL.md rendering:

```go
fsskillprovider.WithRunScripts(true)
```

## End-to-end examples

Working end-to-end coverage lives in:

- [fs test](./internal/integration/fs_test.go)

It demonstrates:

- creating a runtime
- adding a skill
- listing and prompting skills
- creating a session with initial active skills
- invoking skill tools

## Development

- Formatting follows `gofumpt` and `golines` via `golangci-lint`. Rules are in [.golangci.yml](.golangci.yml).
- Useful scripts are defined in `taskfile.yml`; requires [Task](https://taskfile.dev/).
- Bug reports and PRs are welcome:
  - Keep the public API small and intentional.
  - Avoid leaking provider‑specific types through the public surface; put them under `internal/`.
  - Please run tests and linters before sending a PR.

## License

Copyright (c) 2026 - Present - Pankaj Pipada

All source code in this repository, unless otherwise noted, is licensed under the MIT License.
See [LICENSE](./LICENSE) for details.
