package document

import (
	"regexp"
	"sort"
	"strings"
)

var doubleBracePlaceholderRE = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

func SkillInsertMatches(allowed []SkillInsert, actual SkillInsert) bool {
	normActual, ok := NormalizeSkillInsert(actual)
	if !ok {
		return false
	}
	for _, candidate := range allowed {
		normCandidate, ok := NormalizeSkillInsert(candidate)
		if ok && normCandidate == normActual {
			return true
		}
	}
	return false
}

func NormalizeSkillInsert(v SkillInsert) (SkillInsert, bool) {
	switch SkillInsert(strings.ToLower(strings.TrimSpace(string(v)))) {
	case "":
		return SkillInsertInstructions, true
	case SkillInsertInstructions:
		return SkillInsertInstructions, true
	case SkillInsertUserMessage:
		return SkillInsertUserMessage, true
	default:
		return SkillInsertInstructions, false
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
func RenderSkillBody(body string, arguments []SkillArgument, values map[string]string) RenderSkillBodyResult {
	out := RenderSkillBodyResult{
		AppliedArguments: map[string]string{},
	}

	declared := map[string]string{}
	seenArgs := map[string]struct{}{}
	for _, a := range arguments {
		name := strings.TrimSpace(a.Name)
		if !isValidSkillArgumentName(name) {
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

	unknown := map[string]struct{}{}

	rendered := renderDollarPlaceholders(body, declared, unknown)
	rendered = renderDoubleBracePlaceholders(rendered, declared, unknown)

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

	unknown map[string]struct{},
) string {
	return doubleBracePlaceholderRE.ReplaceAllStringFunc(s, func(match string) string {
		parts := doubleBracePlaceholderRE.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		name := parts[1]
		if value, ok := declared[name]; ok {
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
	if !isValidSkillArgumentName(s) {
		return "", 0
	}
	return s, len(s)
}

func isValidSkillArgumentName(name string) bool {
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
