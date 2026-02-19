package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type detectedAddon struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Version   string `json:"version"`
	Source    string `json:"source"`
}

type addonWithInfo struct {
	detectedAddon
	DBMatch              *Addon `json:"db_match,omitempty"`
	CompatibilityContent string `json:"compatibility_content,omitempty"`
	CompatibilityURL     string `json:"compatibility_url,omitempty"`
	FetchError           string `json:"fetch_error,omitempty"`
}

// getClusterVersion runs kubectl version and returns major.minor string.
func getClusterVersion(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "kubectl", "version", "--output=json").Output()
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

// listInstalledAddons discovers addons from the cluster deterministically.
func listInstalledAddons(ctx context.Context, namespace string) ([]detectedAddon, error) {
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
	var addons []detectedAddon

	for _, q := range queries {
		cmdArgs := append([]string{"get", q.resource, "-o", "json"}, nsFlag...)
		out, err := exec.CommandContext(ctx, "kubectl", cmdArgs...).Output()
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

			addons = append(addons, detectedAddon{
				Name:      name,
				Namespace: item.Metadata.Namespace,
				Version:   version,
				Source:    q.source,
			})
		}
	}

	return addons, nil
}

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)
var whitespaceRe = regexp.MustCompile(`\s+`)

// fetchCompatibilityPage fetches a URL and returns stripped text content.
func fetchCompatibilityPage(ctx context.Context, url string) (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response body: %w", err)
	}

	text := htmlTagRe.ReplaceAllString(string(body), " ")
	text = whitespaceRe.ReplaceAllString(text, " ")

	if len(text) > 30000 {
		text = text[:30000]
	}

	return text, nil
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
