package prompt

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"crdx.org/hereduck"
	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/conditions"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/skill"
	"crdx.org/oh/internal/app/toolset"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/util/pathutil"
	"crdx.org/oh/internal/util/strutil"
)

const (
	shellToolName  = "bash"
	jobToolName    = "job"
	lookupToolName = "lookup"
	fetchToolName  = "fetch"
	titleToolName  = "title"
	notifyToolName = "notify"

	clipboardDropsHeading = "# Clipboard Drops"
	defaultGlobalContext  = "You are a helpful coding assistant."
	globalContextName     = "SYSTEM.md"
)

var (
	projectContextNames    = []string{"AGENTS.md", "AGENTS.local.md"}
	harnessContextTemplate = template.Must(template.New("harness").Funcs(template.FuncMap{
		"filesystem":               filesystem,
		"filepathJoin":             filepath.Join,
		"scopeRules":               scopeRules,
		"shellAccess":              shellAccess,
		"lookupAccess":             lookupAccess,
		"networkSection":           networkSection,
		"stateRules":               stateRules,
		"scratchRules":             scratchRules,
		"waitingForUserSection":    waitingForUserSection,
		"readOnlyWorkspaceSection": readOnlyWorkspaceSection,
		"homeWriteRule":            homeWriteRule,
		"shellSandbox":             shellSandbox,
		"sandboxHeader":            sandboxHeader,
		"titleSection":             titleSection,
		"notifySection":            notifySection,
	}).Parse(hereduck.D(`
		{{ sandboxHeader .Yolo .ShellOffered }}# Harness

		- "oh" is the harness you are running within
		- Each session dir is under {{ .SessionsDir }}, named after the session
		- This session's directory is {{ .SessionDir }}
		- "session.jsonl" is the journal, the single source of truth, as JSONL
		- "meta.json" is the listing entry: name, title, timestamps, and message count
		- "chat.md" is the readable transcript of the conversation
		- "wire.http" is the raw traffic between the harness and the model endpoint
		- The user's settings are in {{ .ConfigFile }}, and their instructions in {{ .GlobalPath }}
		- A session name said with no other context is a hint to read that session's files

		# Scope

		- Your workspace is the current directory, {{ .WorkspaceDir }}
		- Your session is named {{ .SessionName }}
		{{ scopeRules . }}

		# Personality

		- Use casual lowercase when chatting with the user, but write normally everywhere else
		- Adopt the personality of the animal in your session name, and use its emoji

		{{ networkSection . }}# /tmp

		{{ scratchRules . }}

		# Home

		- HOME is {{ .HomeDir }}, which is exclusively for you and your agent companions
		{{ homeWriteRule . }}
		- A tilde (~) for you is not the same as for the user. The user has their own HOME.
		- Every path on the user's machine, including the ones above, is written here in full
		- Write them the same way back, and never abbreviate one to a tilde

		# State

		{{ stateRules . }}

		These states can change at any time. You will be told what changed when it does.
		When a state blocks the work and no workflow below covers it, ask the user to change that state.

		{{ titleSection . }}{{ notifySection . }}{{ waitingForUserSection . }}{{ readOnlyWorkspaceSection . }}
	`)))
)

type harnessContextTemplateData struct {
	WorkspaceDir      string
	SessionName       string
	SessionsDir       string
	SessionDir        string
	ConfigFile        string
	GlobalPath        string
	TmpDir            string
	HomeDir           string
	ExtraPaths        shell.Paths
	DropsDirectory    string
	ShellOffered      bool
	TitleOffered      bool
	NotifyOffered     bool
	LookupOffered     bool
	FetchOffered      bool
	Conditions        conditions.Conditions
	WorkspaceWritable bool
	IsRepository      bool
	GitWritable       bool
	ShellGranted      bool
	LookupGranted     bool
	JobsGranted       bool
	NetworkGranted    bool
	Yolo              bool
}

func ProjectContextPaths(workspace *work.Space) []string {
	paths := make([]string, 0, len(projectContextNames))
	for _, name := range projectContextNames {
		paths = append(paths, filepath.Join(workspace.GetDir(), name))
	}
	return paths
}

type File struct {
	Name string
	Body string
}

type Config struct {
	GlobalPath     string
	Workspace      *work.Space
	SessionName    string
	SessionsDir    string
	SessionDir     string
	ConfigFile     string
	TmpDir         string
	HomeDir        string
	CurrentCaps    caps.Set
	ExtraPaths     shell.Paths
	DropsDirectory string
	OfferedTools   []string
	Skills         []skill.Skill
	Conditions     conditions.Conditions
	JobsGranted    bool
	NetworkGranted bool
	Yolo           bool
}

