package harness

import (
	"crdx.org/oh/internal/app/experimental"
	"crdx.org/oh/internal/req"
)

func streamTimeoutsFrom(toggles *experimental.Toggles) func() req.StreamTimeouts {
	return func() req.StreamTimeouts {
		return req.StreamTimeouts{
			Response: toggles.GetDuration(experimental.ResponseTimeout, req.DefaultStreamTimeout),
			Idle:     toggles.GetDuration(experimental.StreamIdleTimeout, req.DefaultStreamTimeout),
		}
	}
}
