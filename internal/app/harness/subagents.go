package harness

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"crdx.org/oh/internal/app/backend"
	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/pictures"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/subagents"
	"crdx.org/oh/internal/app/toolset"
	"crdx.org/oh/internal/app/usage"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/internal/util/pathutil"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox"
)

type childOptions struct {
	parentChoice    model.Choice
	endpoints       backend.EndpointSettings
	workspace       *work.Space
	settings        config.Config
	sessionName     string
	sessionsDir     string
	scratchParent   string
	runner          sandbox.Runner
	ensurePersisted func() error
	isYolo          bool
	parentFiles     *file.Root
	parentWritable  func() []string
	parentCaps      func() caps.Set
	userHome        string
}

func userHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

type preparedChildManager struct {
	manager *subagents.Manager
	rotate  func(config.Subagent, model.Defaults) (subagents.Rotation, error)
}

func prepareChildManager(options childOptions, choices []model.Choice, seenModelsPath string, isPrinting bool) (preparedChildManager, error) {
	if len(options.settings.Subagent.Rotation()) == 0 || isPrinting {
		return preparedChildManager{}, nil
	}
	rotate := func(settings config.Subagent, defaults model.Defaults) (subagents.Rotation, error) {
		return childRotationOf(options.parentChoice, settings, defaults, choices, seenModelsPath, options.endpoints.OverrideURL != "")
	}
	rotation, err := rotate(options.settings.Subagent, options.settings.Defaults.ForSelections())
	if err != nil {
		return preparedChildManager{}, err
	}
	manager, err := subagents.New(subagents.Options{
		Directory:   session.ChildrenDir(options.sessionsDir, options.sessionName),
		Scratch:     options.scratchParent,
		Parent:      options.sessionName,
		Models:      rotation.Models,
		Choose:      rotation.Choose,
		Concurrency: rotation.Concurrency,
		Meta: store.Meta{
			WorkspaceDir: options.workspace.GetDir(),
			Yolo:         options.isYolo,
		},
		Factory:      newChildFactory(options),
		PickName:     rand.IntN,
		EnsureParent: options.ensurePersisted,
		Workspace:    childWorkspace(options),
		ScratchNote:  childScratchNote(options),
		Caps: func() caps.Set {
			if options.isYolo {
				return caps.Unconfined()
			}
			return options.parentCaps() & (caps.Read | caps.Shell)
		},
	})
	return preparedChildManager{manager: manager, rotate: rotate}, err
}

func childRotationOf(
	parentChoice model.Choice,
	settings config.Subagent,
	defaults model.Defaults,
	choices []model.Choice,
	seenModelsPath string,
	isSimulated bool,
) (subagents.Rotation, error) {
	rotation := settings.Rotation()
	setting := settings.Setting()
	if len(rotation) == 0 {
		return subagents.Rotation{
			Choose: func() (subagents.ChildModel, error) {
				return subagents.ChildModel{}, errors.New("no subagent model is configured, so no subagent can start")
			},
			Concurrency: settings.Concurrency,
		}, nil
	}
	selections, err := model.ParseRoundRobin(choices, rotation, defaults)
	if err != nil {
		return subagents.Rotation{}, fmt.Errorf("%s: %w", setting, err)
	}
	isOllamaOnly := parentChoice.Provider == model.OllamaProvider
	models := make([]subagents.ChildModel, 0, len(selections))
	allowedSelections := make([]model.Selection, 0, len(selections))
	for _, selection := range selections {
		choice, err := model.Chosen(choices, seenModelsPath, selection.Provider, selection.Model)
		if err != nil {
			return subagents.Rotation{}, fmt.Errorf("%s: %w", setting, err)
		}
		if isOllamaOnly && choice.Provider != model.OllamaProvider {
			continue
		}
		models = append(models, subagents.ChildModel{Choice: choice, Selection: selection})
		allowedSelections = append(allowedSelections, selection)
	}
	if len(models) == 0 {
		return subagents.Rotation{
			Choose: func() (subagents.ChildModel, error) {
				return subagents.ChildModel{}, fmt.Errorf(
					"this session runs on ollama, so its subagents run only on ollama models, and %s names none",
					setting,
				)
			},
			Concurrency: settings.Concurrency,
		}, nil
	}
	selections = allowedSelections
	return subagents.Rotation{
		Models:      models,
		Choose:      childRotation(models, selections, isSimulated),
		Concurrency: settings.Concurrency,
	}, nil
}

