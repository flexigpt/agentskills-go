package agentskills

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/goccy/go-yaml"

	"github.com/flexigpt/agentskills-go/internal/catalog"
	"github.com/flexigpt/agentskills-go/spec"
)

const (
	SkillDocumentFileName = "SKILL.md"
	MaxSkillDocumentBytes = 2 << 20

	maxSkillNameBytes        = 64
	maxSkillDescriptionBytes = 1024
	maxSkillDisplayNameBytes = 256
	maxSkillArguments        = 64
	maxSkillArgumentBytes    = 4096
	maxSkillTags             = 64
	maxSkillTagBytes         = 128

	propKeyName = "name"
)

var skillDocumentNamePattern = regexp.MustCompile(
	`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`,
)

// ParseSkillDocument parses a materialized SKILL.md document.
//
// Parsing is intentionally tolerant for optional fields:
// malformed optional values are ignored or normalized and returned as warnings.
// Name, description, readable YAML frontmatter, UTF-8, and document size remain
// required because the runtime needs them for safe discovery and processing.
func ParseSkillDocument(
	content []byte,
	options spec.ParseSkillDocumentOptions,
) (spec.SkillDocument, []string, error) {
	if len(content) > MaxSkillDocumentBytes {
		return spec.SkillDocument{}, nil, fmt.Errorf(
			"SKILL.md exceeds %d bytes",
			MaxSkillDocumentBytes,
		)
	}
	if !utf8.Valid(content) {
		return spec.SkillDocument{}, nil, errors.New(
			"SKILL.md must contain valid UTF-8",
		)
	}
	if bytes.IndexByte(content, 0) >= 0 {
		return spec.SkillDocument{}, nil, errors.New(
			"SKILL.md contains a NUL byte",
		)
	}

	var warnings []string
	if after, ok := bytes.CutPrefix(content, []byte{0xef, 0xbb, 0xbf}); ok {
		content = after
		warnings = append(warnings, "UTF-8 BOM was removed")
	}

	frontmatter, body, err := splitSkillDocumentFrontmatter(content)
	if err != nil {
		return spec.SkillDocument{}, nil, err
	}

	properties := map[string]any{}
	if err := yaml.Unmarshal(frontmatter, &properties); err != nil {
		return spec.SkillDocument{}, nil, fmt.Errorf(
			"decode SKILL.md frontmatter: %w",
			err,
		)
	}

	name, nameWarnings, err := requiredSkillDocumentString(
		properties,
		propKeyName,
	)
	if err != nil {
		return spec.SkillDocument{}, nil, err
	}
	warnings = append(warnings, nameWarnings...)
	if err := validateSkillDocumentName(name); err != nil {
		return spec.SkillDocument{}, nil, err
	}

	description, descriptionWarnings, err := requiredSkillDocumentString(
		properties,
		"description",
	)
	if err != nil {
		return spec.SkillDocument{}, nil, err
	}
	warnings = append(warnings, descriptionWarnings...)
	if err := validateSkillDocumentDescription(description); err != nil {
		return spec.SkillDocument{}, nil, err
	}

	if expected := strings.TrimSpace(options.ExpectedName); expected != "" &&
		name != expected {
		return spec.SkillDocument{}, nil, fmt.Errorf("frontmatter.name %q must match expected name %q", name, expected)
	}

	insert, insertWarnings := parseSkillDocumentInsert(
		properties["insert"],
	)
	warnings = append(warnings, insertWarnings...)

	arguments, argumentWarnings := parseSkillDocumentArguments(
		properties["arguments"],
	)
	warnings = append(warnings, argumentWarnings...)

	tags, tagWarnings := parseSkillDocumentTags(properties["tags"])
	warnings = append(warnings, tagWarnings...)

	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	body = strings.TrimLeft(body, "\n")
	if strings.TrimSpace(body) == "" {
		warnings = append(
			warnings,
			"SKILL.md body is empty; the skill may still provide resources or scripts",
		)
	}

	displayName := firstSkillDocumentHeading(body)
	if displayName == "" {
		displayName = name
	}
	if len(displayName) > maxSkillDisplayNameBytes {
		displayName = truncateValidUTF8(
			displayName,
			maxSkillDisplayNameBytes,
		)
		warnings = append(
			warnings,
			fmt.Sprintf(
				"display name was truncated to %d bytes",
				maxSkillDisplayNameBytes,
			),
		)
	}

	return spec.SkillDocument{
		Name:           name,
		DisplayName:    displayName,
		Description:    description,
		Insert:         insert,
		Arguments:      arguments,
		Tags:           tags,
		MarkdownBody:   body,
		RawFrontmatter: cloneSkillDocumentMap(properties),
	}, uniqueSkillDocumentWarnings(warnings), nil
}

