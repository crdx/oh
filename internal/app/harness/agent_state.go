package harness

import (
	"context"
	"io"
	"slices"
	"time"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/drops"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/pictures"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/skill"
	"crdx.org/oh/internal/app/toolset"
	"crdx.org/oh/internal/app/usage"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/tool/middleware/truncate"
	"crdx.org/oh/pkg/toolbox"
)

type agentOptions struct {
	workspace        *work.Space
	mode             *caps.Mode
	paths            shell.Paths
	isUnconfined     bool
	roots            shell.Roots
	home             *file.Root
	homeCache        *file.Root
	scratch          *file.Root
	parent           shell.Parent
	skills           []skill.Skill
	sessionDirectory string
	ensurePersisted  func() error
	outputLimit      *truncate.Limit
}

type agentFeatures struct {
	beforeShell []tool.Tool
	afterShell  []tool.Tool
	custom      []tool.Tool
}

type agentState struct {
	options agentOptions
	files   *file.Root
	access  *shell.PathAccess
	drops   *drops.Keeper
	closers []func()
}

func discoverSkills(workspaceDirectory string, globalDirectories []string, exclusions []string, warnings io.Writer) ([]skill.Skill, error) {
	skills, err := skill.Discover(workspaceDirectory, globalDirectories, warnings)
	if err != nil {
		return nil, err
	}

	return skill.ExcludeGlobal(skills, exclusions), nil
}

func guardUsage(
	ctx context.Context,
	provider usage.StatefulProvider,
	choice model.Choice,
	selection model.Selection,
	cachePath string,
) usage.StatefulProvider {
	return usage.Guard(ctx, provider, usage.GuardSettings{
		ProviderName:        model.ProviderName(selection.Provider),
		ModelName:           selection.Model,
		CachePath:           cachePath,
		Now:                 time.Now,
		IsQuotaExempt:       choice.IsGoQuotaExempt(),
		CanRetryLimitedSend: selection.Provider == model.OpencodeGoProvider,
	})
}

func buildAgentState(options agentOptions) (*agentState, error) {
	state := &agentState{options: options, files: file.New(options.workspace.GetRoot(), caps.RefuseWrite(options.mode))}
	state.files.Mount(options.roots.HomeDirectory, options.home)
	state.closeRoots(options.home)

	access, err := shell.NewPathAccessForMode(state.files, options.mode, options.paths, options.isUnconfined)
	if err != nil {
		state.Close()
		return nil, err
	}
	state.access = access
	state.closers = append(state.closers, access.Close)

	err = shell.PrepareHomeMappings(
		options.workspace.GetDir(), options.roots, options.paths, options.mode.Current(),
	)
	if err != nil {
		state.Close()
		return nil, err
	}
	if options.homeCache != nil {
		state.files.Mount(shell.HomeCacheDirectory(options.roots.HomeDirectory), options.homeCache)
		state.closeRoots(options.homeCache)
	}
	state.closeRoots(shell.MountScratch(state.files, options.roots.ScratchDirectory, options.scratch, options.isUnconfined)...)
	state.closeRoots(skill.MountGlobalSkills(state.files, options.skills)...)
	access.ShareSkills(skill.GlobalDirectories(options.skills))

	dropKeeper, err := drops.Open(state.files, options.sessionDirectory, options.ensurePersisted)
	if err != nil {
		state.Close()
		return nil, err
	}
	state.drops = dropKeeper
	state.closers = append(state.closers, func() { _ = dropKeeper.Close() })

	return state, nil
}

func (self *agentState) Close() {
	for _, closer := range slices.Backward(self.closers) {
		closer()
	}
	self.closers = nil
}

func (self *agentState) shell(
	approveNetwork func(ctx context.Context, command string, intent string) error,
	runner sandbox.Runner,
) tool.Tool {
	return shell.New(
		self.options.workspace.GetDir(),
		self.options.roots,
		self.options.parent,
		self.access,
		self.options.mode,
		self.files,
		self.options.isUnconfined,
		approveNetwork,
		runner,
	)
}

func (self *agentState) tools(shellTool tool.Tool, features agentFeatures) ([]tool.Tool, error) {
	tools := toolbox.RummageWithRunner(self.files, file.NewSnapshots(), grepRunner(self.options.isUnconfined))
	tools = append(tools, features.beforeShell...)
	tools = append(tools, shellTool)
	tools = append(tools, features.afterShell...)

	return self.capped(tools, features.custom)
}

func (self *agentState) capped(tools []tool.Tool, custom []tool.Tool) ([]tool.Tool, error) {
	tools, err := toolset.Combine(tools, custom)
	if err != nil {
		return nil, err
	}

	return truncate.Tools(tools, self.options.outputLimit, self.drops.SaveOutput), nil
}

func (self *agentState) storePictures(worker *agent.Agent) {
	worker.StorePicturesWith(func(picture tool.Image) *agent.Picture {
		reference, err := pictures.Store(self.options.sessionDirectory, self.options.ensurePersisted, picture)
		if err != nil {
			return nil
		}

		return reference
	})
}

func (self *agentState) closeRoots(roots ...*file.Root) {
	for _, root := range roots {
		self.closers = append(self.closers, func() { _ = root.Close() })
	}
}
