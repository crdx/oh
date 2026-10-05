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
	isUnconfined    bool
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

	return factory(state{
		getGrantedCaps:  getGrantedCaps,
		isPrefixPending: isPrefixPending,
		getGroupStatus:  groups,
	})
}

func NewUnconfined(
	getGrantedCaps func() caps.Set,
	isPrefixPending func() bool,
	getGroupStatus func() caps.GroupStatus,
) segment.Factory {
	return factory(state{
		getGrantedCaps:  getGrantedCaps,
		isPrefixPending: isPrefixPending,
		getGroupStatus:  getGroupStatus,
		isUnconfined:    true,
	})
}

func factory(segmentState state) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return segmentState, nil
	}
}

func (self state) Render(segment.Context) string {
	grantedCaps := self.getGrantedCaps()
	isPrefixPending := self.isPrefixPending()

	if self.isUnconfined {
		return self.letter(caps.Lookup, grantedCaps.Has(caps.Lookup), style.Lookup, isPrefixPending) +
			self.groupLetters(isPrefixPending)
	}

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

	return renderedMode + self.groupLetters(isPrefixPending)
}

func (self state) groupLetters(isPrefixPending bool) string {
	groups := self.getGroupStatus()
	var groupLetters strings.Builder
	for _, flag := range groups.Flags {
		groupLetters.WriteString(self.groupLetter(string(flag), groups.Has(string(flag)), isPrefixPending))
	}
	return groupLetters.String()
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