// RenderSkillDocument renders an already materialized Skill document.
//
// It uses the same argument substitution semantics as Runtime.RenderSkill but
// does not register a provider skill, activate a session, read resources, or
// execute scripts.
func RenderSkillDocument(
	document spec.SkillDocument,
	arguments map[string]string,
) (spec.RenderSkillOut, error) {
	if err := validateSkillDocument(document); err != nil {
		return spec.RenderSkillOut{}, fmt.Errorf(
			"%w: invalid Skill document: %w",
			spec.ErrInvalidArgument,
			err,
		)
	}

	insert, ok := catalog.NormalizeSkillInsert(document.Insert)
	if !ok {
		return spec.RenderSkillOut{}, fmt.Errorf(
			"%w: invalid insert behavior %q",
			spec.ErrInvalidArgument,
			document.Insert,
		)
	}

	rendered := catalog.RenderSkillBody(
		document.MarkdownBody,
		document.Arguments,
		arguments,
	)

	displayName := document.DisplayName
	if displayName == "" {
		displayName = document.Name
	}

	return spec.RenderSkillOut{
		Name:             document.Name,
		Description:      document.Description,
		DisplayName:      displayName,
		Insert:           insert,
		Tags:             append([]string(nil), document.Tags...),
		Text:             rendered.Text,
		Arguments:        append([]spec.SkillArgument(nil), document.Arguments...),
		AppliedArguments: cloneSkillDocumentStringMap(rendered.AppliedArguments),
		RawFrontmatter:   cloneSkillDocumentMap(document.RawFrontmatter),
		Warnings:         append([]string(nil), rendered.Warnings...),
	}, nil
}

// MarshalSkillDocument produces a canonical SKILL.md representation.
//
// Known semantic fields replace corresponding RawFrontmatter fields. Unknown
// frontmatter fields are retained. If DisplayName differs from Name and the
// body has no H1, a display heading is added.
func MarshalSkillDocument(
	document spec.SkillDocument,
) ([]byte, error) {
	if err := validateSkillDocument(document); err != nil {
		return nil, fmt.Errorf(
			"%w: invalid Skill document: %w",
			spec.ErrInvalidArgument,
			err,
		)
	}

	insert, _ := catalog.NormalizeSkillInsert(document.Insert)
	properties := cloneSkillDocumentMap(document.RawFrontmatter)
	if properties == nil {
		properties = map[string]any{}
	}

	properties[propKeyName] = document.Name
	properties["description"] = document.Description
	properties["insert"] = string(insert)

	if len(document.Arguments) == 0 {
		delete(properties, "arguments")
	} else {
		values := make([]map[string]any, 0, len(document.Arguments))
		for _, argument := range document.Arguments {
			value := map[string]any{propKeyName: argument.Name}
			if argument.Description != "" {
				value["description"] = argument.Description
			}
			if argument.Default != "" {
				value["default"] = argument.Default
			}
			values = append(values, value)
		}
		properties["arguments"] = values
	}

	if len(document.Tags) == 0 {
		delete(properties, "tags")
	} else {
		properties["tags"] = append([]string(nil), document.Tags...)
	}

	frontmatter, err := yaml.Marshal(properties)
	if err != nil {
		return nil, fmt.Errorf("encode SKILL.md frontmatter: %w", err)
	}

	body := strings.ReplaceAll(document.MarkdownBody, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	body = strings.TrimLeft(body, "\n")

	displayName := strings.TrimSpace(document.DisplayName)
	if firstSkillDocumentHeading(body) == "" &&
		displayName != "" &&
		displayName != document.Name {
		if body == "" {
			body = "# " + displayName
		} else {
			body = "# " + displayName + "\n\n" + body
		}
	}

	var output strings.Builder
	output.WriteString("---\n")
	output.WriteString(strings.TrimSpace(string(frontmatter)))
	output.WriteString("\n---\n")
	if body != "" {
		output.WriteString("\n")
		output.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			output.WriteByte('\n')
		}
	}

	raw := []byte(output.String())
	if len(raw) > MaxSkillDocumentBytes {
		return nil, fmt.Errorf(
			"encoded SKILL.md exceeds %d bytes",
			MaxSkillDocumentBytes,
		)
	}
	return raw, nil
}