func Load(config Config) (string, []File, error) {
	globalFile, err := readGlobalContext(config.GlobalPath)
	if err != nil {
		return "", nil, err
	}

	projectFiles, err := readProjectContext(config.Workspace.GetRoot())
	if err != nil {
		return "", nil, err
	}

	files := projectFiles
	if globalFile != nil {
		files = append([]File{*globalFile}, projectFiles...)
	}

	return mergeContexts(
		harnessContext(config),
		globalContext(globalFile),
		projectContext(projectFiles),
		skill.Context(config.Skills),
	), files, nil
}

func readContextFile(name string, read func() ([]byte, error)) (*File, error) {
	data, err := read()
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil //nolint:nilnil // an absent context file is nothing to report
	case err != nil:
		return nil, err
	}

	if strings.TrimSpace(string(data)) == "" {
		return nil, nil //nolint:nilnil // an empty context file is nothing to report
	}

	return &File{Name: name, Body: string(data)}, nil
}

func readGlobalContext(path string) (*File, error) {
	file, err := readContextFile(globalContextName, func() ([]byte, error) {
		return os.ReadFile(path) //nolint:gosec // this is the one documented config path
	})
	if err != nil {
		return nil, fmt.Errorf("could not read the system context %s: %w", path, err)
	}

	return file, nil
}

func readProjectContext(root *os.Root) ([]File, error) {
	var files []File

	for _, name := range projectContextNames {
		file, err := readContextFile(name, func() ([]byte, error) { return root.ReadFile(name) })
		if err != nil {
			return nil, fmt.Errorf("could not read the project context %s: %w", name, err)
		}

		if file != nil {
			files = append(files, *file)
		}
	}

	return files, nil
}

func globalContext(file *File) string {
	if file == nil {
		return defaultGlobalContext
	}

	return file.Body
}

func harnessContext(config Config) string {
	currentCaps := config.CurrentCaps
	data := harnessContextTemplateData{
		WorkspaceDir:      config.Workspace.GetDir(),
		SessionName:       config.SessionName,
		SessionsDir:       config.SessionsDir,
		SessionDir:        config.SessionDir,
		ConfigFile:        config.ConfigFile,
		GlobalPath:        config.GlobalPath,
		TmpDir:            config.TmpDir,
		HomeDir:           config.HomeDir,
		ExtraPaths:        config.ExtraPaths,
		DropsDirectory:    config.DropsDirectory,
		ShellOffered:      toolset.Offers(config.OfferedTools, shellToolName),
		TitleOffered:      toolset.Offers(config.OfferedTools, titleToolName),
		NotifyOffered:     toolset.Offers(config.OfferedTools, notifyToolName),
		LookupOffered:     toolset.Offers(config.OfferedTools, lookupToolName),
		FetchOffered:      toolset.Offers(config.OfferedTools, fetchToolName),
		Conditions:        config.Conditions,
		WorkspaceWritable: currentCaps.Has(caps.Write),
		IsRepository:      pathutil.Exists(filepath.Join(config.Workspace.GetDir(), ".git")),
		GitWritable:       currentCaps.Has(caps.Git),
		ShellGranted:      currentCaps.Has(caps.Shell),
		JobsGranted:       config.JobsGranted && toolset.Offers(config.OfferedTools, jobToolName),
		NetworkGranted:    config.NetworkGranted,
		LookupGranted:     currentCaps.Has(caps.Lookup),
		Yolo:              config.Yolo,
	}

	var renderedText strings.Builder
	if err := harnessContextTemplate.Execute(&renderedText, data); err != nil {
		panic(err)
	}
	return strings.TrimSpace(renderedText.String())
}

func WithDropsDirectory(systemPrompt string, dropsDirectory string) string {
	if dropsDirectory == "" {
		return systemPrompt
	}
	rule := dropsRule(dropsDirectory)
	if strings.Contains(systemPrompt, rule) {
		return systemPrompt
	}
	return strings.TrimSpace(systemPrompt) + "\n\n" + clipboardDropsHeading + "\n\n- " + rule
}

func dropsRule(dropsDirectory string) string {
	return "Pasting a clipboard image with ctrl+v saves it under " + dropsDirectory + ", where path tools can read it."
}

