package spec

const (
	skillsRulesCommon = `
Rules:
1) Only use skills that are listed in the provided skills prompt.
2) Prefer reading skill resources (skills-readresource) before running scripts.
3) After calling skills-load or skills-unload, rely on the updated skills context in subsequent turns.`

	skillsToolsBase = `You have access to "skills" tools:
- skills-load
- skills-unload
- skills-readresource`

	skillsToolsLoadOnly = `You have access to tool "skills-load". After you load at least one skill, more skills tools may be available.`

	skillsToolsAllWithRunScript = skillsToolsBase + "\n" + "- skills-runscript"
)

const (
	SkillsRulesPromptLoadOnly = skillsToolsLoadOnly + "\n" + skillsRulesCommon

	SkillsRulesPromptWithoutRunScript = skillsToolsBase + "\n" + skillsRulesCommon

	SkillsRulesPromptAll = skillsToolsAllWithRunScript + "\n" + skillsRulesCommon
)