func splitSkillDocumentFrontmatter(
	content []byte,
) (frontmatterBytes []byte, bodyStr string, err error) {
	reader := bufio.NewReader(bytes.NewReader(content))

	first, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, "", err
	}
	if strings.TrimSpace(strings.TrimRight(first, "\r\n")) != "---" {
		return nil, "", errors.New(
			"SKILL.md requires YAML frontmatter",
		)
	}

	var frontmatter strings.Builder
	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, "", readErr
		}

		trimmed := strings.TrimSpace(strings.TrimRight(line, "\r\n"))
		if trimmed == "---" || trimmed == "..." {
			break
		}

		frontmatter.WriteString(line)
		if errors.Is(readErr, io.EOF) {
			return nil, "", errors.New(
				"SKILL.md has unterminated YAML frontmatter",
			)
		}
	}

	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, "", err
	}
	return []byte(frontmatter.String()), string(body), nil
}

func requiredSkillDocumentString(
	properties map[string]any,
	key string,
) (name string, nameWarnings []string, err error) {
	raw, exists := properties[key]
	if !exists {
		return "", nil, fmt.Errorf(
			"frontmatter.%s is required",
			key,
		)
	}

	value, ok := raw.(string)
	if !ok {
		return "", nil, fmt.Errorf(
			"frontmatter.%s must be a string",
			key,
		)
	}

	trimmed := strings.TrimSpace(value)
	if trimmed == value {
		return value, nil, nil
	}
	return trimmed, []string{
		fmt.Sprintf(
			"frontmatter.%s had leading or trailing whitespace removed",
			key,
		),
	}, nil
}

func parseSkillDocumentInsert(
	raw any,
) (insert spec.SkillInsert, insertWarnings []string) {
	if raw == nil {
		return spec.SkillInsertInstructions, nil
	}

	value, ok := raw.(string)
	if !ok {
		return spec.SkillInsertInstructions, []string{
			"frontmatter.insert must be a string; defaulted to instructions",
		}
	}

	insert, supported := catalog.NormalizeSkillInsert(
		spec.SkillInsert(value),
	)
	if supported {
		return insert, nil
	}
	return spec.SkillInsertInstructions, []string{
		fmt.Sprintf(
			"unsupported frontmatter.insert value %q; defaulted to instructions",
			value,
		),
	}
}

func parseSkillDocumentArguments(
	raw any,
) (args []spec.SkillArgument, warnings []string) {
	if raw == nil {
		return nil, nil
	}

	var items []any

	switch value := raw.(type) {
	case []any:
		items = value
	case []string:
		items = make([]any, 0, len(value))
		for _, item := range value {
			items = append(items, item)
		}
	case string:
		items = []any{value}
		warnings = append(
			warnings,
			"frontmatter.arguments string was treated as a one-item list",
		)
	default:
		return nil, []string{
			"frontmatter.arguments must be a list of objects or strings",
		}
	}

	output := make([]spec.SkillArgument, 0, min(len(items), maxSkillArguments))
	seen := make(map[string]struct{}, len(items))

	for index, item := range items {
		if len(output) >= maxSkillArguments {
			warnings = append(
				warnings,
				fmt.Sprintf(
					"frontmatter.arguments was truncated to %d entries",
					maxSkillArguments,
				),
			)
			break
		}

		var argument spec.SkillArgument
		switch value := item.(type) {
		case string:
			argument.Name = strings.TrimSpace(value)

		default:
			properties, ok := skillDocumentStringMap(value)
			if !ok {
				warnings = append(
					warnings,
					fmt.Sprintf(
						"frontmatter.arguments[%d] was ignored because it is not an object or string",
						index,
					),
				)
				continue
			}

			name, ok := properties[propKeyName].(string)
			if !ok {
				warnings = append(
					warnings,
					fmt.Sprintf(
						"frontmatter.arguments[%d].name is invalid",
						index,
					),
				)
				continue
			}
			argument.Name = strings.TrimSpace(name)
			argument.Description, warnings = optionalSkillArgumentText(
				properties,
				"description",
				index,
				warnings,
			)
			argument.Default, warnings = optionalSkillArgumentText(
				properties,
				"default",
				index,
				warnings,
			)
		}

		if !catalog.IsValidSkillArgumentName(argument.Name) {
			warnings = append(
				warnings,
				fmt.Sprintf(
					"frontmatter.arguments[%d].name is invalid",
					index,
				),
			)
			continue
		}
		if _, duplicate := seen[argument.Name]; duplicate {
			warnings = append(
				warnings,
				"duplicate argument ignored: "+argument.Name,
			)
			continue
		}

		seen[argument.Name] = struct{}{}
		output = append(output, argument)
	}

	return output, warnings
}