func childRotation(models []subagents.ChildModel, selections []model.Selection, isSimulated bool) func() (subagents.ChildModel, error) {
	return func() (subagents.ChildModel, error) {
		now := time.Now()
		selection, err := model.ReserveAvailableRoundRobin(
			location.GetSubagentRoundRobinPath(),
			selections,
			func(candidate model.Selection) bool {
				return usage.IsSelectionAvailable(
					location.GetUsageCachePath(candidate.Provider, isSimulated),
					candidate.Model,
					now,
				)
			},
		)
		if err != nil {
			return subagents.ChildModel{}, err
		}
		for _, candidate := range models {
			if candidate.Selection == selection {
				return candidate, nil
			}
		}
		return subagents.ChildModel{}, fmt.Errorf("the subagent rotation chose %s, which it does not hold", selection)
	}
}

func newChildFactory(options childOptions) subagents.Factory {
	return func(ctx context.Context, child subagents.Child) (subagents.Worker, error) {
		home := filepath.Join(child.Scratch, subagents.HomeName)
		if err := child.ScratchRoot.Mkdir(subagents.HomeName, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return subagents.Worker{}, err
		}
		mode := caps.NewMode(child.Caps)
		workspace := work.At(child.Workspace)
		if err := workspace.Open(); err != nil {
			return subagents.Worker{}, err
		}
		files := file.New(workspace.GetRoot(), caps.RefuseWrite(mode))
		paths := childPaths(options.settings.Sandbox)
		access, err := shell.NewPathAccess(files, mode, paths)
		if err != nil {
			_ = workspace.Close()
			return subagents.Worker{}, err
		}
		homeDirectoryRoot, err := child.ScratchRoot.OpenRoot(subagents.HomeName)
		if err != nil {
			access.Close()
			_ = workspace.Close()
			return subagents.Worker{}, err
		}
		files.Mount(home, file.New(homeDirectoryRoot, func(string) error { return nil }))
		files.Mount(sandbox.TmpDir, file.New(child.ScratchRoot, func(string) error { return nil }))
		closeChild := func() {
			_ = homeDirectoryRoot.Close()
			access.Close()
			_ = workspace.Close()
		}
		if err := shell.PrepareHomeMappings(workspace.GetDir(), home, child.Scratch, paths, child.Caps); err != nil {
			closeChild()
			return subagents.Worker{}, err
		}
		runner := options.runner
		if options.isYolo {
			runner = sandbox.Direct()
		}
		tools := toolbox.RummageWithRunner(files, file.NewSnapshots(), grepRunner(options.isYolo))
		tools = append(tools, shell.NewForSubagent(
			workspace.GetDir(), home, child.Scratch, shell.Parent{Scratch: options.scratchParent, Writable: options.parentWritable},
			access, mode, files, options.isYolo,
			func(context.Context, string, string) error {
				return errors.New("host network is not available to this subagent")
			},
			runner,
		))
		prompt := child.SystemPrompt
		if prompt == "" {
			prompt = childPrompt(options, workspace, child, home, tools)
		}
		connection, err := backend.Connect(child.Choice, child.Selection, options.endpoints)
		if err != nil {
			closeChild()
			return subagents.Worker{}, err
		}
		connection.UseSession(options.sessionName + "-" + child.Name)
		connection.ObserveHTTP(child.Observer)
		provider := usage.Guard(ctx, connection.Client, usage.GuardSettings{
			ProviderName: model.ProviderName(child.Selection.Provider),
			ModelName:    child.Selection.Model,
			CachePath:    location.GetUsageCachePath(child.Selection.Provider, options.endpoints.OverrideURL != ""),
			Now:          time.Now,
		})
		worker, changes, err := childAgent(prompt, provider, tools, child)
		if err != nil {
			closeChild()
			return subagents.Worker{}, err
		}
		worker.StorePicturesWith(func(picture tool.Image) *agent.Picture {
			reference, storeError := pictures.Store(child.Directory, child.EnsurePersisted, picture)
			if storeError != nil {
				return nil
			}
			return reference
		})
		return subagents.Worker{Agent: worker, Tools: tools, SystemPrompt: prompt, Changes: changes, Close: closeChild}, nil
	}
}

