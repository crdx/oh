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
	"defaults": ReachNextSession,
	"agent":    ReachNextSession,
	"tools":    ReachNextSession,
}

var settingReaches = map[string]Reach{
	"defaults.tool_output":       ReachLive,
	"experimental.reply_command": ReachNextRun,
	"experimental.wait_tool":     ReachNextRun,
}

func ReachOf(setting string) Reach {
	if reach, isListed := settingReaches[setting]; isListed {
		return reach
	}
	table, _, _ := strings.Cut(setting, ".")
	return reaches[table]
}
