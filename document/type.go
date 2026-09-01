package document

import "errors"

var errInvalidDocumentArgument = errors.New("invalid document argument")

// SkillInsert describes where a rendered SKILL.md body should be inserted by the consumer.
//
// The default is SkillInsertInstructions. This keeps normal Agent Skills behavior:
// a skill body is instruction/context material unless it explicitly opts into user insertion.
type SkillInsert string

const (
	// SkillInsertInstructions means the rendered body is instruction/context material.
	SkillInsertInstructions SkillInsert = "instructions"
	// SkillInsertUserMessage means the rendered body should be placed in the user-message body.
	SkillInsertUserMessage SkillInsert = "user-message"
)

// ParseSkillDocumentOptions controls provider-independent SKILL.md parsing.
type ParseSkillDocumentOptions struct {
	// ExpectedName is an optional source-derived name, such as the containing
	// filesystem directory name. A mismatch triggers a error.
	ExpectedName string `json:"expectedName,omitempty"`
}

// SkillArgument is a named string argument supported by the FlexiGPT skill extension.
//
// Values are intentionally string-only. Consumers may build richer UI validation on top,
// but the runtime only renders strings into the skill body.
type SkillArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Default     string `json:"default,omitempty"`
}

// SkillDocument is a materialized, provider-independent SKILL.md document.
//
// RawFrontmatter preserves fields that the runtime does not interpret.
type SkillDocument struct {
	Name         string          `json:"name"`
	DisplayName  string          `json:"displayName,omitempty"`
	Description  string          `json:"description"`
	Insert       SkillInsert     `json:"insert"`
	Arguments    []SkillArgument `json:"arguments,omitempty"`
	Tags         []string        `json:"tags,omitempty"`
	MarkdownBody string          `json:"markdownBody"`

	RawFrontmatter map[string]any `json:"rawFrontmatter,omitempty"`
}

// RenderSkillBodyResult is the low-level result of rendering declared arguments into a skill body.
type RenderSkillBodyResult struct {
	Text string `json:"text"`

	AppliedArguments    map[string]string `json:"appliedArguments,omitempty"`
	UnknownPlaceholders []string          `json:"unknownPlaceholders,omitempty"`
	Warnings            []string          `json:"warnings,omitempty"`
}

type RenderSkillDocumentOut struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	DisplayName string      `json:"displayName,omitempty"`
	Insert      SkillInsert `json:"insert"`

	Tags []string `json:"tags,omitempty"`

	Text string `json:"text"`

	Arguments        []SkillArgument   `json:"arguments,omitempty"`
	AppliedArguments map[string]string `json:"appliedArguments,omitempty"`

	RawFrontmatter map[string]any `json:"rawFrontmatter,omitempty"`
	Warnings       []string       `json:"warnings,omitempty"`
}
