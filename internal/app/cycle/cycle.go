package cycle

type TransitionKind int

const (
	Quit TransitionKind = iota
	NewSession
	ResumeSession
	Restart
)

type Transition struct {
	Kind      TransitionKind
	Arguments []string
}

type Session struct {
	Name         string
	ID           string
	Directory    string
	WorkspaceDir string
	Provider     string
	Model        string
	Effort       string
}
