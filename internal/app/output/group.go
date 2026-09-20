package output

type Group int

const (
	NoticeGroup Group = iota
	ToolGroup
	AnswerGroup
	ReasoningGroup
)

const groupCount = int(ReasoningGroup) + 1
