package catalog

import (
	"sort"
	"strings"
)

const (
	availableSkillsStart         = "<<<AVAILABLE_SKILLS>>>"
	availableSkillsEnd           = "<<<END_AVAILABLE_SKILLS>>>"
	activeSkillsStart            = "<<<ACTIVE_SKILLS>>>"
	activeSkillsEnd              = "<<<END_ACTIVE_SKILLS>>>"
	nextAvailableSkillsSeparator = "---"
	nextActiveSkillsSeparator    = "<!-- SKILL SEPARATOR -->"
)

type AvailableSkillItem struct {
	Name        string
	Description string
	Location    string
}

type ActiveSkillItem struct {
	Name string
	Body string
}

func AvailableSkillsPrompt(items []AvailableSkillItem) string {
	sorted := append([]AvailableSkillItem(nil), items...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Name == sorted[j].Name {
			return sorted[i].Location < sorted[j].Location
		}
		return sorted[i].Name < sorted[j].Name
	})

	var sb strings.Builder
	sb.WriteString(availableSkillsStart)
	sb.WriteByte('\n')

	if len(sorted) == 0 {
		sb.WriteString("(none)\n")
		sb.WriteString(availableSkillsEnd)
		return sb.String()
	}

	for idx, it := range sorted {
		if idx != 0 {
			sb.WriteString(nextAvailableSkillsSeparator + "\n")
		}

		sb.WriteString("name: ")
		sb.WriteString(trimInline(it.Name))
		sb.WriteByte('\n')

		if it.Location != "" {
			sb.WriteString("location: ")
			sb.WriteString(trimInline(it.Location))
			sb.WriteByte('\n')
		}

		if it.Description != "" {
			sb.WriteString("description: ")
			sb.WriteString(trimInline(it.Description))
			sb.WriteByte('\n')
		}
	}

	sb.WriteString(availableSkillsEnd)
	return sb.String()
}

func ActiveSkillsPrompt(items []ActiveSkillItem) string {
	var sb strings.Builder
	sb.WriteString(activeSkillsStart)
	sb.WriteByte('\n')

	if len(items) == 0 {
		sb.WriteString("(none)\n")
		sb.WriteString(activeSkillsEnd)
		return sb.String()
	}

	for idx, it := range items {
		if idx != 0 {
			sb.WriteString(nextActiveSkillsSeparator + "\n")
		}
		sb.WriteString("name: ")
		sb.WriteString(trimInline(it.Name))
		sb.WriteByte('\n')
		sb.WriteString("body:\n")

		body := trimTrailingNewlines(it.Body)
		if body != "" {
			sb.WriteString(body)
			sb.WriteByte('\n')
		}
	}

	sb.WriteString(activeSkillsEnd)
	return sb.String()
}

func trimInline(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}

func trimTrailingNewlines(s string) string {
	return strings.TrimRight(s, "\r\n")
}
