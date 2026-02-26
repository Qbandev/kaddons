package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/qbandev/kaddons/internal/resilience"
)

// DetectedAddon represents a workload discovered from the cluster.
type DetectedAddon struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Version   string `json:"version"`
	Source    string `json:"source"`
}

// GetClusterVersion runs kubectl version and returns major.minor string.
func GetClusterVersion(ctx context.Context) (string, error) {
	out, err := runKubectlCommandWithRetry(ctx, "version", "--output=json")
	if err != nil {
		return "", fmt.Errorf("kubectl version failed: %w", err)
	}

	var ver struct {
		ServerVersion struct {
			Major string `json:"major"`
			Minor string `json:"minor"`
		} `json:"serverVersion"`
	}
	if err := json.Unmarshal(out, &ver); err != nil {
		return "", fmt.Errorf("parsing kubectl version output: %w", err)
	}

	minor := strings.TrimRight(ver.ServerVersion.Minor, "+")
	return fmt.Sprintf("%s.%s", ver.ServerVersion.Major, minor), nil
}

// ListInstalledAddons discovers addons from the cluster deterministically.
func ListInstalledAddons(ctx context.Context, namespace string) ([]DetectedAddon, error) {
	var nsFlag []string
	if namespace != "" {
		nsFlag = []string{"-n", namespace}
	} else {
		nsFlag = []string{"--all-namespaces"}
	}

	type resourceQuery struct {
		resource string
		source   string
		isCRD    bool
	}

	queries := []resourceQuery{
		{"deployments", "deployment", false},
		{"daemonsets", "daemonset", false},
		{"statefulsets", "statefulset", false},
		{"helmreleases.helm.toolkit.fluxcd.io", "helmrelease", true},
		{"applications.argoproj.io", "argocd-app", true},
	}

	seen := make(map[string]bool)
	var addons []DetectedAddon

	for _, q := range queries {
		cmdArgs := append([]string{"get", q.resource, "-o", "json"}, nsFlag...)
		out, err := runKubectlCommandWithRetry(ctx, cmdArgs...)
		if err != nil {
			if q.isCRD {
				continue
			}
			return nil, fmt.Errorf("kubectl get %s failed: %w", q.resource, err)
		}

		var list struct {
			Items []struct {
				Metadata struct {
					Name        string            `json:"name"`
					Namespace   string            `json:"namespace"`
					Labels      map[string]string `json:"labels"`
					Annotations map[string]string `json:"annotations"`
				} `json:"metadata"`
				Spec struct {
					Template struct {
						Spec struct {
							Containers []struct {
								Image string `json:"image"`
							} `json:"containers"`
						} `json:"spec"`
					} `json:"template"`
					Source struct {
						Chart string `json:"chart"`
					} `json:"source"`
				} `json:"spec"`
			} `json:"items"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping %s due to unexpected kubectl JSON output: %v\n", q.resource, err)
			continue
		}

		for _, item := range list.Items {
			labels := item.Metadata.Labels
			annotations := item.Metadata.Annotations

			var name string
			switch {
			case q.source == "argocd-app":
				name = item.Spec.Source.Chart
				if name == "" {
					name = item.Metadata.Name
				}
			case labels["app.kubernetes.io/name"] != "":
				name = labels["app.kubernetes.io/name"]
			case annotations["meta.helm.sh/release-name"] != "":
				name = annotations["meta.helm.sh/release-name"]
			case labels["helm.sh/chart"] != "":
				name = stripChartVersion(labels["helm.sh/chart"])
			default:
				name = item.Metadata.Name
			}

			var version string
			switch {
			case labels["app.kubernetes.io/version"] != "":
				version = labels["app.kubernetes.io/version"]
			case labels["helm.sh/chart"] != "":
				version = extractChartVersion(labels["helm.sh/chart"])
			default:
				if len(item.Spec.Template.Spec.Containers) > 0 {
					version = extractImageTag(item.Spec.Template.Spec.Containers[0].Image)
				}
			}

			key := name + "/" + item.Metadata.Namespace
			if seen[key] {
				continue
			}
			seen[key] = true

			addons = append(addons, DetectedAddon{
				Name:      name,
				Namespace: item.Metadata.Namespace,
				Version:   version,
				Source:    q.source,
			})
		}
	}

	return addons, nil
}

func runKubectlCommandWithRetry(ctx context.Context, args ...string) ([]byte, error) {
	policy := resilience.RetryPolicy{
		Attempts:     3,
		InitialDelay: 500 * time.Millisecond,
		MaxDelay:     time.Second,
		Multiplier:   2,
	}
	return resilience.RetryWithResult(ctx, policy, isRetryableKubectlError, func(callCtx context.Context) ([]byte, error) {
		output, err := exec.CommandContext(callCtx, "kubectl", args...).Output() // #nosec G204 -- kubectl is a well-known binary, not user-controlled input
		if err == nil {
			return output, nil
		}
		return nil, err
	})
}

func isRetryableKubectlError(err error) bool {
	return resilience.IsRetryableNetworkError(err)
}

func stripChartVersion(chart string) string {
	idx := strings.LastIndex(chart, "-")
	if idx == -1 {
		return chart
	}
	suffix := chart[idx+1:]
	if len(suffix) > 0 && suffix[0] >= '0' && suffix[0] <= '9' {
		return chart[:idx]
	}
	return chart
}

func extractChartVersion(chart string) string {
	idx := strings.LastIndex(chart, "-")
	if idx == -1 {
		return ""
	}
	suffix := chart[idx+1:]
	if len(suffix) > 0 && suffix[0] >= '0' && suffix[0] <= '9' {
		return suffix
	}
	return ""
}

func extractImageTag(image string) string {
	if idx := strings.LastIndex(image, ":"); idx != -1 {
		return image[idx+1:]
	}
	return ""
}
