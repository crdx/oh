package skill

import (
	"encoding/xml"
	"strings"
)

type root struct {
	XMLName xml.Name `xml:"available_skills"`
	Skills  []skill  `xml:"skill"`
}

type skill struct {
	Name        string `xml:"name"`
	Description string `xml:"description"`
	Location    string `xml:"location"`
}

func Context(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}

	listedSkills := make([]skill, 0, len(skills))
	for _, foundSkill := range skills {
		listedSkills = append(listedSkills, skill{
			Name:        foundSkill.Name,
			Description: foundSkill.Description,
			Location:    foundSkill.Location,
		})
	}

	xmlStr, err := xml.MarshalIndent(root{Skills: listedSkills}, "", "  ")
	if err != nil {
		return ""
	}

	var out strings.Builder

	out.WriteString("# Skills\n\n")
	out.WriteString("When a task matches a skill description below, use the read tool to read its SKILL.md at the listed location before proceeding. Resolve paths mentioned by a skill relative to the directory containing that SKILL.md. Read other bundled files only as the skill directs.\n\n")
	out.Write(xmlStr)

	return out.String()
}
