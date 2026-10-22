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
	ParentScratchHost  string
	SiblingScratchHost string
	IsParentUnconfined bool
	IsScratchReadable  bool
	HasShell           bool
	UserHome           string
	Paths              shell.Paths
	Skills             []skill.Skill
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
		subagentStateSection(config),
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
	lines := []string{pathToolRule(config.DropsDirectory != "", config.Paths)}
	lines = append(lines, denyRules(config.Paths.Deny)...)
	lines = append(lines, configuredPathRules(config.Paths, config.ScratchDir, config.UserHome, config.HasShell)...)
	if config.HasShell {
		lines = append(lines, shellExecutionRules(skill.HasGlobal(config.Skills))...)
		lines = append(lines, shellRootRule)
	}
	lines = append(
		lines,
		"- Everything else is out of your reach",
		"- The shell can still stat such a path, but opening it gives a permission error",
		"- Some such paths do not exist in the sandbox at all",
		"- Path tools refuse such a path as outside the root",
		"- Report such a path as out of your reach, never as missing or broken",
	)
	return section("Scope", lines)
}

func subagentStateSection(config SubagentConfig) string {
	lines := []string{
		workspaceStateRule("The workspace ("+config.WorkspaceDir+")", false),
		bashAccessRule(config.HasShell, false),
	}
	if config.HasShell {
		lines = append(lines, leftBehindRule(false))
	}
	return section("State", lines)
}

func subagentNetworkSection(config SubagentConfig) string {
	if !config.HasShell {
		return ""
	}

	return section("Network", []string{
		loopbackRule,
		hostNetworkChoiceRule,
		hostNetworkWithheldRule,
		"- Stay on the private loopback",
	})
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
		"- "+UntrustedDropsRule(config.DropsDirectory),
	)
	return section("Identity", lines)
}

func parentScratchRules(config SubagentConfig) []string {
	view := "- Your parent has its own separate /tmp, where your /tmp appears as " + config.ParentScratchDir
	if config.IsParentUnconfined {
		view = "- Your parent sees the host's /tmp, and your /tmp as " + config.ParentScratchDir
	}
	lines := []string{
		view,
		"- Whenever you give your parent a path under /tmp, including one you ask it to write to, " +
			"give only the path as your parent sees it: write " + filepath.Join(config.ParentScratchDir, "notes.txt") +
			", never /tmp/notes.txt, even labelled as your side",
	}
	missingFileRule := "- If a /tmp path your parent mentions is missing from your /tmp, it is in your parent's: " +
		"ask your parent to paste its contents or to copy it to " + filepath.Join(config.ParentScratchDir, "<file>")
	siblings := config.SiblingScratchHost
	switch {
	case config.IsScratchReadable && !config.IsParentUnconfined:
		lines = append(
			lines,
			"- You can read your parent's /tmp at "+config.ParentScratchHost+", and every other subagent's scratch beneath "+siblings,
			"- You cannot write to your parent's /tmp or to another subagent's scratch",
		)
		missingFileRule = "- If a /tmp path your parent mentions is missing from your /tmp, it is in your parent's: " +
			"read it beneath " + config.ParentScratchHost + ", such as " + filepath.Join(config.ParentScratchHost, "notes.txt") + " for /tmp/notes.txt"
	case config.IsScratchReadable:
		lines = append(
			lines,
			"- You cannot see your parent's /tmp",
			"- You can read every other subagent's scratch beneath "+siblings+", but you cannot write to it",
		)
	default:
		lines = append(lines, "- You cannot see your parent's /tmp or any other subagent's scratch")
	}
	return append(lines, "- Your parent can write files into your /tmp for you", missingFileRule)
}

func section(heading string, lines []string) string {
	return "# " + heading + "\n\n" + strings.Join(lines, "\n")
}
