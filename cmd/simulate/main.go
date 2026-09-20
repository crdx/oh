package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"time"

	"crdx.org/col"
	"crdx.org/oh/internal/sim"
)

const usage = `simulate — stand in for every provider endpoint at once

Usage:
    simulate --scenario <path> [--address <address>]
    simulate --help

Options:
    -s, --scenario <path>      The scenario
    -a, --address <address>    Where to listen [default: localhost:8080]
    -h, --help                 Show this help

One scenario is played through whichever API a request arrives in, so the address printed for a
provider is what OH_ENDPOINT_URL wants for a conversation with that provider. The model listing and
a stand-in models.dev registry are served alongside, so oh -u works here too.
`

const readTimeout = 30 * time.Second

func main() {
	col.Init()

	if err := run(); err != nil {
		if errors.Is(err, errHelp) {
			fmt.Print(usage)
			return
		}

		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var errHelp = errors.New("help")

type options struct {
	scenario string
	address  string
}

func parse(arguments []string) (options, error) {
	self := options{address: "localhost:8080"}

	for i := 0; i < len(arguments); i++ {
		argument := arguments[i]

		wants := func() bool { return i+1 < len(arguments) }

		switch argument {
		case "-s", "--scenario":
			if !wants() {
				return self, fmt.Errorf("%s wants a path", argument)
			}

			i++
			self.scenario = arguments[i]

		case "-a", "--address":
			if !wants() {
				return self, fmt.Errorf("%s wants an address", argument)
			}

			i++
			self.address = arguments[i]

		case "-h", "--help":
			return self, errHelp

		default:
			return self, fmt.Errorf("no such option: %s", argument)
		}
	}

	if self.scenario == "" {
		return self, errors.New("a scenario is needed")
	}

	return self, nil
}

func run() error {
	options, err := parse(os.Args[1:])
	if err != nil {
		return err
	}

	scenario, err := sim.Read(options.scenario)
	if err != nil {
		return err
	}

	endpoint := sim.New(scenario)

	server := &http.Server{
		Addr:              options.address,
		Handler:           endpoint,
		ReadHeaderTimeout: readTimeout,
	}

	fmt.Printf(
		"%s %s, %d turns%s\n",
		col.Green("answering as"),
		col.White(scenario.Model),
		len(scenario.Turns),
		loops(scenario.Loop),
	)

	announce(endpoint, "http://"+options.address, scenario.Model)

	return server.ListenAndServe()
}

func announce(endpoint *sim.Endpoint, base string, model string) {
	addresses := endpoint.Addresses(base)

	formats := make([]string, 0, len(addresses))
	for format := range addresses {
		formats = append(formats, format)
	}

	slices.Sort(formats)

	fmt.Printf("\n%s\n\n", col.Green("answering these wire formats:"))

	for _, format := range formats {
		fmt.Printf(
			"  %s %s\n", col.Green(fmt.Sprintf("%-12s", format)), col.White(addresses[format]),
		)
	}

	fmt.Printf("\n%s\n\n", col.Green("point a provider at the address for the format it speaks:"))
	fmt.Printf("  %s\n", col.White("export OH_ENDPOINT_URL=<one of the above>"))
	fmt.Printf("  %s\n", col.White("oh -u"))
	fmt.Printf("  %s\n\n", col.White("oh -m <provider>/"+model+"@high"))
}

func loops(isLooping bool) string {
	if isLooping {
		return ", looping"
	}

	return ""
}
