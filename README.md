# Agent Skills Runtime for Go

[![License: MIT](https://img.shields.io/badge/License-MIT-brightgreen.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/flexigpt/agentskills-go)](https://goreportcard.com/report/github.com/flexigpt/agentskills-go)
[![lint](https://github.com/flexigpt/agentskills-go/actions/workflows/lint.yml/badge.svg?branch=main)](https://github.com/flexigpt/agentskills-go/actions/workflows/lint.yml)
[![test](https://github.com/flexigpt/agentskills-go/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/flexigpt/agentskills-go/actions/workflows/test.yml)

- Runtime + filesystem skill runtime implementation for "AgentSkills" in Go.
- An "AgentSkill" is a directory/location containing a `SKILL.md` file with YAML frontmatter. Full specification at the official [site](https://agentskills.io/specification).
- The tools are implemented using the specification and data types provided in [llmtools-go](https://github.com/flexigpt/llmtools-go) repo.

## Table of contents <!-- omit in toc -->

- [Features at a glance](#features-at-a-glance)
- [Filesystem skill provider](#filesystem-skill-provider)
  - [Quickstart](#quickstart)
  - [Security model notes (FS provider)](#security-model-notes-fs-provider)
- [End to end examples](#end-to-end-examples)
- [Development](#development)
- [License](#license)

## Features at a glance

- A runtime that hosts a catalog of skills and manages _session-scoped active skills_.

- Progressive disclosure:
  - the catalog exposes _metadata only_
  - a session "loads" a skill to disclose its full `SKILL.md` body into the prompt

- A provider abstraction (`spec.SkillProvider`) and a hardened reference provider:
  - `providers/fsskillprovider`: skills backed by a local filesystem directory

- Tool wiring via [`llmtools-go`](https://github.com/flexigpt/llmtools-go):
  - `skills.load`, `skills.unload`, `skills.readresource`, `skills.runscript`
  - A runtime prompt API `Runtime.SkillsPromptXML(...)` which can emit:
    - `<availableSkills>...</availableSkills>`
    - `<activeSkills>...</activeSkills>`
    - or a wrapper `<skillsPrompt>...</skillsPrompt>` when both are requested.

## Filesystem skill provider

### Quickstart

- Create a runtime with the filesystem provider

  ```go
  fsp, _ := fsskillprovider.New() // RunScript disabled by default

  rt, _ := agentskills.New(
    agentskills.WithProvider(fsp),
  )
  ```

- Add a skill to the catalog

  ```go
  rec, err := rt.AddSkill(ctx, spec.SkillDef{
    Type:     "fs",
    Name:     "hello-skill",
    // This is base dir containing SKILL.md for "hello-skill".
    // Typically, base dir name and skill name should match.
    Location: "/abs/path/to/hello-skill",
  })
  _ = rec
  _ = err
  ```

- Build the “available skills” prompt XML (metadata only)

  ```go
  xml, _ := rt.SkillsPromptXML(ctx, &agentskills.SkillFilter{
    Activity: agentskills.SkillActivityInactive, // with no SessionID: treated as "all skills"
  })
  // <availableSkills> ... </availableSkills>
  ```

- Create a session with initial active skills (progressive disclosure)

  ```go
  sid, active, err := rt.NewSession(ctx,
    agentskills.WithSessionActiveSkills([]spec.SkillDef{rec.Def}),
  )
  _ = sid
  _ = active // []spec.SkillDef
  _ = err
  ```

- Build “active skills” prompt XML

  ```go
  activeXML, _ := rt.SkillsPromptXML(ctx, &agentskills.SkillFilter{
    SessionID: sid,
    Activity:  agentskills.SkillActivityActive,
  })
  // <activeSkills>
  //   <skill name="hello-skill"><![CDATA[ ... SKILL.md body ... ]]></skill>
  // </activeSkills>
  ```

- Build a combined prompt (active + available/inactive) for a session

  ```go
  xml, _ := rt.SkillsPromptXML(ctx, &agentskills.SkillFilter{
    SessionID: sid,
    Activity:  agentskills.SkillActivityAny,
  })
  // <skillsPrompt>
  //   <availableSkills>...</availableSkills>
  //   <activeSkills>...</activeSkills>
  // </skillsPrompt>
  ```

- Create a tool registry for an LLM session

  ```go
  reg, _ := rt.NewSessionRegistry(ctx, sid)
  // Registry includes: skills.load / skills.unload / skills.readresource / skills.runscript
  _ = reg
  ```

### Security model notes (FS provider)

The FS provider is intentionally thin and delegates most sandboxing/hardening to `llmtools-go`:

- `skills.readresource` uses `llmtools-go/fstool` and is scoped to the skill root via:
  - `allowedRoots = [skillRoot]`
  - `workBaseDir = skillRoot`
- `skills.runscript` uses `llmtools-go/exectool` and is scoped similarly
- `RunScript` is disabled by default; enable explicitly via `fsskillprovider.WithRunScripts(true)`

## End to end examples

Working end-to-end examples live in:

- [fs test](./internal/integration/fs_test.go)
  - Demonstrates: create runtime, add skill, list/prompt, create session with initial actives, run tools.

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
