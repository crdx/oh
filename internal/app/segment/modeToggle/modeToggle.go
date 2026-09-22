package modeToggle

import (
	"strings"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/style"
)

const gap = " "

type state struct {
	getGrantedCaps    func() caps.Set
	getChangeableCaps func() caps.Set
	isPrefixPending   func() bool
}

func New(
	getGrantedCaps func() caps.Set,
	getChangeableCaps func() caps.Set,
	isPrefixPending func() bool,
) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{
			getGrantedCaps:    getGrantedCaps,
			getChangeableCaps: getChangeableCaps,
			isPrefixPending:   isPrefixPending,
		}, nil
	}
}

func (self state) Render(segment.Context) string {
	grantedCaps := self.getGrantedCaps()
	changeableCaps := self.changeable()
	isPrefixPending := self.isPrefixPending()

	files := self.letter(caps.Read, changeableCaps, true, style.Read, isPrefixPending) +
		self.letter(
			caps.Shell,
			changeableCaps,
			grantedCaps.Has(caps.Shell),
			style.ExecWhenWritable(grantedCaps.CanChangeFiles()),
			isPrefixPending,
		) +
		self.letter(caps.Write, changeableCaps, grantedCaps.Has(caps.Write), style.Write, isPrefixPending)

	reach := self.letter(caps.Network, changeableCaps, grantedCaps.Has(caps.Network), style.Network, isPrefixPending) +
		self.letter(caps.Git, changeableCaps, grantedCaps.Has(caps.Git), style.Git, isPrefixPending) +
		self.letter(caps.Lookup, changeableCaps, grantedCaps.Has(caps.Lookup), style.Lookup, isPrefixPending)

	return strings.Join(written(files, reach), gap)
}

func written(groups ...string) []string {
	present := make([]string, 0, len(groups))
	for _, group := range groups {
		if group != "" {
			present = append(present, group)
		}
	}

	return present
}

func (self state) changeable() caps.Set {
	if self.getChangeableCaps == nil {
		return caps.All()
	}

	return self.getChangeableCaps()
}

func (self state) letter(
	whichCaps caps.Set,
	changeableCaps caps.Set,
	isGranted bool,
	paint style.Style,
	isPrefixPending bool,
) string {
	if !changeableCaps.Has(whichCaps) {
		return ""
	}

	if !isGranted {
		paint = style.Dim
	}

	if isPrefixPending {
		return style.PendingPrefix(paint(whichCaps.Flag()))
	}

	return paint(whichCaps.Flag())
}
