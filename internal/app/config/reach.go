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
	"caps":     ReachNextSession,
	"model":    ReachNextSession,
}

func ReachOf(setting string) Reach {
	table, _, _ := strings.Cut(setting, ".")
	return reaches[table]
}
