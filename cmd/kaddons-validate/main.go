package main

import (
	"fmt"
	"os"

	"github.com/qbandev/kaddons/internal/validate"
	"github.com/spf13/cobra"
)

var (
	linksOnly  bool
	matrixOnly bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "kaddons-validate",
		Short: "Validate kaddons addon database",
		Long: `Validate the kaddons addon database by checking URL reachability 
and verifying that compatibility matrix URLs contain actual K8s version data.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return validate.Run(linksOnly, matrixOnly)
		},
		SilenceUsage: true,
	}

	rootCmd.Flags().BoolVar(&linksOnly, "links", false, "Only check URL reachability (skip matrix content validation)")
	rootCmd.Flags().BoolVar(&matrixOnly, "matrix", false, "Only validate compatibility matrix URLs")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
