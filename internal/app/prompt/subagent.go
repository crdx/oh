package prompt

import (
	"path/filepath"
	"strings"

	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/skill"
	"crdx.org/oh/pkg/agent"
)

type SubagentConfig struct {
	SessionName        string
	Name               string
	WorkspaceDir       string
	HomeDir            string
	ScratchDir         string
	ParentScratchDir   string
	IsParentUnconfined bool
	HasShell           bool
	UserHome           string
	Paths              shell.Paths
	Skills             []skill.Skill
	ToolNames          []string
	DropsDirectory     string
	SharedPrompt       string
	Notes              agent.NoteFormat
}

func Subagent(config SubagentConfig) string {
	return mergeContexts(
		subagentSection(config),
		subagentScopeSection(config),
		subagentNetworkSection(config),
		subagentScratchSection(),
		subagentHomeSection(),
		skill.Context(config.Skills),
		config.SharedPrompt,
		subagentIdentitySection(config),
	)
}

func subagentSection(config SubagentConfig) string {
	return section("Subagent", []string{
		"- You are a subagent of session " + config.SessionName,
		"- Report your findings in your final answer",
		"- Your parent may send you messages while you work, and follow-ups after you answer",
		"- " + NoteRule(config.Notes),
	})
}

func subagentScopeSection(config SubagentConfig) string {
	lines := []string{
		workspaceStateRule("The workspace ("+config.WorkspaceDir+")", false),
		pathToolRule(config.DropsDirectory != "", config.Paths),
	}
	lines = append(lines, denyRules(config.Paths.Deny)...)
	lines = append(lines, configuredPathRules(config.Paths, config.ScratchDir, config.UserHome, config.HasShell)...)
	if config.HasShell {
		lines = append(lines, shellExecutionRules(skill.HasGlobal(config.Skills))...)
	}
	lines = append(
		lines,
		"- The sandbox hides everything else rather than refusing it, so a path outside what you can reach looks absent. "+
			"Its absence is no evidence that it is missing: report it as out of your reach, not as missing or broken.",
		bashAccessRule(config.HasShell, false),
	)
	return section("Scope", lines)
}

func subagentNetworkSection(config SubagentConfig) string {
	if !config.HasShell {
		return ""
	}

	return section("Network", append([]string{loopbackRule}, hostNetworkRules(false, false)...))
}

func subagentScratchSection() string {
	return section("/tmp", []string{scratchPersistenceRule})
}

func subagentHomeSection() string {
	return section("Home", []string{homeWriteRule(true), tildeRules()})
}

func subagentIdentitySection(config SubagentConfig) string {
	lines := []string{
		"- Your identity is " + config.Name,
		"- /tmp is at " + config.ScratchDir + " on the host",
	}
	lines = append(lines, parentScratchRules(config)...)
	lines = append(
		lines,
		"- HOME is "+config.HomeDir,
		"- Your tools are: "+strings.Join(config.ToolNames, ", "),
		"- "+UntrustedDropsRule(config.DropsDirectory),
	)
	return section("Identity", lines)
}

func parentScratchRules(config SubagentConfig) []string {
	view := "- Your parent has its own separate /tmp, where your /tmp appears as " + config.ParentScratchDir
	if config.IsParentUnconfined {
		view = "- Your parent sees the host's /tmp, and your /tmp as " + config.ParentScratchDir
	}
	return []string{
		view,
		"- Whenever you give your parent a path under /tmp, including one you ask it to write to, " +
			"give only the path as your parent sees it: write " + filepath.Join(config.ParentScratchDir, "notes.txt") +
			", never /tmp/notes.txt, even labelled as your side",
		"- You cannot see your parent's /tmp or any other subagent's scratch, but your parent can write files into your /tmp for you",
		"- If a /tmp path your parent mentions is missing from your /tmp, it is in your parent's: " +
			"ask your parent to paste its contents or to copy it to " + filepath.Join(config.ParentScratchDir, "<file>"),
	}
}

func section(heading string, lines []string) string {
	return "# " + heading + "\n\n" + strings.Join(lines, "\n")
}
