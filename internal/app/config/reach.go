package config

import "strings"

type Reach int

const (
	ReachLive Reach = iota
	ReachNextRun
	ReachNextSession
)

var reaches = map[string]Reach{
	"grants":   ReachNextRun,
	"skill":    ReachNextRun,
	"provider": ReachNextRun,
	"debug":    ReachNextRun,
	"default":  ReachNextSession,
	"agent":    ReachNextSession,
	"tool":     ReachNextSession,
}

var settingReaches = map[string]Reach{
	"agent.forward_hostname": ReachNextRun,
	"agent.resync":           ReachLive,
	"debug.wire":             ReachNextSession,
	"default.tool_output":    ReachLive,
	"tool.bash.host_network": ReachLive,
	"tool.fetch.permission":  ReachLive,
	"tool.lookup.permission": ReachLive,
}

func ReachOf(setting string) Reach {
	for candidate := setting; candidate != ""; candidate = parentSetting(candidate) {
		if reach, isListed := settingReaches[candidate]; isListed {
			return reach
		}
	}
	table, _, _ := strings.Cut(setting, ".")
	return reaches[table]
}

func parentSetting(setting string) string {
	end := strings.LastIndexByte(setting, '.')
	if end < 0 {
		return ""
	}
	return setting[:end]
}
