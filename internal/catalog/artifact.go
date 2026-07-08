package catalog

import (
	"regexp"
	"sort"
	"strings"

	"github.com/flexigpt/agentskills-go/spec"
)

var doubleBracePlaceholderRE = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

func NormalizeSkillInsert(v spec.SkillInsert) (spec.SkillInsert, bool) {
	switch spec.SkillInsert(strings.ToLower(strings.TrimSpace(string(v)))) {
	case "":
		return spec.SkillInsertInstructions, true
	case spec.SkillInsertInstructions:
		return spec.SkillInsertInstructions, true
	case spec.SkillInsertUserMessage:
		return spec.SkillInsertUserMessage, true
	default:
		return spec.SkillInsertInstructions, false
	}
}

// RenderSkillBody renders declared string arguments into body.
//
// Supported placeholders:
//   - $name
//   - {{name}}
//   - {{ name }}
//
// Only declared arguments are substituted. Unknown placeholders are preserved and warned.
// No runtime variables are expanded. No command syntax is interpreted or sanitized.
func RenderSkillBody(body string, arguments []spec.SkillArgument, values map[string]string) spec.RenderSkillBodyResult {
	out := spec.RenderSkillBodyResult{
		AppliedArguments: map[string]string{},
	}

	declared := map[string]string{}
	seenArgs := map[string]struct{}{}
	for _, a := range arguments {
		name := strings.TrimSpace(a.Name)
		if !IsValidSkillArgumentName(name) {
			if name != "" {
				out.Warnings = append(out.Warnings, "invalid argument name ignored: "+name)
			}
			continue
		}
		if _, exists := seenArgs[name]; exists {
			out.Warnings = append(out.Warnings, "duplicate argument ignored: "+name)
			continue
		}
		seenArgs[name] = struct{}{}

		value := a.Default
		if values != nil {
			if supplied, ok := values[name]; ok {
				value = supplied
			}
		}
		declared[name] = value
		out.AppliedArguments[name] = value
	}

	used := map[string]struct{}{}
	unknown := map[string]struct{}{}

	rendered := renderDollarPlaceholders(body, declared, used, unknown)
	rendered = renderDoubleBracePlaceholders(rendered, declared, used, unknown)

	for name := range unknown {
		out.UnknownPlaceholders = append(out.UnknownPlaceholders, name)
		out.Warnings = append(out.Warnings, "unknown placeholder left unchanged: "+name)
	}

	sort.Strings(out.UnknownPlaceholders)
	out.Warnings = uniqueSortedStrings(out.Warnings)
	out.Text = rendered
	return out
}

func renderDollarPlaceholders(
	s string,
	declared map[string]string,
	used map[string]struct{},
	unknown map[string]struct{},
) string {
	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); {
		// Escape: \$name renders as literal $name.
		if s[i] == '\\' && i+1 < len(s) && s[i+1] == '$' {
			b.WriteByte('$')
			i += 2
			continue
		}

		if s[i] != '$' {
			b.WriteByte(s[i])
			i++
			continue
		}

		name, n := scanIdentifier(s[i+1:])
		if n == 0 {
			b.WriteByte(s[i])
			i++
			continue
		}

		if value, ok := declared[name]; ok {
			b.WriteString(value)
			used[name] = struct{}{}
		} else {
			b.WriteByte('$')
			b.WriteString(name)
			unknown[name] = struct{}{}
		}
		i += 1 + n
	}

	return b.String()
}

func renderDoubleBracePlaceholders(
	s string,
	declared map[string]string,
	used map[string]struct{},
	unknown map[string]struct{},
) string {
	return doubleBracePlaceholderRE.ReplaceAllStringFunc(s, func(match string) string {
		parts := doubleBracePlaceholderRE.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		name := parts[1]
		if value, ok := declared[name]; ok {
			used[name] = struct{}{}
			return value
		}
		unknown[name] = struct{}{}
		return match
	})
}

func scanIdentifier(s string) (name string, n int) {
	if s == "" {
		return "", 0
	}
	for i, r := range s {
		valid := r == '_' ||
			(r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(i > 0 && r >= '0' && r <= '9')
		if !valid {
			if i == 0 {
				return "", 0
			}
			return s[:i], i
		}
	}
	if !IsValidSkillArgumentName(s) {
		return "", 0
	}
	return s, len(s)
}

func IsValidSkillArgumentName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r == '_':
			continue
		case r >= 'a' && r <= 'z':
			continue
		case r >= 'A' && r <= 'Z':
			continue
		case i > 0 && r >= '0' && r <= '9':
			continue
		default:
			return false
		}
	}
	return true
}

func uniqueSortedStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	set := map[string]struct{}{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		set[s] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
