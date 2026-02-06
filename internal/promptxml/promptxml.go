package promptxml

import (
	"encoding/xml"
	"fmt"
	"sort"
	"strings"

	"github.com/flexigpt/agentskills-go/spec"
)

type availableSkills struct {
	XMLName xml.Name         `xml:"available_skills"`
	Skills  []availableSkill `xml:"skill"`
}

type availableSkill struct {
	Name        string `xml:"name"`
	Description string `xml:"description"`
	Location    string `xml:"location,omitempty"`
}

type activeSkills struct {
	XMLName xml.Name      `xml:"active_skills"`
	Skills  []activeSkill `xml:"skill"`
}

// IMPORTANT: matches spec shape:
// <skill name="..."><![CDATA[SKILL.md body]]></skill>.
type activeSkill struct {
	Name string `xml:"name,attr"`
	Body string `xml:",cdata"`
}

func AvailableSkillsStruct(skills []spec.SkillRecord, includeLocation bool) any {
	sorted := append([]spec.SkillRecord(nil), skills...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	out := availableSkills{Skills: make([]availableSkill, 0, len(sorted))}
	for _, sk := range sorted {
		it := availableSkill{
			Name:        sk.Name,
			Description: sk.Description,
		}
		if includeLocation && strings.TrimSpace(sk.Location) != "" {
			it.Location = sk.Location
		}
		out.Skills = append(out.Skills, it)
	}
	return out
}

func AvailableSkillsXML(skills []spec.SkillRecord, includeLocation bool) (string, error) {
	v := AvailableSkillsStruct(skills, includeLocation)
	b, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("xml encode: %w", err)
	}
	return string(b), nil
}

func ActiveSkillsXML(active []spec.SkillRecord) (string, error) {
	out := activeSkills{Skills: make([]activeSkill, 0, len(active))}
	for _, sk := range active {
		out.Skills = append(out.Skills, activeSkill{
			Name: sk.Name,
			Body: sk.SkillMDBody,
		})
	}
	b, err := xml.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", fmt.Errorf("xml encode: %w", err)
	}
	return string(b), nil
}
