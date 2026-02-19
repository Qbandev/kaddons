package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"google.golang.org/genai"
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
	)

	rootCmd := &cobra.Command{
		Use:     "kaddons",
		Version: version,
		Short:   "Kubernetes addon compatibility checker",
		Long:           "Discovers addons installed in a Kubernetes cluster and checks their compatibility with the cluster's Kubernetes version using Gemini AI.",
		SilenceUsage:   true,
		SilenceErrors:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if output != "json" && output != "table" {
				return fmt.Errorf("invalid output format %q: must be json or table", output)
			}

			key := apiKey
			if key == "" {
				key = os.Getenv("GEMINI_API_KEY")
			}
			if key == "" {
				return fmt.Errorf("Gemini API key is required: set GEMINI_API_KEY env var or use --api-key flag")
			}

			ctx := context.Background()
			client, err := genai.NewClient(ctx, &genai.ClientConfig{
				APIKey:  key,
				Backend: genai.BackendGeminiAPI,
			})
			if err != nil {
				return fmt.Errorf("creating Gemini client: %w", err)
			}

			return runAgent(ctx, client, model, namespace, k8sVersion, addonsFilter, output)
		},
	}

	rootCmd.Flags().StringVar(&namespace, "namespace", "", "Kubernetes namespace filter (empty for all namespaces)")
	rootCmd.Flags().StringVar(&k8sVersion, "k8s-version", "", "Kubernetes version override (e.g. 1.30)")
	rootCmd.Flags().StringVar(&addonsFilter, "addons", "", "Comma-separated addon name filter")
	rootCmd.Flags().StringVar(&apiKey, "api-key", "", "Gemini API key (overrides GEMINI_API_KEY env)")
	rootCmd.Flags().StringVar(&model, "model", "gemini-3-flash-preview", "Gemini model to use")
	rootCmd.Flags().StringVarP(&output, "output", "o", "json", "Output format: json or table")

	rootCmd.AddCommand(newLinkcheckCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}