func titleSection(data harnessContextTemplateData) string {
	if !data.TitleOffered {
		return ""
	}

	return "# Titles\n\n" + strings.Join([]string{
		"- Use the title tool as soon as you can to title the conversation.",
		"- Use a 3-word, hyphen-separated title following VERB–MODIFIER–NOUN pattern",
		"- Keep its total length ≤ 30 characters.",
		"- Do not go on a long task without setting a title first.",
		"- Even a provisional title is fine; it can be updated whenever.",
	}, "\n") + "\n\n"
}

func notifySection(data harnessContextTemplateData) string {
	if !data.NotifyOffered {
		return ""
	}

	return "# Notifications\n\n" + strings.Join([]string{
		"- The notify tool alerts the user via a desktop notification when the terminal is not focused.",
		"- Use it to get the user's attention, or tell the user what work you've done.",
		"- Ensure you call the tool *before* you output your response and end your turn.",
		"- Do not send any for regular back-and-forth conversation where the user is clearly engaged.",
	}, "\n") + "\n\n"
}

func scopeRules(data harnessContextTemplateData) string {
	extraPaths := data.ExtraPaths
	dropsDirectory := data.DropsDirectory

	var lines []string

	configuredPathCount := len(extraPaths.Read) + len(extraPaths.Write) + len(extraPaths.Exec) +
		len(extraPaths.Path) + len(extraPaths.Home)
	switch {
	case dropsDirectory != "":
		lines = append(lines, "- Tools that accept a path can access the workspace, private home, /tmp, read-only system and executable search paths, and the paths listed here.")
	case configuredPathCount > 0:
		lines = append(lines, "- Tools that accept a path can access the workspace, private home, /tmp, read-only system and executable search paths, and the configured paths listed here.")
	default:
		lines = append(lines, "- Tools that accept a path can only access the workspace, private home, /tmp, and read-only system and executable search paths.")
	}

	if dropsDirectory != "" {
		lines = append(lines, "- "+dropsRule(dropsDirectory))
	}
	for _, pattern := range extraPaths.Deny {
		lines = append(lines, "- Path tools and shell commands cannot access any file or directory named by the configured deny pattern "+pattern+".")
	}
	if len(extraPaths.Deny) > 0 {
		lines = append(lines, "- A denied path appears as an empty unreadable file or directory, so tools (e.g. git) may show the file with modifications. It can safely be ignored.")
	}
	for _, path := range extraPaths.Read {
		lines = append(lines, "- The configured path "+path+" is read-only"+scratchException(path, data)+".")
	}
	for _, path := range extraPaths.Write {
		lines = append(lines, "- The configured path "+path+" is read-write.")
	}
	for _, path := range extraPaths.Exec {
		lines = append(lines, "- The configured executable path "+path+" is read-only to path tools.")
	}
	for _, path := range extraPaths.Path {
		lines = append(lines, "- The configured PATH directory "+path+" is read-only to path tools.")
	}
	for _, path := range extraPaths.Home {
		relative, isHomePath := shell.HomeRelativePath(path)
		if isHomePath {
			lines = append(lines, "- The configured home path "+path+" is read-only and exposed at HOME/"+relative+".")
		} else {
			lines = append(lines, "- The configured home path "+path+" is read-only to path tools but cannot be exposed in private HOME because it is outside the user's home.")
		}
	}
	if data.ShellOffered && !data.Yolo {
		lines = append(lines, "- The shell shares those read grants and additionally sees private process, terminal, resolver, and language-package cache files needed to run commands.")
		lines = append(lines, "- The shell has the same write access as path tools; runtime devices are the only additional writable exceptions.")
		lines = append(lines, "- The shell can execute files under the system directories, every directory in PATH, the workspace, HOME, and /tmp.")
		for _, path := range extraPaths.Exec {
			lines = append(lines, "- The shell can execute files at or under "+path+".")
		}
		for _, path := range extraPaths.Path {
			lines = append(lines, "- The shell can execute files at or under "+path+", which is in PATH.")
		}
	} else if data.ShellOffered && data.Yolo {
		lines = append(lines, "- The shell is unconfined in --yolo mode; path tools remain limited to the paths above.")
	}

	return strings.Join(append(lines, pathGrantRules()...), "\n")
}

func pathGrantRules() []string {
	return []string{
		"- The user can grant access to paths with /grant, and take them back with /revoke.",
		"- Ask the user to grant a needed path rather than working around it or giving up.",
	}
}

