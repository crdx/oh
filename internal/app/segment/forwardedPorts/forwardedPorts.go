package forwardedPorts

import (
	"strconv"

	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/segment/fit"
	"crdx.org/oh/internal/app/segment/taskNames"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/pkg/tasks"
)

type Routes struct {
	GetRoutes func() []portgrant.Route
	GetTasks  func() []tasks.Snapshot
	Hostname  string
}

type state struct {
	routes Routes
}

func New(routes Routes) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		if err := options.Read(&struct{}{}); err != nil {
			return nil, err
		}
		return state{routes: routes}, nil
	}
}

func (self state) Render(context segment.Context) string {
	ladder := self.Ladder(context)
	if len(ladder) == 0 {
		return ""
	}

	return ladder[0]
}

func (self state) Ladder(segment.Context) []string {
	return fit.Parts(self.getParts())
}

func (self state) getParts() []string {
	var parts []string
	for _, route := range self.routes.GetRoutes() {
		port := route.Port
		portName := strconv.Itoa(int(port))
		text := style.Normal(portName)
		if route.TaskName != "" {
			text = taskNames.RenderState(getTaskState(self.routes.GetTasks, route.TaskName), route.TaskName) + style.Dim(":"+portName)
		}
		parts = append(parts, link.RenderURL(text, portgrant.URL(self.routes.Hostname, port)))
	}

	return parts
}

func getTaskState(getTasks func() []tasks.Snapshot, name string) tasks.State {
	if getTasks != nil {
		for _, snapshot := range getTasks() {
			if snapshot.ID == name {
				return snapshot.State
			}
		}
	}
	return ""
}
