package harness

import (
	"testing"

	"crdx.org/oh/internal/app/commands"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/title"
)

func TestASessionStartIsRefusedBeforeTheSessionIsLeft(t *testing.T) {
	tools := []tool.Tool{title.New()}

	for name, test := range map[string]struct {
		start           commands.SessionStart
		isYoloInherited bool
		isRefused       bool
	}{
		"no options":                              {},
		"known capabilities":                      {start: commands.SessionStart{CapFlags: "rxw"}},
		"a custom tool group":                     {start: commands.SessionStart{CapFlags: "ra"}},
		"an unknown capability":                   {start: commands.SessionStart{CapFlags: "rz"}, isRefused: true},
		"confined capabilities beside --yolo":     {start: commands.SessionStart{CapFlags: "rx", IsYolo: true}, isRefused: true},
		"confined capabilities in a yolo session": {start: commands.SessionStart{CapFlags: "w"}, isYoloInherited: true, isRefused: true},
		"lookup beside --yolo":                    {start: commands.SessionStart{CapFlags: "l", IsYolo: true}},
		"a known tool":                            {start: commands.SessionStart{Tools: []string{title.Name}}},
		"an unknown tool":                         {start: commands.SessionStart{Tools: []string{"nope"}}, isRefused: true},
	} {
		t.Run(name, func(t *testing.T) {
			err := checkSessionStart(test.start, tools, "a", test.isYoloInherited)
			if isRefused := err != nil; isRefused != test.isRefused {
				t.Errorf("got %v, want refused %v", err, test.isRefused)
			}
		})
	}
}