func scratchException(path string, data harnessContextTemplateData) string {
	if data.TmpDir == "" {
		return ""
	}
	if _, isCovered := pathutil.RelativeTo(path, data.TmpDir); !isCovered {
		return ""
	}

	return ", apart from your scratch space at " + data.TmpDir + ", which is writable"
}

func projectContext(files []File) string {
	if len(files) == 0 {
		return ""
	}

	sections := make([]string, 0, len(files))
	for _, file := range files {
		sections = append(sections, "## "+file.Name+"\n\n"+strings.TrimSpace(file.Body))
	}

	return "# Project Context\n\n" + strings.Join(sections, "\n\n")
}

func mergeContexts(sections ...string) string {
	out := sections[:0]
	for _, section := range sections {
		if section = strings.TrimSpace(section); section != "" {
			out = append(out, section)
		}
	}
	return strings.Join(out, "\n\n")
}

func filesystem(isWritable bool) string {
	if isWritable {
		return "read-write"
	}

	return "read-only"
}

func shellAccess(isGranted bool) string {
	if isGranted {
		return "granted"
	}

	return "refused"
}

func lookupAccess(isGranted bool) string {
	if isGranted {
		return "granted external network access"
	}

	return "refused"
}

func sandboxHeader(isYolo bool, isShellOffered bool) string {
	if !isYolo || !isShellOffered {
		return ""
	}

	return hereduck.D(`
		# No Sandbox

		- This session was started with --yolo, so the bash tool runs with no sandbox
		- All commands can read, write, delete, and reach the network as freely as the user can
		- Nothing stops a mistake, so read a destructive command back to yourself before running it
		- The states below still govern the file tools; hold the bash tool to them yourself
	`) + "\n"
}

func networkSection(data harnessContextTemplateData) string {
	rules := networkRules(data)
	if rules == "" {
		return ""
	}

	return "# Network\n\n" + rules + "\n\n"
}

func stateRules(data harnessContextTemplateData) string {
	lines := []string{
		"- The workspace (" + data.WorkspaceDir + ") is " + filesystem(data.WorkspaceWritable),
	}

	if data.IsRepository {
		lines = append(lines, "- The .git directory within it ("+
			filepath.Join(data.WorkspaceDir, ".git")+") is "+filesystem(data.GitWritable))
	} else {
		lines = append(lines, "- The workspace is not a git repository")
	}

	if data.ShellOffered {
		lines = append(lines, "- The bash tool is "+shellAccess(data.ShellGranted)+shellSandbox(data.Yolo))
		if !data.Yolo {
			lines = append(lines, "- A process a bash call leaves running is killed when that call ends"+
				jobSurvival(data.JobsGranted))
		}
	}

	return strings.Join(lines, "\n")
}

func jobSurvival(areJobsGranted bool) string {
	if !areJobsGranted {
		return ""
	}

	return ", so start anything that must outlive the call with the job tool"
}

func networkRules(data harnessContextTemplateData) string {
	if !data.ShellOffered {
		return strings.Join(networkToolRules(data), "\n")
	}

	if data.Yolo {
		return strings.Join(append(
			[]string{"- There is no network sandbox: everything runs on the host network"},
			networkToolRules(data)...,
		), "\n")
	}

	loopback := "- Processes in the same sandbox can communicate over 127.0.0.1"
	if data.Conditions.IPv6 {
		loopback += " and ::1"
	} else {
		loopback += ", and this machine has no IPv6 at all"
	}

	lines := []string{
		"- A bash call has no network other than the sandbox's private loopback interface",
		loopback,
	}

	if data.JobsGranted {
		lines = append(
			lines,
			"- A job command has only private loopback networking and cannot request the host network",
			"- A service started with the job tool stays running, and can be reached on 127.0.0.1 afterwards",
		)
	}

	canRequestHostNetwork := data.NetworkGranted
	hostReachability := unreachableRule(
		"the host's loopback interface and external networks are unreachable",
		canRequestHostNetwork,
	)
	lines = append(lines, unixSocketRule(data.Conditions.UnixSockets), hostReachability)
	lines = append(lines, networkToolRules(data)...)

	return strings.Join(append(lines, hostNetworkRules(data.NetworkGranted, data.Conditions.Interactive)...), "\n")
}

func homeWriteRule(data harnessContextTemplateData) string {
	if data.Yolo {
		return "- You can write anywhere inside HOME"
	}

	return "- HOME is writable only while the workspace is writable, " +
		"though .cache inside HOME is always writable"
}

func unixSocketRule(areUnixSocketsReachable bool) string {
	if areUnixSocketsReachable {
		return "- Unix sockets work beneath /tmp, but not beneath the workspace"
	}

	return "- Unix sockets do not work"
}

