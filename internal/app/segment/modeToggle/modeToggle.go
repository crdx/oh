package modeToggle

import (
	"strings"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/style"
)

const gap = " "

type state struct {
	getGrantedCaps  func() caps.Set
	isPrefixPending func() bool
	getGroupStatus  func() caps.GroupStatus
}

func New(
	getGrantedCaps func() caps.Set,
	isPrefixPending func() bool,
	getGroupStatus ...func() caps.GroupStatus,
) segment.Factory {
	groups := func() caps.GroupStatus { return caps.GroupStatus{} }
	if len(getGroupStatus) > 0 && getGroupStatus[0] != nil {
		groups = getGroupStatus[0]
	}

	return func(segment.Options) (segment.Segment, error) {
		return state{
			getGrantedCaps:  getGrantedCaps,
			isPrefixPending: isPrefixPending,
			getGroupStatus:  groups,
		}, nil
	}
}

func (self state) Render(segment.Context) string {
	grantedCaps := self.getGrantedCaps()
	isPrefixPending := self.isPrefixPending()

	renderedMode := self.letter(caps.Read, true, style.Read, isPrefixPending) +
		self.letter(
			caps.Shell,
			grantedCaps.Has(caps.Shell),
			style.ExecWhenWritable(grantedCaps.CanChangeFiles()),
			isPrefixPending,
		) +
		self.letter(caps.Write, grantedCaps.Has(caps.Write), style.Write, isPrefixPending) +
		gap +
		self.letter(caps.Network, grantedCaps.Has(caps.Network), style.Network, isPrefixPending) +
		self.letter(caps.Git, grantedCaps.Has(caps.Git), style.Git, isPrefixPending) +
		self.letter(caps.Lookup, grantedCaps.Has(caps.Lookup), style.Lookup, isPrefixPending)

	groups := self.getGroupStatus()
	if groups.Flags == "" {
		return renderedMode
	}
	var groupLetters strings.Builder
	for _, flag := range groups.Flags {
		groupLetters.WriteString(self.groupLetter(string(flag), groups.Has(string(flag)), isPrefixPending))
	}
	return renderedMode + groupLetters.String()
}

func (self state) groupLetter(flag string, isGranted bool, isPrefixPending bool) string {
	paint := style.Info
	if !isGranted {
		paint = style.Dim
	}
	if isPrefixPending {
		return style.PendingPrefix(paint(flag))
	}
	return paint(flag)
}

func (self state) letter(caps caps.Set, isGranted bool, paint style.Style, isPrefixPending bool) string {
	if !isGranted {
		paint = style.Dim
	}

	if isPrefixPending {
		return style.PendingPrefix(paint(caps.Flag()))
	}

	return paint(caps.Flag())
}
