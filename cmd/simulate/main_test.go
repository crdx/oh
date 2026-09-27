package main

import (
	"errors"
	"testing"
)

func TestTheOptionsNameAScenarioAndWhereToListen(t *testing.T) {
	for name, testCase := range map[string]struct {
		arguments []string
		want      options
	}{
		"a scenario alone":       {arguments: []string{"-s", "turns.yml"}, want: options{scenario: "turns.yml", address: "localhost:8080"}},
		"a scenario and address": {arguments: []string{"--scenario", "turns.yml", "--address", ":9000"}, want: options{scenario: "turns.yml", address: ":9000"}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := parse(testCase.arguments)
			if err != nil || got != testCase.want {
				t.Errorf("got %+v, %v; want %+v", got, err, testCase.want)
			}
		})
	}
}

func TestOptionsThatCannotBeUsedAreRefused(t *testing.T) {
	for name, arguments := range map[string][]string{
		"no scenario":              {},
		"a scenario with no path":  {"--scenario"},
		"an address with no value": {"-s", "turns.yml", "-a"},
		"an option nobody knows":   {"--loud"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parse(arguments); err == nil {
				t.Error("the options were accepted")
			}
		})
	}
}

func TestHelpIsAskedForRatherThanRefused(t *testing.T) {
	if _, err := parse([]string{"-s", "turns.yml", "--help"}); !errors.Is(err, errHelp) {
		t.Errorf("got %v, want help", err)
	}
}

func TestALoopingScenarioSaysSo(t *testing.T) {
	if loops(true) != ", looping" || loops(false) != "" {
		t.Errorf("got %q and %q", loops(true), loops(false))
	}
}