func optionalSkillArgumentText(
	properties map[string]any,
	key string,
	index int,
	warnings []string,
) (argText string, warn []string) {
	raw, exists := properties[key]
	if !exists || raw == nil {
		return "", warnings
	}

	value, ok := raw.(string)
	if !ok {
		warnings = append(
			warnings,
			fmt.Sprintf(
				"frontmatter.arguments[%d].%s was ignored because it is not a string",
				index,
				key,
			),
		)
		return "", warnings
	}
	if len(value) > maxSkillArgumentBytes {
		value = truncateValidUTF8(value, maxSkillArgumentBytes)
		warnings = append(
			warnings,
			fmt.Sprintf(
				"frontmatter.arguments[%d].%s was truncated to %d bytes",
				index,
				key,
				maxSkillArgumentBytes,
			),
		)
	}
	return value, warnings
}

func parseSkillDocumentTags(raw any) (tags, tagWarnings []string) {
	if raw == nil {
		return nil, nil
	}

	var (
		items    []any
		warnings []string
	)
	switch value := raw.(type) {
	case []any:
		items = value
	case []string:
		items = make([]any, 0, len(value))
		for _, item := range value {
			items = append(items, item)
		}
	case string:
		items = []any{value}
		warnings = append(
			warnings,
			"frontmatter.tags string was treated as a one-item list",
		)
	default:
		return nil, []string{
			"frontmatter.tags was ignored because it is not a list or string",
		}
	}

	output := make([]string, 0, min(len(items), maxSkillTags))
	seen := make(map[string]struct{}, len(items))
	for index, item := range items {
		if len(output) >= maxSkillTags {
			warnings = append(
				warnings,
				fmt.Sprintf(
					"frontmatter.tags was truncated to %d entries",
					maxSkillTags,
				),
			)
			break
		}

		value, ok := item.(string)
		if !ok {
			warnings = append(
				warnings,
				fmt.Sprintf(
					"frontmatter.tags[%d] was ignored because it is not a string",
					index,
				),
			)
			continue
		}

		value = strings.TrimSpace(value)
		if value == "" {
			warnings = append(
				warnings,
				fmt.Sprintf(
					"frontmatter.tags[%d] was ignored because it is empty",
					index,
				),
			)
			continue
		}
		if len(value) > maxSkillTagBytes {
			warnings = append(
				warnings,
				fmt.Sprintf(
					"frontmatter.tags[%d] was ignored because it exceeds %d bytes",
					index,
					maxSkillTagBytes,
				),
			)
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}

		seen[value] = struct{}{}
		output = append(output, value)
	}

	return output, warnings
}