func unreachableRule(text string, canRequest bool) string {
	if canRequest {
		return "- By default, " + text
	}

	return "- " + strutil.Capitalise(text)
}

func networkToolRules(data harnessContextTemplateData) []string {
	var lines []string

	if data.LookupOffered {
		lines = append(lines, "- The lookup tool is "+lookupAccess(data.LookupGranted))
	}
	if data.FetchOffered {
		lines = append(lines, "- The fetch tool is "+lookupAccess(data.NetworkGranted))
	}

	return lines
}

func hostNetworkRules(isNetworkGranted bool, isInteractive bool) []string {
	lines := []string{
		"- The bash tool takes network=loopback or network=host, and defaults to loopback",
	}

	if !isNetworkGranted {
		lines = append(lines,
			"- The host network is withheld in this session, so a call asking for network=host is "+
				"refused before the command runs")
		if isInteractive {
			return append(
				lines,
				"- The user can grant the host network with ctrl+x n",
				"- Ask the user to grant the host network rather than asking the user to run the command",
			)
		}

		return append(lines,
			"- The user cannot grant the host network here, so keep to the sandbox's private loopback")
	}

	lines = append(
		lines,
		"- A call with network=host runs on the host's own network instead of the private loopback",
		"- A host call reaches the internet, the local network, and the host's own loopback listeners",
		"- A host call cannot reach the sandbox's private loopback, so a sandbox service is out of reach",
	)

	if isInteractive {
		lines = append(
			lines,
			"- The user may be asked to approve each host call, and may refuse the call or let it time out",
		)
	} else {
		lines = append(
			lines,
			"- The user cannot approve a host call, so expect refusal unless permission is granted in advance",
		)
	}

	return append(
		lines,
		"- Ask for the host network only when the work needs the host network, and say why in the call",
	)
}

func scratchRules(data harnessContextTemplateData) string {
	var lines []string

	if data.Yolo {
		lines = []string{
			"- /tmp is the machine's own /tmp, shared with everything else running on it",
			"- Your persistent scratch space is " + data.TmpDir + ", which you can always read and write to",
			"- Give the user that path exactly as it is written here",
		}
	} else {
		lines = []string{
			"- /tmp is your persistent scratch space, which you can always read and write to",
			"- It maps to " + data.TmpDir + " on the user's machine, so bear that in mind",
			"- Always translate /tmp paths to the user's equivalent path before giving it to them",
			"\t- For example: /tmp/foo.png → " + filepath.Join(data.TmpDir, "foo.png"),
		}
	}

	return strings.Join(lines, "\n")
}

func waitingForUserSection(data harnessContextTemplateData) string {
	if !data.JobsGranted {
		return ""
	}

	return strings.Join([]string{
		"# Waiting for the User",
		"",
		"- If waiting on the user, start a job that watches for completion, if feasible",
		"- Define a command whose success proves completion, and have the job check it immediately",
		"- Have the job recheck after each relevant event until the command succeeds",
		"- For filesystem changes, prefer inotifywait on relevant paths; if unavailable, poll with a modest delay",
		"- Once the job completes, continue where you left off",
	}, "\n") + "\n\n"
}

func readOnlyWorkspaceSection(data harnessContextTemplateData) string {
	if !data.ShellOffered {
		return ""
	}

	lines := []string{
		"# Read-only Workspaces",
		"",
		"- When implementation would modify a read-only workspace, use this workflow instead of asking for write access:",
		"\t- Clone it into your scratch space with: git clone --shared <workspace> <destination>",
		"\t- Bring tracked changes across with: git -C <workspace> diff --binary HEAD | git -C <destination> apply",
		"\t- Copy untracked files you need by hand, and use cp -r only when the workspace is not a repository",
		"\t- Do the work and run its checks in the scratch copy",
		"\t- Produce workspace-relative *.patch files the user can apply to the real repository",
		"\t- Check each patch with git -C <workspace> apply --reverse --check <patch>; hand off only unapplied patches",
		"\t- Verify each unapplied patch with: git -C <workspace> apply --check <patch>",
		"\t- Start a watcher (see \"Waiting for the User\") that exits once the user has applied the patch",
		"\t- Tell the user to apply it with: cd <workspace> && git apply <user's path to patch>",
	}

	return strings.Join(lines, "\n")
}

func shellSandbox(isYolo bool) string {
	if isYolo {
		return ", and runs unconfined"
	}

	return ""
}
