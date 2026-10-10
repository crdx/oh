package config

import "strings"

type Reach int

const (
	ReachLive Reach = iota
	ReachNextRun
	ReachNextSession
)

var reaches = map[string]Reach{
	"sandbox":  ReachNextRun,
	"skills":   ReachNextRun,
	"provider": ReachNextRun,
	"ports":    ReachNextRun,
	"debug":    ReachNextRun,
	"subagent": ReachNextRun,
	"defaults": ReachNextSession,
	"agent":    ReachNextSession,
	"tools":    ReachNextSession,
}

var liveExceptions = map[string]bool{
	"defaults.tool_output": true,
}

func ReachOf(setting string) Reach {
	if liveExceptions[setting] {
		return ReachLive
	}
	table, _, _ := strings.Cut(setting, ".")
	return reaches[table]
}
