package usage

import (
	"errors"

	"crdx.org/oh/internal/req"
)

func FailureStatus(err error) (int, bool) {
	if refusedRequest, ok := errors.AsType[*req.StatusError](err); ok {
		return refusedRequest.Status, true
	}

	return 0, false
}
