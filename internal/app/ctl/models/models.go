package models

import (
	"os"

	"crdx.org/duckopt/v2"

	"crdx.org/oh/internal/app/backend"
	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/model"
)

const usage = `oh --ctl models — print the qualified names of usable models

Usage:
    $0 --ctl models

Options:
    -h, --help    Show this help
`

type inputOpts struct {
	IsControlling bool `docopt:"--ctl"`
	Models        bool `docopt:"models"`
}

func Run() error {
	duckopt.MustBind[inputOpts](usage, "$0")
	endpoints := backend.EndpointSettings{OverrideURL: os.Getenv(backend.EndpointVariable)}
	return model.ListNames(os.Stdout, location.GetModelCachePath(), func(providerName string) bool {
		return backend.IsAvailable(providerName, endpoints)
	})
}
