package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"google.golang.org/genai"
)

func main() {
	var (
		namespace    string
		eksVersion   string
		addonsFilter string
		apiKey       string
		model        string
	)

	rootCmd := &cobra.Command{
		Use:   "kaddons",
		Short: "Kubernetes addon compatibility checker",
		Long:           "Discovers addons installed in a Kubernetes cluster and checks their compatibility with the cluster's Kubernetes version using Gemini AI.",
		SilenceUsage:   true,
		SilenceErrors:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
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

			return runAgent(ctx, client, model, namespace, eksVersion, addonsFilter)
		},
	}

	rootCmd.Flags().StringVar(&namespace, "namespace", "", "Kubernetes namespace filter (empty for all namespaces)")
	rootCmd.Flags().StringVar(&eksVersion, "eks", "", "EKS/K8s version override (e.g. 1.29)")
	rootCmd.Flags().StringVar(&addonsFilter, "addons", "", "Comma-separated addon name filter")
	rootCmd.Flags().StringVar(&apiKey, "api-key", "", "Gemini API key (overrides GEMINI_API_KEY env)")
	rootCmd.Flags().StringVar(&model, "model", "gemini-3-flash", "Gemini model to use")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}
