package permission_test

import (
	"testing"

	"crdx.org/oh/internal/app/permission"
)

func TestARuleIsOneOfTheOfferedWords(t *testing.T) {
	for name, test := range map[string]struct {
		value   string
		want    permission.Rule
		wantErr bool
	}{
		"ask":       {value: "ask", want: permission.Ask},
		"allow":     {value: "allow", want: permission.Allow},
		"deny":      {value: "deny", wantErr: true},
		"nothing":   {value: "", wantErr: true},
		"shouting":  {value: "ASK", wantErr: true},
		"a sneeze":  {value: "aksh", wantErr: true},
		"a mistake": {value: "allows", wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			rule, err := permission.ParseRule(test.value)
			if test.wantErr {
				if err == nil {
					t.Fatalf("ParseRule(%q) was accepted as %q", test.value, rule)
				}

				return
			}
			if err != nil {
				t.Fatalf("ParseRule(%q) failed: %v", test.value, err)
			}
			if rule != test.want {
				t.Errorf("got %q, want %q", rule, test.want)
			}
		})
	}
}

func TestAReplacedSetIsTheOneEveryReaderSees(t *testing.T) {
	live := permission.New(permission.Set{
		Network: permission.Ask,
		Lookup:  permission.Allow,
		Fetch:   permission.Allow,
	})

	if live.Network() != permission.Ask || live.Lookup() != permission.Allow {
		t.Fatalf("got %+v, want the set it was given", live.Get())
	}

	live.Replace(permission.Set{
		Network: permission.Allow,
		Lookup:  permission.Ask,
		Fetch:   permission.Ask,
	})

	if live.Network() != permission.Allow {
		t.Errorf("got network %q, want the replacement", live.Network())
	}
	if live.Lookup() != permission.Ask || live.Fetch() != permission.Ask {
		t.Errorf("got %+v, want the replacement", live.Get())
	}
}
