# AgentSkills Runtime for Go

[![License: MIT](https://img.shields.io/badge/License-MIT-brightgreen.svg)](https://opensource.org/licenses/MIT)
[![lint](https://github.com/flexigpt/agentskills-go/actions/workflows/lint.yml/badge.svg?branch=main)](https://github.com/flexigpt/agentskills-go/actions/workflows/lint.yml)
[![test](https://github.com/flexigpt/agentskills-go/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/flexigpt/agentskills-go/actions/workflows/test.yml)

Runtime for [Agent Skills](https://agentskills.io/specification) in Go. It provides provider-independent `SKILL.md` document handling, a session-aware runtime, pluggable skill providers, and a bundled filesystem provider.

## Table of contents <!-- omit in toc -->

- [Overview](#overview)
- [Package layout](#package-layout)
- [Features](#features)
- [Supported SKILL.md extensions](#supported-skillmd-extensions)
  - [Parsing, validation, and tolerance](#parsing-validation-and-tolerance)
  - [Rendering and insertion](#rendering-and-insertion)
- [Prompt format](#prompt-format)
- [Consumer responsibilities](#consumer-responsibilities)
- [Filesystem skill provider](#filesystem-skill-provider)
  - [Quickstart](#quickstart)
  - [Security notes](#security-notes)
- [End-to-end examples](#end-to-end-examples)
- [Development](#development)
- [License](#license)

## Overview

An Agent Skill is a directory or provider-defined location containing a `SKILL.md` document with YAML frontmatter.

The runtime is built around progressive disclosure:

- The host indexes skills into a runtime-owned catalog.
- Discovery prompts expose metadata for available instruction skills.
- A session activates only the skills it needs.
- Only active instruction skills disclose their full rendered body to the LLM prompt.
- User-message templates remain host-controlled and are rendered separately.

This keeps the base prompt small while allowing an LLM to discover and load relevant skills as needed.

## Package layout

The public API is intentionally split by responsibility:

- [`document`](./document) provides provider-independent parsing, rendering, validation, and marshaling of materialized `SKILL.md` documents.
- [`provider`](./provider) defines `SkillDef`, `SkillProvider`, resource metadata, and provider-level contracts.
- [`provider/fs`](./provider/fs) provides the bundled filesystem-backed provider.
- [`runtime`](./runtime) owns the skill catalog, host lifecycle APIs, sessions, prompts, and registry creation.
- [`runtime/spec`](./runtime/spec) contains LLM-facing handles, session/tool contracts, tool definitions, and runtime errors.
- [`runtime/internal/catalog`](./runtime/internal/catalog) and [`runtime/internal/session`](./runtime/internal/session) implement catalog identity, progressive body loading, session state, and tool behavior.

Applications should integrate through the public packages. Provider-canonical keys and the runtime's internal catalog/session packages are implementation details.

## Features

- Host lifecycle APIs for adding, listing, rendering, and removing skills.
- Session-scoped active skills with configurable limits, TTL, capacity, and allowlists.
- Progressive disclosure with lazy, cached skill-body loading.
- A pluggable `provider.SkillProvider` abstraction.
- A bundled filesystem provider at `provider/fs`.
- LLM tool registry integration through [`llmtools-go`](https://github.com/flexigpt/llmtools-go):
  - `skills-load`
  - `skills-unload`
  - `skills-readresource`
  - `skills-runscript` when at least one configured provider supports script execution
- Prompt generation for available skills, active skills, and combined session state.
- Provider-independent `SKILL.md` document APIs:
  - `document.ParseSkillDocument`
  - `document.RenderSkillDocument`
  - `document.MarshalSkillDocument`
- FlexiGPT `SKILL.md` extensions for insertion behavior, string arguments, and tags.
- Resource discovery metadata through `provider.SkillResourceInfo`.

## Supported SKILL.md extensions

The runtime supports normal Agent Skills-style documents and a small set of extensions for host-rendered templates.

The semantic frontmatter fields are:

- `name`: required lowercase hyphenated skill name.
- `description`: required discovery text.
- `insert`: optional insertion behavior, either `instructions` or `user-message`.
- `arguments`: optional named string arguments with optional descriptions and defaults.
- `tags`: optional non-empty strings for host or UI categorization.

The first H1 in the Markdown body becomes the skill `DisplayName`. If no H1 exists, the skill name is used.

`insert: instructions` is the default. These skills are advertised in the LLM-facing prompt and can be activated in a session.

`insert: user-message` is for host-rendered templates. These skills are not advertised in `SkillsPrompt`, cannot be activated with `skills-load`, and must be rendered by the host through `runtime.Runtime.RenderSkill` before their text is placed in a user-message area.

Declared arguments support `$name`, `{{name}}`, and `{{ name }}` placeholders. Only declared arguments are substituted. `\$name` renders as a literal `$name`. Unknown placeholders remain unchanged and are returned as warnings.

The runtime does not expand environment variables, runtime variables, shell syntax, or Claude Code-style dynamic commands. Command-like text in a `SKILL.md` body remains text.

Unknown frontmatter fields are retained in `RawFrontmatter` for wrappers that need compatibility metadata from another client or skill ecosystem.

### Parsing, validation, and tolerance

Use `document.ParseSkillDocument` when a `SKILL.md` document has already been materialized by a database, editor, API client, or another provider.

The parser requires:

- A document no larger than `document.MaxSkillDocumentBytes`, currently 2 MiB.
- Valid UTF-8 without NUL bytes.
- Delimited, readable YAML frontmatter.
- A valid `name` and non-empty `description`.
- A matching normalized name when `ParseSkillDocumentOptions.ExpectedName` is supplied.

The parser is intentionally tolerant for optional metadata. It removes a UTF-8 BOM, normalizes body line endings, trims required text values, and returns warnings when it defaults an unsupported `insert` value, ignores malformed optional arguments or tags, truncates bounded optional text, or encounters an empty body.

`document.RenderSkillDocument` renders a materialized document without registering it in a runtime. `document.MarshalSkillDocument` writes a canonical `SKILL.md` representation while preserving unknown frontmatter fields. Both reject invalid in-memory documents instead of silently repairing them.

The runnable document API example is [`TestReadmeDocumentWorkflow`](./runtime/internal/integration/readme_document_test.go).

### Rendering and insertion

There are two intended rendering paths:

- Use `document.RenderSkillDocument` for a materialized document that has not been registered with a runtime.
- Use `runtime.Runtime.RenderSkill` for a skill already present in the runtime catalog.

`RenderSkill` accepts the exact registered `provider.SkillDef` and optional string argument values. It returns rendered text, insertion behavior, declared arguments, applied values, resource metadata, preserved frontmatter, and warnings.

Active instruction skills are rendered with their declared default argument values when the runtime builds an active-skills prompt. Hosts can provide explicit values through `RenderSkillParams.Arguments` when rendering a skill for a UI or message composer.

Neither rendering path activates a skill, reads a resource, or executes a script.

## Prompt format

`Runtime.SkillsPrompt` produces structured plain text for LLM consumption. It deliberately uses explicit delimiters and labeled fields instead of XML encoding.

The prompt can contain:

- An `<<<AVAILABLE_SKILLS>>>` section with prompt-visible skill names, user-provided locations, and descriptions.
- An `<<<ACTIVE_SKILLS>>>` section with active skill names and their rendered bodies.
- A combined `<<<SKILLS_PROMPT>>>` wrapper when both sections are emitted.

Current behavior:

- Available skills are sorted by LLM-visible name, then user-provided location.
- Active skills preserve activation order.
- Available prompts only include `insert: instructions` skills.
- Active prompts only contain active instruction skills.
- Empty sections render as `(none)`.
- A request for only active or only inactive skills returns that section as the root document.
- A session-scoped `SkillActivityAny` request produces both active and inactive sections inside the combined wrapper.

`SkillFilter.NamePrefix` matches the LLM-visible `spec.SkillHandle.Name`. `SkillListFilter.NamePrefix` matches the host-facing `provider.SkillDef.Name`.

When multiple skills would have the same LLM-visible name and location, the runtime computes an opaque suffix such as `skill-name#1a2b3c4d`. The suffix is derived from host-visible definitions and does not expose provider-canonical locations.

See [`TestRuntime_SkillsPrompt_SectionsOrderingAndFiltering`](./runtime/internal/integration/runtime_test.go) and [`TestRuntime_SkillsPrompt_NamePrefixIsLLMHandleNotHostName`](./runtime/internal/integration/runtime_test.go) for executable prompt-format and filtering coverage.

## Consumer responsibilities

The runtime intentionally does not decide how an application stores, displays, or trusts.

Consumers should:

- Use `provider.SkillDef` for host lifecycle operations such as `AddSkill`, `RemoveSkill`, `ListSkills`, and initial session configuration.
- Use `spec.SkillHandle` only for LLM-facing prompts and skill tools such as `skills-load`, `skills-unload`, `skills-readresource`, and `skills-runscript`.
- Preserve exact host-provided definitions. Lifecycle APIs intentionally do not expose provider canonicalization as a host-facing identity.
- Place rendered `instructions` text in instruction or context material.
- Place rendered `user-message` text in the user message composer or message body.
- Use `runtime.WithSessionAllowedSkills` when a session must be limited to a known set of host-defined skills.
- Treat script execution as a separate product capability with explicit user, trust, and policy decisions.
- Keep product-specific state such as enabled status, source URI, revision, trust level, and marketplace metadata in the application layer.
- Inspect `RawFrontmatter` when compatibility fields from other tools are needed.

Close sessions when a conversation ends. The runtime also supports session TTL, maximum-session, and maximum-active-skill configuration.

## Filesystem skill provider

The bundled provider is available from [`provider/fs`](./provider/fs). Use `fs.Type` when constructing a filesystem `provider.SkillDef`.

The filesystem provider indexes a skill directory, validates its `SKILL.md`, discovers additional resources, and delegates resource reads and script execution sandboxing to `llmtools-go`.

### Quickstart

- A quick walkthrough is at: [`TestReadmeQuickstart`](./runtime/internal/integration/quickstart_test.go).

It demonstrates the complete public API flow:

- Create an `fs.Provider` and a `runtime.Runtime`.
- Add instruction skills and a `user-message` template.
- List instruction skills through the host lifecycle API.
- Build an available-skills prompt.
- Create an allowlisted session with an initially active skill.
- Build an active-skills prompt and a combined session prompt.
- Render a `user-message` template with caller-supplied arguments.
- Create a session-specific `llmtools-go` registry.
- Close the session.

Run that example directly with `go test ./runtime/internal/integration -run TestReadmeQuickstart`.

The separate [`TestReadmeDocumentWorkflow`](./runtime/internal/integration/document_test.go) example covers provider-independent document parsing, rendering, and marshaling.

### Security notes

The filesystem provider is intentionally thin and delegates generic filesystem and execution hardening to `llmtools-go`.

- A skill root must resolve to a directory.
- The provider keeps canonical filesystem locations internal. Host records and LLM handles retain the user-provided `SkillDef.Location`.
- `SKILL.md` must be a regular non-symlink file.
- The `SKILL.md` frontmatter name must match both the skill directory basename and `SkillDef.Name`.
- Regular non-symlink files below the skill root, excluding `SKILL.md`, are indexed as resources.
- `provider.SkillResourceInfo.Locations` is capped at `provider.MaxSkillResourceLocations`, while `TotalCount` reports the complete count.
- `skills-readresource` is scoped to the skill root through `llmtools-go/fstool` using the skill root as both an allowed root and work base directory.
- The LLM-facing resource tool requires the selected skill to be active in the current session.
- Script execution is disabled by default.
- Enabling filesystem script execution with `fs.WithRunScripts(true)` is an explicit host decision.
- `skills-runscript` is registered only when the runtime has a provider that reports script support.
- Script execution is scoped through `llmtools-go/exectool`, including its execution and run-script policies.
- By default, the filesystem provider permits `.sh` and `.py` scripts on non-Windows systems, and `.ps1` and `.py` scripts on Windows.
- Scripts may currently live anywhere under the skill root. Applications that need stricter layout rules should enforce them through provider policy or a custom provider.

## End-to-end examples

The examples are executable tests so documentation and API behavior evolve together.

- [`TestReadmeDocumentWorkflow`](./runtime/internal/integration/document_test.go) covers parse, render, marshal, and unknown-frontmatter preservation.
- [`TestReadmeQuickstart`](./runtime/internal/integration/quickstart_test.go) covers the current public filesystem runtime flow.
- [`TestReadmeRunScriptConfiguration`](./runtime/internal/integration/quickstart_test.go) verifies that filesystem script capability propagates to the runtime.
- [`TestBasicRuntimeFlow`](./runtime/internal/integration/basic_test.go) covers a compact active-skill lifecycle.
- [`TestRuntime_FSProvider_EndToEnd`](./runtime/internal/integration/fs_test.go) covers filesystem indexing, prompting, session activation, and registry creation.
- [`TestRuntime_FSProvider_TolerantDocumentAndTemplateWorkflow`](./runtime/internal/integration/fs_test.go) covers tolerant optional metadata, resources, warnings, templates, and source-name validation.
- [`runtime/internal/session/tools_impl_test.go`](./runtime/internal/session/tools_impl_test.go) covers load, unload, resource-read, and script-run tool behavior.

## Development

- Formatting and linting are configured through `golangci-lint`; see [`.golangci.yml`](.golangci.yml).
- Repository tasks are defined in [`taskfile.yml`](taskfile.yml) and require [Task](https://taskfile.dev/).
- Keep host lifecycle types, LLM-facing handles, and provider-canonical keys separate.
- Do not expose provider-specific canonicalization through host or LLM-facing application APIs.
- Add or update an integration test whenever README-visible behavior changes.
- Run tests and linters before opening a pull request.

## License

Copyright (c) 2026-present, Pankaj Pipada.

All source code in this repository, unless otherwise noted, is licensed under the MIT License. See [LICENSE](./LICENSE) for details.
