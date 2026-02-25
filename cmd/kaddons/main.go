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

namespace         string
k8sVersion        string
addonsFilter      string
apiKey            string
model             string
outputFormat      string
showVersion       bool
)

func main() {
rootCmd := &cobra.Command{
Use:   "kaddons",
Short: "Kubernetes addon compatibility checker",
Long: `kaddons discovers addons running in your Kubernetes cluster and uses Gemini AI 
to determine whether each addon is compatible with the cluster's Kubernetes version.`,
RunE: func(cmd *cobra.Command, args []string) error {
if showVersion {
fmt.Printf("kaddons %s (commit: %s, built: %s)\n", version, commit, date)
return nil
}

key := apiKey
if key == "" {
key = os.Getenv("GEMINI_API_KEY")
}
if key == "" {
return fmt.Errorf("GEMINI_API_KEY environment variable or --key flag must be set")
}

ctx := context.Background()
return agent.Run(ctx, key, model, namespace, k8sVersion, addonsFilter, outputFormat)
},
SilenceUsage: true,
}

rootCmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace filter (default: all namespaces)")
rootCmd.Flags().StringVarP(&k8sVersion, "cluster", "c", "", "Kubernetes version override (e.g. 1.30)")
rootCmd.Flags().StringVarP(&addonsFilter, "addons", "a", "", "Comma-separated addon name filter")
rootCmd.Flags().StringVarP(&apiKey, "key", "k", "", "Gemini API key (overrides GEMINI_API_KEY env var)")
rootCmd.Flags().StringVarP(&model, "model", "m", "gemini-3-flash-preview", "Gemini model to use")
rootCmd.Flags().StringVarP(&outputFormat, "output", "o", "json", "Output format: json or table")
rootCmd.Flags().BoolVarP(&showVersion, "version", "v", false, "Show version information")

if err := rootCmd.Execute(); err != nil {
fmt.Fprintln(os.Stderr, err)
os.Exit(1)
}
}
