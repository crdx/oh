package shell

import (
	"fmt"
	"strings"
)

type Access uint8

const (
	ReadAccess Access = 1 << iota
	ExecAccess
	WriteAccess
)

var accessMap = []struct {
	grantedAccess Access
	flag          string
}{
	{ReadAccess, "r"},
	{ExecAccess, "x"},
	{WriteAccess, "w"},
}

var AllAccessFlags = (ReadAccess | ExecAccess | WriteAccess).Flags()

func (self Access) Has(want Access) bool { return self&want == want }

func IsAccess(access Access) bool {
	return access.Has(ReadAccess) && access&^(ReadAccess|ExecAccess|WriteAccess) == 0
}

func (self Access) Flags() string {
	var flags strings.Builder

	for _, right := range accessMap {
		if self.Has(right.grantedAccess) {
			flags.WriteString(right.flag)
		}
	}

	return flags.String()
}

func (self Access) Describe() string {
	var names []string
	for _, right := range accessMap {
		if !self.Has(right.grantedAccess) {
			continue
		}
		names = append(names, accessName(right.grantedAccess))
	}

	switch len(names) {
	case 1:
		return names[0] + "-only"
	case 2:
		return names[0] + " and " + names[1]
	}

	return strings.Join(names[:len(names)-1], ", ") + ", and " + names[len(names)-1]
}

func accessName(access Access) string {
	switch access {
	case ReadAccess:
		return "read"
	case ExecAccess:
		return "execute"
	case WriteAccess:
		return "write"
	}

	return ""
}

func ParseAccess(flags string) (Access, error) {
	grantedAccess := ReadAccess

	for _, flag := range flags {
		right, found := namedAccess(string(flag))
		if !found {
			return 0, fmt.Errorf(
				"unknown access flag %q — must be one of %q",
				string(flag),
				AllAccessFlags,
			)
		}

		grantedAccess |= right
	}

	return grantedAccess, nil
}

func namedAccess(flag string) (Access, bool) {
	for _, right := range accessMap {
		if right.flag == flag {
			return right.grantedAccess, true
		}
	}

	return 0, false
}
