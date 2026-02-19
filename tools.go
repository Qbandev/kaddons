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

	"google.golang.org/genai"
)

type Tool interface {
	Name() string
	Description() string
	FunctionDeclaration() *genai.FunctionDeclaration
	Run(ctx context.Context, args map[string]any) (map[string]any, error)
}

func allTools(namespace string, addons []Addon) []Tool {
	return []Tool{
		&getClusterVersionTool{},
		&listInstalledAddonsTool{namespace: namespace},
		&lookupAddonInfoTool{addons: addons},
		&checkCompatibilityURLTool{},
	}
}

func toolDeclarations(tools []Tool) []*genai.FunctionDeclaration {
	decls := make([]*genai.FunctionDeclaration, len(tools))
	for i, t := range tools {
		decls[i] = t.FunctionDeclaration()
	}
	return decls
}

func toolsByName(tools []Tool) map[string]Tool {
	m := make(map[string]Tool, len(tools))
	for _, t := range tools {
		m[t.Name()] = t
	}
	return m
}

// --- Tool 1: get_cluster_version ---

type getClusterVersionTool struct{}

func (t *getClusterVersionTool) Name() string { return "get_cluster_version" }
func (t *getClusterVersionTool) Description() string {
	return "Get the Kubernetes cluster server version using kubectl"
}

func (t *getClusterVersionTool) FunctionDeclaration() *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: &genai.Schema{
			Type:       genai.TypeObject,
			Properties: map[string]*genai.Schema{},
		},
	}
}

func (t *getClusterVersionTool) Run(ctx context.Context, args map[string]any) (map[string]any, error) {
	out, err := exec.CommandContext(ctx, "kubectl", "version", "--output=json").CombinedOutput()
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("kubectl version failed: %s: %s", err, string(out))}, nil
	}

	var ver struct {
		ServerVersion struct {
			Major string `json:"major"`
			Minor string `json:"minor"`
		} `json:"serverVersion"`
	}
	if err := json.Unmarshal(out, &ver); err != nil {
		return map[string]any{"error": fmt.Sprintf("parsing kubectl version output: %s", err)}, nil
	}

	minor := strings.TrimRight(ver.ServerVersion.Minor, "+")
	version := fmt.Sprintf("%s.%s", ver.ServerVersion.Major, minor)
	return map[string]any{
		"server_version": version,
		"full_output":    string(out),
	}, nil
}

// --- Tool 2: list_installed_addons ---

type listInstalledAddonsTool struct {
	namespace string
}

func (t *listInstalledAddonsTool) Name() string { return "list_installed_addons" }
func (t *listInstalledAddonsTool) Description() string {
	return "List addons installed in the Kubernetes cluster by inspecting deployments, daemonsets, statefulsets, Flux HelmReleases, and ArgoCD Applications"
}

func (t *listInstalledAddonsTool) FunctionDeclaration() *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"namespace": {
					Type:        genai.TypeString,
					Description: "Kubernetes namespace to filter (empty for all namespaces)",
				},
			},
		},
	}
}

type detectedAddon struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Version   string `json:"version"`
	Source    string `json:"source"`
}

func (t *listInstalledAddonsTool) Run(ctx context.Context, args map[string]any) (map[string]any, error) {
	ns := t.namespace
	if v, ok := args["namespace"].(string); ok && v != "" {
		ns = v
	}

	var nsFlag []string
	if ns != "" {
		nsFlag = []string{"-n", ns}
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
		out, err := exec.CommandContext(ctx, "kubectl", cmdArgs...).CombinedOutput()
		if err != nil {
			if q.isCRD {
				continue // CRD not installed, skip silently
			}
			return map[string]any{"error": fmt.Sprintf("kubectl get %s failed: %s: %s", q.resource, err, string(out))}, nil
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

	return map[string]any{"addons": addons}, nil
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

// --- Tool 3: lookup_addon_info ---

type lookupAddonInfoTool struct {
	addons []Addon
}

func (t *lookupAddonInfoTool) Name() string { return "lookup_addon_info" }
func (t *lookupAddonInfoTool) Description() string {
	return "Look up addon metadata (project URL, compatibility matrix URL, changelog) from the embedded addon database"
}

func (t *lookupAddonInfoTool) FunctionDeclaration() *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"addon_name": {
					Type:        genai.TypeString,
					Description: "Name of the addon to look up",
				},
			},
			Required: []string{"addon_name"},
		},
	}
}

func (t *lookupAddonInfoTool) Run(ctx context.Context, args map[string]any) (map[string]any, error) {
	name, _ := args["addon_name"].(string)
	if name == "" {
		return map[string]any{"error": "addon_name is required"}, nil
	}

	matches := lookupAddon(name, t.addons)
	return map[string]any{
		"matches": matches,
		"found":   len(matches) > 0,
	}, nil
}

// --- Tool 4: check_compatibility_url ---

type checkCompatibilityURLTool struct{}

func (t *checkCompatibilityURLTool) Name() string { return "check_compatibility_url" }
func (t *checkCompatibilityURLTool) Description() string {
	return "Fetch a URL (typically a compatibility matrix page) and return its text content for analysis"
}

func (t *checkCompatibilityURLTool) FunctionDeclaration() *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"url": {
					Type:        genai.TypeString,
					Description: "URL to fetch",
				},
			},
			Required: []string{"url"},
		},
	}
}

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

func (t *checkCompatibilityURLTool) Run(ctx context.Context, args map[string]any) (map[string]any, error) {
	url, _ := args["url"].(string)
	if url == "" {
		return map[string]any{"error": "url is required"}, nil
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return map[string]any{"url": url, "error": fmt.Sprintf("HTTP request failed: %s", err)}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return map[string]any{"url": url, "error": fmt.Sprintf("reading response body: %s", err)}, nil
	}

	text := htmlTagRe.ReplaceAllString(string(body), " ")
	// Collapse whitespace
	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")

	truncated := false
	if len(text) > 30000 {
		text = text[:30000]
		truncated = true
	}

	return map[string]any{
		"url":       url,
		"content":   text,
		"truncated": truncated,
	}, nil
}
