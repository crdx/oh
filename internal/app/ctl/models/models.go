package models

import (
	"os"

	"crdx.org/duckopt/v2"
	"crdx.org/oh/internal/app/backend"
	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/model"
)

const Usage = `oh --ctl models — print the qualified names of usable models

Usage:
    $0 --ctl models [-I]

Options:
    -I, --ignored    Include ignored models
    -h, --help       Show this help
`

type inputOpts struct {
	IsControlling    bool `docopt:"--ctl"`
	Models           bool `docopt:"models"`
	IsShowingIgnored bool `docopt:"--ignored"`
}

func Run() error {
	options := duckopt.MustBind[inputOpts](Usage, "$0")
	endpoints := backend.EndpointSettings{OverrideURL: os.Getenv(backend.EndpointVariable)}

	var ignoredChoices []model.Choice
	if options.IsShowingIgnored {
		var err error
		ignoredChoices, err = model.IgnoredChoices(location.GetModelCachePath(), location.GetSeenModelsPath())
		if err != nil {
			return err
		}
	}

	return model.ListNames(os.Stdout, location.GetModelCachePath(), ignoredChoices, func(providerName string) bool {
		return backend.IsAvailable(providerName, endpoints)
	})
}
