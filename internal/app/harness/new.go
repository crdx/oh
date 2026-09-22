package harness

import (
	"os"
	"time"

	"crdx.org/oh/internal/app/bar"
	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/conditions"
	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/cycle"
	"crdx.org/oh/internal/app/editor"
	"crdx.org/oh/internal/app/experimental"
	"crdx.org/oh/internal/app/metrics"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/pictures"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/record"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/terminal"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/jobs"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/ask"
	"crdx.org/oh/pkg/tool/middleware/truncate"
)

type Options struct {
	Agent           *agent.Agent
	Screen          *output.Screen
	Terminal        terminal.Terminal
	Metrics         metrics.Tracker
	Recorder        *record.Recorder
	Workspace       *work.Space
	Mode            *caps.Mode
	Conditions      *conditions.State
	EditorConfig    *editor.Config
	ToolOutputLimit *truncate.Limit
	Experimental    *experimental.Toggles
	Questions       *ask.Broker
	Keyboard        *os.File

	IsPrinting  bool
	IsYolo      bool
	IsSimulated bool

	ConfigObserver  *config.Observer
	PathGrants      *pathgrant.Grants
	HostToSandbox   *portgrant.HostToSandbox
	SandboxToHost   *portgrant.SandboxToHost
	Jobs            *jobs.Manager
	DoJobsWake      bool
	OpeningEvents   []agent.Event
	Pictures        pictures.Display
	OnFailure       func(failure error)
	OnQuestion      func(question ask.Question)
	SavePastedImage func(mediaType string, data []byte) (string, error)
}

func New(options Options) *App {
	return &App{
		agent:           options.Agent,
		screen:          options.Screen,
		terminal:        options.Terminal,
		metrics:         options.Metrics,
		recorder:        options.Recorder,
		workspace:       options.Workspace,
		mode:            options.Mode,
		conditions:      options.Conditions,
		editorConfig:    options.EditorConfig,
		toolOutputLimit: options.ToolOutputLimit,
		experimental:    options.Experimental,
		question:        questionState{broker: options.Questions},
		keyboard:        options.Keyboard,
		runMode: runMode{
			isPrinting:  options.IsPrinting,
			isYolo:      options.IsYolo,
			isSimulated: options.IsSimulated,
		},
		configObserver:  options.ConfigObserver,
		pathGrants:      options.PathGrants,
		hostToSandbox:   options.HostToSandbox,
		sandboxToHost:   options.SandboxToHost,
		jobs:            jobState{manager: options.Jobs, doesWake: options.DoJobsWake},
		openingEvents:   options.OpeningEvents,
		display:         displayState{pictures: options.Pictures},
		onFailure:       options.OnFailure,
		onQuestion:      options.OnQuestion,
		savePastedImage: options.SavePastedImage,
		startedAt:       util.WallClock(time.Now()),
	}
}

func (self *App) Begin(message string) cycle.Transition {
	return self.begin(message)
}

func (self *App) UseBar(
	options bar.Options,
	buildLayout func(segment.Registry) (segment.Layout, error),
) error {
	options.Sources = self.getBarSources()
	registry := bar.NewRegistry(options)

	layout, err := buildLayout(registry)
	if err != nil {
		return err
	}

	self.display.bar = bar.NewConfiguration(registry, layout)

	return nil
}

func (self *App) UseModelName(name string) {
	self.display.modelName = name
}

func (self *App) IsFocused() bool {
	return self.terminal.IsFocused()
}
