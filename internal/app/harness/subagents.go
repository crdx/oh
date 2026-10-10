package harness

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
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
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox"
)

type childOptions struct {
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
}

type preparedChildManager struct {
	manager *subagents.Manager
}

func prepareChildManager(options childOptions, choices []model.Choice, seenModelsPath string, isPrinting bool) (preparedChildManager, error) {
	if options.settings.Subagent.Model == "" || isPrinting {
		return preparedChildManager{}, nil
	}
	selection, err := model.ParseSelection(choices, options.settings.Subagent.Model, options.settings.Model.GetDefaults())
	if err != nil {
		return preparedChildManager{}, fmt.Errorf("subagent.model: %w", err)
	}
	choice, err := model.Chosen(choices, seenModelsPath, selection.Provider, selection.Model)
	if err != nil {
		return preparedChildManager{}, fmt.Errorf("subagent.model: %w", err)
	}
	manager, err := subagents.New(subagents.Options{
		Directory: session.ChildrenDir(options.sessionsDir, options.sessionName),
		Scratch:   options.scratchParent,
		Parent:    options.sessionName,
		Choice:    choice,
		Meta: store.Meta{
			Model:        selection.Model,
			WorkspaceDir: options.workspace.GetDir(),
			Provider:     selection.Provider,
			Effort:       selection.Effort,
			IsFast:       selection.IsFast,
			ModelChoice:  &choice,
			Yolo:         options.isYolo,
		},
		Factory:      newChildFactory(options),
		PickName:     rand.IntN,
		EnsureParent: options.ensurePersisted,
		Workspace:    childWorkspace(options),
		ScratchNote:  childScratchNote(options),
		Concurrency:  options.settings.Subagent.Concurrency,
		Caps: func() caps.Set {
			if options.isYolo {
				return caps.Unconfined()
			}
			return options.parentCaps() & (caps.Read | caps.Shell)
		},
	})
	return preparedChildManager{manager: manager}, err
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
		paths := shell.Paths{
			Deny: options.settings.Sandbox.Deny,
			Exec: options.settings.Sandbox.Exec,
			Path: options.settings.Sandbox.Path,
		}
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
		"Report your findings in your final answer. Your parent may send you follow-ups after you answer."
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
			parentScratchView(child.Name)
		whereabouts = "/tmp is at " + child.Scratch + " on the host. HOME is " + home + "."
	default:
		circumstances = "Your workspace is read-only. " +
			"You have no shell execution, so bash refuses every command; read, search and list files instead."
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
