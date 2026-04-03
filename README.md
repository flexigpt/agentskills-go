# AgentSkills Runtime for Go

[![License: MIT](https://img.shields.io/badge/License-MIT-brightgreen.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/flexigpt/agentskills-go)](https://goreportcard.com/report/github.com/flexigpt/agentskills-go)
[![lint](https://github.com/flexigpt/agentskills-go/actions/workflows/lint.yml/badge.svg?branch=main)](https://github.com/flexigpt/agentskills-go/actions/workflows/lint.yml)
[![test](https://github.com/flexigpt/agentskills-go/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/flexigpt/agentskills-go/actions/workflows/test.yml)

Runtime for [AgentSkills](https://agentskills.io/specification) in Go with pluggable backend providers for skill lifecycle. Includes a bundled filesystem-backed provider.

## Table of contents <!-- omit in toc -->

- [Overview](#overview)
- [Features](#features)
- [Prompt format](#prompt-format)
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

## Prompt format

Prompt output is structured plain text intended for LLM consumption.

It is deliberately not XML. Instead, it uses explicit start and end delimiters plus labeled fields so the model can interpret the structure clearly without paying the overhead of XML encoding.

Current behavior:

- available skills are sorted by prompt-visible `name`, then `location`
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
- script execution is disabled by default
- enable script execution explicitly with:

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
