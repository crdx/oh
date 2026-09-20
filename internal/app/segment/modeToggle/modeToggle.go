package modeToggle

import (
	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/style"
)

const gap = " "

type state struct {
	getGrantedCaps  func() caps.Set
	isPrefixPending func() bool
}

func New(getGrantedCaps func() caps.Set, isPrefixPending func() bool) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{
			getGrantedCaps:  getGrantedCaps,
			isPrefixPending: isPrefixPending,
		}, nil
	}
}

func (self state) Render(segment.Context) string {
	grantedCaps := self.getGrantedCaps()
	isPrefixPending := self.isPrefixPending()

	return self.letter(caps.Read, true, style.Read, isPrefixPending) +
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
