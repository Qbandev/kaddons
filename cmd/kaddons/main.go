package main

import (
	"context"
	"fmt"
	"os"

	"github.com/qbandev/kaddons/internal/agent"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	var (
		namespace    string
		k8sVersion   string
		addonsFilter string
		apiKey       string
		model        string
		output       string
		outputPath   string
	)

	rootCmd := &cobra.Command{
		Use:           "kaddons",
		Version:       fmt.Sprintf("%s (commit: %s, date: %s)", version, commit, date),
		Short:         "Kubernetes addon compatibility checker",
		Long:          "Discovers addons installed in a Kubernetes cluster and checks their compatibility with the cluster's Kubernetes version using Gemini AI.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if output != "json" && output != "html" {
				return fmt.Errorf("invalid output format %q: must be json or html", output)
			}

			key := apiKey
			if key == "" {
				key = os.Getenv("GEMINI_API_KEY")
			}

			ctx := context.Background()
			return agent.Run(ctx, key, model, namespace, k8sVersion, addonsFilter, output, outputPath)
		},
	}

	rootCmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace filter (empty for all namespaces)")
	rootCmd.Flags().StringVarP(&k8sVersion, "cluster", "c", "", "Kubernetes version override (e.g. 1.30)")
	rootCmd.Flags().StringVarP(&addonsFilter, "addons", "a", "", "Comma-separated addon name filter")
	rootCmd.Flags().StringVarP(&apiKey, "key", "k", "", "Gemini API key (overrides GEMINI_API_KEY env, prefer env var to avoid exposure in process listings)")
	rootCmd.Flags().StringVarP(&model, "model", "m", "gemini-3-flash-preview", "Gemini model to use")
	rootCmd.Flags().StringVarP(&output, "output", "o", "json", "Output format: json or html")
	rootCmd.Flags().StringVar(&outputPath, "output-path", "./kaddons-report.html", "Output file path when --output=html")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}