func validateSkillDocument(document spec.SkillDocument) error {
	if err := validateSkillDocumentName(document.Name); err != nil {
		return err
	}
	if err := validateSkillDocumentDescription(document.Description); err != nil {
		return err
	}
	if document.DisplayName != "" {
		if strings.TrimSpace(document.DisplayName) != document.DisplayName {
			return errors.New("displayName has leading or trailing whitespace")
		}
		if len(document.DisplayName) > maxSkillDisplayNameBytes {
			return fmt.Errorf(
				"displayName exceeds %d bytes",
				maxSkillDisplayNameBytes,
			)
		}
	}
	if _, ok := catalog.NormalizeSkillInsert(document.Insert); !ok {
		return fmt.Errorf("unsupported insert value %q", document.Insert)
	}
	if len(document.Arguments) > maxSkillArguments {
		return fmt.Errorf(
			"arguments exceeds %d entries",
			maxSkillArguments,
		)
	}

	seenArguments := make(map[string]struct{}, len(document.Arguments))
	for index, argument := range document.Arguments {
		if argument.Name != strings.TrimSpace(argument.Name) ||
			!catalog.IsValidSkillArgumentName(argument.Name) {
			return fmt.Errorf("arguments[%d].name is invalid", index)
		}
		if _, duplicate := seenArguments[argument.Name]; duplicate {
			return fmt.Errorf("duplicate argument %q", argument.Name)
		}
		seenArguments[argument.Name] = struct{}{}

		if len(argument.Description) > maxSkillArgumentBytes ||
			len(argument.Default) > maxSkillArgumentBytes {
			return fmt.Errorf(
				"arguments[%d] description or default exceeds %d bytes",
				index,
				maxSkillArgumentBytes,
			)
		}
	}

	if len(document.Tags) > maxSkillTags {
		return fmt.Errorf("tags exceeds %d entries", maxSkillTags)
	}
	seenTags := make(map[string]struct{}, len(document.Tags))
	for index, tag := range document.Tags {
		if tag == "" || tag != strings.TrimSpace(tag) {
			return fmt.Errorf("tags[%d] must be non-empty and trimmed", index)
		}
		if len(tag) > maxSkillTagBytes {
			return fmt.Errorf("tags[%d] exceeds %d bytes", index, maxSkillTagBytes)
		}
		if _, duplicate := seenTags[tag]; duplicate {
			return fmt.Errorf("duplicate tag %q", tag)
		}
		seenTags[tag] = struct{}{}
	}

	if !utf8.ValidString(document.MarkdownBody) {
		return errors.New("markdownBody must contain valid UTF-8")
	}
	if strings.ContainsRune(document.MarkdownBody, 0) {
		return errors.New("markdownBody contains a NUL byte")
	}
	return nil
}

func validateSkillDocumentName(value string) error {
	if value == "" {
		return errors.New("frontmatter.name is required")
	}
	if value != strings.TrimSpace(value) {
		return errors.New(
			"frontmatter.name has leading or trailing whitespace",
		)
	}
	if len(value) > maxSkillNameBytes ||
		!skillDocumentNamePattern.MatchString(value) ||
		strings.Contains(value, "--") {
		return errors.New(
			"frontmatter.name must be a lowercase hyphenated name of at most 64 bytes without consecutive hyphens",
		)
	}
	return nil
}

func validateSkillDocumentDescription(value string) error {
	if value == "" {
		return errors.New("frontmatter.description is required")
	}
	if value != strings.TrimSpace(value) {
		return errors.New(
			"frontmatter.description has leading or trailing whitespace",
		)
	}
	if len(value) > maxSkillDescriptionBytes {
		return fmt.Errorf(
			"frontmatter.description exceeds %d bytes",
			maxSkillDescriptionBytes,
		)
	}
	return nil
}

func firstSkillDocumentHeading(body string) string {
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(line)
		if value, found := strings.CutPrefix(line, "# "); found {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func skillDocumentStringMap(value any) (map[string]any, bool) {
	if properties, ok := value.(map[string]any); ok {
		return properties, true
	}
	if properties, ok := value.(map[any]any); ok {
		output := make(map[string]any, len(properties))
		for key, item := range properties {
			stringKey, ok := key.(string)
			if !ok {
				return nil, false
			}
			output[stringKey] = item
		}
		return output, true
	}
	return nil, false
}

func truncateValidUTF8(value string, maximum int) string {
	if maximum <= 0 || len(value) <= maximum {
		return value
	}
	value = value[:maximum]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func uniqueSkillDocumentWarnings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	output := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		output = append(output, value)
	}
	return output
}

func cloneSkillDocumentStringMap(
	input map[string]string,
) map[string]string {
	if input == nil {
		return nil
	}
	output := make(map[string]string, len(input))
	maps.Copy(output, input)
	return output
}

func cloneSkillDocumentMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = cloneSkillDocumentValue(value)
	}
	return output
}

func cloneSkillDocumentValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneSkillDocumentMap(typed)
	case map[any]any:
		output := make(map[any]any, len(typed))
		for key, item := range typed {
			output[key] = cloneSkillDocumentValue(item)
		}
		return output
	case []any:
		output := make([]any, len(typed))
		for index, item := range typed {
			output[index] = cloneSkillDocumentValue(item)
		}
		return output
	case []string:
		return append([]string(nil), typed...)
	case []spec.SkillArgument:
		return append([]spec.SkillArgument(nil), typed...)
	default:
		return value
	}
}
