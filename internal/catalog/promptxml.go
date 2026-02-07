package catalog

import (
	"encoding/xml"
	"fmt"
	"sort"
)

type AvailableSkillItem struct {
	Name        string `xml:"name"`
	Description string `xml:"description"`
	Path        string `xml:"path"`
}

type availableSkills struct {
	XMLName xml.Name             `xml:"available_skills"`
	Skills  []AvailableSkillItem `xml:"skill"`
}

type ActiveSkillItem struct {
	Name string `xml:"name,attr"`
	Body string `xml:",cdata"`
}

type activeSkills struct {
	XMLName xml.Name          `xml:"active_skills"`
	Skills  []ActiveSkillItem `xml:"skill"`
}

func AvailableSkillsXML(items []AvailableSkillItem) (string, error) {
	sorted := append([]AvailableSkillItem(nil), items...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Name == sorted[j].Name {
			return sorted[i].Path < sorted[j].Path
		}
		return sorted[i].Name < sorted[j].Name
	})

	out := availableSkills{Skills: make([]AvailableSkillItem, 0, len(sorted))}
	for _, it := range sorted {
		out.Skills = append(out.Skills, AvailableSkillItem{
			Name:        it.Name,
			Description: it.Description,
			Path:        it.Path,
		})
	}

	b, err := xml.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", fmt.Errorf("xml encode: %w", err)
	}
	return string(b), nil
}

func ActiveSkillsXML(items []ActiveSkillItem) (string, error) {
	out := activeSkills{Skills: make([]ActiveSkillItem, 0, len(items))}
	for _, it := range items {
		out.Skills = append(out.Skills, ActiveSkillItem{
			Name: it.Name,
			Body: it.Body,
		})
	}

	b, err := xml.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", fmt.Errorf("xml encode: %w", err)
	}
	return string(b), nil
}