func childAgent(prompt string, provider agent.Provider, tools []tool.Tool, child subagents.Child) (*agent.Agent, []agent.Event, error) {
	if len(child.FrozenTools) == 0 {
		return agent.New(prompt, provider, tools), nil, nil
	}
	frozenSet := toolset.Restore(tools, store.RestoreTools(child.FrozenTools), nil)
	availability, err := toolset.RestoreAvailability(child.History, frozenSet.Availability, frozenSet.VersionChanges)
	if err != nil {
		return nil, nil, err
	}
	var changes []agent.Event
	if availability.IsChanged {
		changes = append(changes, availability.Change)
	}
	return agent.NewWithEnabledTools(prompt, provider, frozenSet.RegisteredTools, frozenSet.OfferedTools), changes, nil
}

func childPrompt(options childOptions, workspace *work.Space, child subagents.Child, home string, tools []tool.Tool) string {
	commonGround := "You are a subagent of session " + options.sessionName + ". " +
		"Report your findings in your final answer. " +
		"Your parent may send you messages while you work, and follow-ups after you answer."
	var circumstances, whereabouts string
	switch {
	case options.isYolo:
		circumstances = "You run without a sandbox, including for shell commands. " +
			"/tmp is the host's shared /tmp. You can change host files and use the host network. " +
			"Do not claim read-only confinement."
		whereabouts = "TMPDIR is " + child.Scratch + "."
	case child.Caps.Has(caps.Shell):
		circumstances = "Your workspace is read-only. You have shell execution, but not host networking. " +
			"/tmp is your private writable scratch, and it outlives your answer. " +
			parentScratchView(child.Name) + "\n\n" + reachable(options, true)
		whereabouts = "/tmp is at " + child.Scratch + " on the host. HOME is " + home + "."
	default:
		circumstances = "Your workspace is read-only. " +
			"You have no shell execution, so bash refuses every command; read, search and list files instead.\n\n" +
			reachable(options, false)
		whereabouts = "HOME is " + home + "."
	}
	parts := []string{commonGround + " " + circumstances}
	if child.SharedPrompt != "" {
		parts = append(parts, child.SharedPrompt)
	}
	own := "Your identity is " + child.Name + ". Your workspace is " + workspace.GetDir() + ". "
	if whereabouts != "" {
		own += whereabouts + " "
	}
	own += "Your tools are: " + toolNames(tools) + "."
	return strings.Join(append(parts, own), "\n\n")
}

func childPaths(parentPaths shell.Paths) shell.Paths {
	return shell.Paths{
		Deny: parentPaths.Deny,
		Read: parentPaths.Read,
		Exec: parentPaths.Exec,
		Path: parentPaths.Path,
		Home: parentPaths.Home,
	}
}

