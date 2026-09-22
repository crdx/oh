package jobs

import (
	"testing"

	"crdx.org/oh/internal/sandbox"
)

func TestMain(m *testing.M) {
	sandbox.Init()
	m.Run()
}
