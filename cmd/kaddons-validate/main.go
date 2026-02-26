package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/qbandev/kaddons/internal/validate"
)

func main() {
	linksOnly := flag.Bool("links", false, "Only check URL reachability (skip content validation)")
	matrixOnly := flag.Bool("matrix", false, "Only check compatibility matrix content (skip non-matrix URLs)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: kaddons-validate [--links | --matrix]\n\n")
		fmt.Fprintf(os.Stderr, "Validates addon database URLs for reachability and checks compatibility\n")
		fmt.Fprintf(os.Stderr, "matrix pages for structured Kubernetes version data.\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *linksOnly && *matrixOnly {
		fmt.Fprintf(os.Stderr, "Error: --links and --matrix are mutually exclusive\n")
		os.Exit(2)
	}

	if err := validate.Run(*linksOnly, *matrixOnly); err != nil {
		if errors.Is(err, validate.ErrValidationFailed) {
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(2)
	}
}