func reachable(options childOptions, hasShell bool) string {
	sandboxPaths := options.settings.Sandbox
	readable := "You can read your workspace, /tmp, HOME, and the read-only system and executable search paths"
	searchPaths := slices.Concat(sandboxPaths.Exec, sandboxPaths.Path)
	if len(searchPaths) > 0 {
		readable += ", including " + strings.Join(searchPaths, ", ")
	}
	sentences := []string{readable + "."}
	alsoReadable := slices.Clone(sandboxPaths.Read)
	var homeViews []string
	for _, path := range sandboxPaths.Home {
		if relative, isHomePath := pathutil.RelativeTo(options.userHome, path); options.userHome != "" && isHomePath {
			homeViews = append(homeViews, path+" appears read-only at HOME/"+relative+".")
			continue
		}
		alsoReadable = append(alsoReadable, path)
	}
	if len(alsoReadable) > 0 {
		sentences = append(sentences, "You can also read "+strings.Join(alsoReadable, ", ")+".")
	}
	sentences = append(sentences, homeViews...)
	if hasShell {
		sentences = append(sentences,
			"You can write only /tmp and HOME. "+
				"The shell can execute files under system directories, the executable search paths, the workspace, HOME, and /tmp.",
		)
	} else {
		sentences = append(sentences, "You can write nothing.")
	}
	if len(sandboxPaths.Deny) > 0 {
		sentences = append(sentences, "A file or directory named "+strings.Join(sandboxPaths.Deny, ", ")+" is hidden from you.")
	}
	home := "HOME and ~ are your own, not the user's"
	if options.userHome != "" {
		home += ", whose home is " + options.userHome + "; write paths there in full"
	}
	sentences = append(sentences,
		home+".",
		"The sandbox hides everything else rather than refusing it, so a path outside what you can reach looks absent. "+
			"Its absence is no evidence that it is missing: report it as out of your reach, not as missing or broken.",
	)
	return strings.Join(sentences, " ")
}

func toolNames(tools []tool.Tool) string {
	var names []string
	for _, current := range tools {
		names = append(names, current.Name())
	}
	return strings.Join(names, ", ")
}

func parentScratchPath(name string) string {
	return filepath.Join(sandbox.TmpDir, subagents.ScratchName, name)
}

func parentScratchView(name string) string {
	parentPath := parentScratchPath(name)
	return "Your parent has its own separate /tmp, where your /tmp appears as " + parentPath + ". " +
		"Whenever you give your parent a path under /tmp, including one you ask it to write to, " +
		"give only the path as your parent sees it: write " +
		filepath.Join(parentPath, "notes.txt") + ", never /tmp/notes.txt, even labelled as your side. " +
		"You cannot see your parent's /tmp or any other subagent's scratch, " +
		"but your parent can write files into your /tmp for you. " +
		"If a /tmp path your parent mentions is missing from your /tmp, it is in your parent's: " +
		"ask your parent to paste its contents or to copy it to " + filepath.Join(parentPath, "<file>") + "."
}

const unverifiedNote = "unverified: check what matters before relying on it; "

func childScratchNote(options childOptions) func(string) string {
	return func(name string) string {
		if options.isYolo {
			return unverifiedNote + "its TMPDIR is " + filepath.Join(options.scratchParent, subagents.ScratchName, name)
		}
		return unverifiedNote + "its /tmp is your " + parentScratchPath(name) + ", so read any /tmp path it reports as beneath that"
	}
}

func childWorkspace(options childOptions) func(string) (string, error) {
	parentWorkspace := options.workspace.GetDir()
	return func(directory string) (string, error) {
		if directory == "" {
			return parentWorkspace, nil
		}
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(parentWorkspace, directory)
		}
		if options.isYolo {
			return hostDirectory(directory)
		}
		hostPath, err := readableDirectory(options.parentFiles, directory)
		if err != nil {
			return "", err
		}
		resolvedPath, err := filepath.EvalSymlinks(hostPath)
		if err != nil {
			return "", fmt.Errorf("%s cannot be resolved: %w", directory, err)
		}
		if work.IsShadowed(resolvedPath) {
			return "", fmt.Errorf("%s leads into the host's /tmp, which no subagent can read", directory)
		}
		if resolvedPath == hostPath {
			return hostPath, nil
		}
		if _, err := readableDirectory(options.parentFiles, resolvedPath); err != nil {
			return "", err
		}
		return resolvedPath, nil
	}
}

func hostDirectory(directory string) (string, error) {
	resolvedPath, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return "", fmt.Errorf("%s cannot be resolved: %w", directory, err)
	}
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", directory)
	}
	return resolvedPath, nil
}

func readableDirectory(files *file.Root, directory string) (string, error) {
	root, name, err := files.Resolve(directory)
	if err != nil {
		return "", fmt.Errorf("%s is not a directory you can read", directory)
	}
	info, err := root.Stat(name)
	if err != nil {
		return "", fmt.Errorf("%s is not a directory you can read: %w", directory, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", directory)
	}
	return filepath.Join(root.Name(), name), nil
}
