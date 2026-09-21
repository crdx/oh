package output

type Group int

const (
	NoticeGroup Group = iota
	ToolGroup
	AnswerGroup
	ReasoningGroup
	PanelGroup
)

const groupCount = int(PanelGroup) + 1
