package config

import (
	"reflect"
	"testing"
)

func TestReachOfNamesWhenASettingLands(t *testing.T) {
	cases := map[string]Reach{
		"input.speed_dial":            ReachLive,
		"ui.theme.dim":                ReachLive,
		"editor.command":              ReachLive,
		"snippets.fix":                ReachLive,
		"sandbox.deny":                ReachNextRun,
		"sandbox.read":                ReachNextRun,
		"skills.include":              ReachNextRun,
		"provider.ollama":             ReachNextRun,
		"ports.hostname":              ReachNextRun,
		"debug.stalls":                ReachNextRun,
		"debug.cpu_profile":           ReachNextRun,
		"debug.heap_profile":          ReachNextRun,
		"subagent.model":              ReachLive,
		"subagent.round_robin":        ReachLive,
		"subagent.concurrency":        ReachLive,
		"experimental.reply_command":  ReachNextRun,
		"experimental.wait_tool":      ReachNextRun,
		"experimental.shared_history": ReachLive,
		"ui.currency":                 ReachLive,
		"defaults.caps":               ReachNextSession,
		"agent.model":                 ReachNextSession,
		"agent.round_robin":           ReachNextSession,
		"defaults.effort":             ReachNextSession,
		"sandbox":                     ReachNextRun,
		"":                            ReachLive,
	}

	for setting, want := range cases {
		if got := ReachOf(setting); got != want {
			t.Errorf("%q reaches %d, want %d", setting, got, want)
		}
	}
}

func TestEveryConfigTableSaysWhenItLands(t *testing.T) {
	liveTables := map[string]bool{
		"version":      true,
		"editor":       true,
		"input":        true,
		"snippets":     true,
		"bar":          true,
		"ui":           true,
		"tool":         true,
		"permissions":  true,
		"experimental": true,
		"subagent":     true,
	}

	for field := range reflect.TypeFor[Config]().Fields() {
		table, isTagged := field.Tag.Lookup("toml")
		if !isTagged {
			continue
		}
		if _, isWaiting := reaches[table]; isWaiting {
			continue
		}
		if !liveTables[table] {
			t.Errorf("[%s] says nothing about when it lands: add it to reaches or to this test", table)
		}
	}
}
