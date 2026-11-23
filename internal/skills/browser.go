package skills

import _ "embed"

// KitBrowserName is the identity of Kit's in-app browser skill, which a user
// or project skill of the same name replaces.
const KitBrowserName = "kit-browser"

//go:embed kit-browser.md
var kitBrowserDefinition string

// kitBrowserTools are the tools the Kit app's browser plugin hosts (ADR 0051)
// and the browser skill teaches. Kit offers the skill only while all of them
// are connected.
var kitBrowserTools = []string{"browser__act", "browser__navigate", "browser__screenshot", "browser__snapshot", "browser__tabs"}

// kitBrowserSkill is the in-app browser skill.
func kitBrowserSkill() Skill {
	skill := embeddedSkill(KitBrowserName, kitBrowserDefinition)
	skill.ClientTools = append([]string(nil), kitBrowserTools...)
	return skill
}
