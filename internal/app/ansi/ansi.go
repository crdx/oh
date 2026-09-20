package ansi

import "strconv"

const (
	HideCursor    = "\x1b[?25l"
	ShowCursor    = "\x1b[?25h"
	BarCursor     = "\x1b[5 q"
	DefaultCursor = "\x1b[0 q"
)

const (
	EnterAltScreen = "\x1b[?1049h"
	LeaveAltScreen = "\x1b[?1049l"
)

const (
	Home            = "\x1b[H"
	EraseLine       = "\x1b[K"
	EraseRow        = "\r\x1b[2K"
	EraseBelow      = "\x1b[J"
	EraseScreen     = "\x1b[H\x1b[2J"
	EraseScrollback = "\x1b[3J"
)

const (
	BeginFrame = "\x1b[?2026h"
	EndFrame   = "\x1b[?2026l"
)

const (
	NoAutoWrap = "\x1b[?7l"
	AutoWrap   = "\x1b[?7h"
)

const (
	PushTitle = "\x1b[22;0t"
	PopTitle  = "\x1b[23;0t"
)

const Reset = "\x1b[0m"

func Up(rows int) string {
	return "\x1b[" + strconv.Itoa(rows) + "A"
}

func Right(cells int) string {
	return "\x1b[" + strconv.Itoa(cells) + "C"
}
