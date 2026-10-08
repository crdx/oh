package sandbox_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"crdx.org/oh/pkg/sandbox"
)

func TestDirectRefusesAnUnconfinedPolicy(t *testing.T) {
	directory := t.TempDir()
	outside := filepath.Join(directory, "unconfined")
	policy := sandbox.Policy{Yolo: true}

	if _, err := sandbox.Direct().Run(t.Context(), directory, "touch "+outside, policy); err == nil {
		t.Fatal("Direct ran an unconfined command")
	}
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("Direct wrote outside the sandbox")
	}

	if _, err := sandbox.Direct().Start(t.Context(), directory, "true", policy, &bytes.Buffer{}); err == nil {
		t.Fatal("Direct started an unconfined command")
	}
}

func TestDerivedPoliciesDoNotShareMutableFields(t *testing.T) {
	base := sandbox.Policy{
		Read:   []string{"/original"},
		Write:  []string{"/writable"},
		SetEnv: map[string]string{"A": "original"},
	}
	derived := base.WithRead("/new")
	base.Write[0] = "/changed"
	base.SetEnv["A"] = "changed"

	if derived.Write[0] != "/writable" || derived.SetEnv["A"] != "original" {
		t.Errorf("WithRead retained mutable fields from its source: %+v", derived)
	}
}

func TestPolicyCloneSeparatesMutableFields(t *testing.T) {
	base := sandbox.Policy{Env: []string{"PATH"}, SetEnv: map[string]string{"HOME": "/home"}}
	copyPolicy := base.Clone()
	base.Env[0] = "TOKEN"
	base.SetEnv["HOME"] = "/other"
	if copyPolicy.Env[0] != "PATH" || copyPolicy.SetEnv["HOME"] != "/home" {
		t.Errorf("Clone retained the source's mutable fields: %+v", copyPolicy)
	}
}

func TestDirectArgvRefusesAnUnconfinedPolicy(t *testing.T) {
	runner := sandbox.DirectArgv()
	policy := sandbox.Policy{Yolo: true}
	if _, err := runner.RunArgv(t.Context(), t.TempDir(), []string{"/usr/bin/true"}, policy); err == nil {
		t.Fatal("DirectArgv ran unconfined")
	}
	if _, err := runner.StartArgv(t.Context(), t.TempDir(), []string{"/usr/bin/true"}, policy, &bytes.Buffer{}); err == nil {
		t.Fatal("DirectArgv started unconfined")
	}
}
