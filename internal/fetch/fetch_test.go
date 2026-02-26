package fetch

import (
	"strings"
	"testing"
)

func TestGitHubRawURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "repo root with anchor",
			input: "https://github.com/stakater/Reloader#compatibility",
			want:  "https://raw.githubusercontent.com/stakater/Reloader/HEAD/README.md",
		},
		{
			name:  "repo root with #readme",
			input: "https://github.com/stakater/Reloader#readme",
			want:  "https://raw.githubusercontent.com/stakater/Reloader/HEAD/README.md",
		},
		{
			name:  "bare repo root",
			input: "https://github.com/stakater/Reloader",
			want:  "https://raw.githubusercontent.com/stakater/Reloader/HEAD/README.md",
		},
		{
			name:  "blob ref file",
			input: "https://github.com/open-telemetry/opentelemetry-operator/blob/main/docs/compatibility.md",
			want:  "https://raw.githubusercontent.com/open-telemetry/opentelemetry-operator/main/docs/compatibility.md",
		},
		{
			name:  "blob nested path with anchor",
			input: "https://github.com/owner/repo/blob/v1.2.3/docs/install/README.md#section",
			want:  "https://raw.githubusercontent.com/owner/repo/v1.2.3/docs/install/README.md",
		},
		{
			name:  "tree ref directory root",
			input: "https://github.com/prometheus-community/helm-charts/tree/main",
			want:  "https://raw.githubusercontent.com/prometheus-community/helm-charts/main/README.md",
		},
		{
			name:  "tree ref subdirectory",
			input: "https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack",
			want:  "https://raw.githubusercontent.com/prometheus-community/helm-charts/main/charts/kube-prometheus-stack/README.md",
		},
		{
			name:  "tree with query params and anchor",
			input: "https://github.com/owner/repo/tree/main?tab=readme-ov-file#section",
			want:  "https://raw.githubusercontent.com/owner/repo/main/README.md",
		},
		{
			name:  "wiki URL unchanged",
			input: "https://github.com/owner/repo/wiki/Some-Page",
			want:  "https://github.com/owner/repo/wiki/Some-Page",
		},
		{
			name:  "release URL unchanged",
			input: "https://github.com/owner/repo/releases/tag/v1.0.0",
			want:  "https://github.com/owner/repo/releases/tag/v1.0.0",
		},
		{
			name:  "non-GitHub URL unchanged",
			input: "https://argo-cd.readthedocs.io/en/stable/operator-manual/installation/#tested-versions",
			want:  "https://argo-cd.readthedocs.io/en/stable/operator-manual/installation/#tested-versions",
		},
		{
			name:  "org-only URL unchanged",
			input: "https://github.com/prometheus-community",
			want:  "https://github.com/prometheus-community",
		},
		{
			name:  "rst file in blob",
			input: "https://github.com/owner/repo/blob/main/docs/compat.rst",
			want:  "https://raw.githubusercontent.com/owner/repo/main/docs/compat.rst",
		},
		{
			name:  "repo root with trailing slash",
			input: "https://github.com/stakater/Reloader/",
			want:  "https://raw.githubusercontent.com/stakater/Reloader/HEAD/README.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GitHubRawURL(tt.input)
			if got != tt.want {
				t.Errorf("GitHubRawURL(%q)\n  got:  %q\n  want: %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseEOLProducts(t *testing.T) {
	body := []byte(`{
		"schema_version":"1.2.0",
		"result":[
			{"name":"argo-cd","aliases":["argocd"],"label":"Argo CD"},
			{"name":"keda","aliases":[],"label":"KEDA"}
		]
	}`)

	products, err := parseEOLProducts(body)
	if err != nil {
		t.Fatalf("parseEOLProducts() error = %v", err)
	}

	if len(products) != 2 {
		t.Fatalf("parseEOLProducts() length = %d, want 2", len(products))
	}
	if products[0].Name != "argo-cd" {
		t.Fatalf("products[0].Name = %q, want %q", products[0].Name, "argo-cd")
	}
	if len(products[0].Aliases) != 1 || products[0].Aliases[0] != "argocd" {
		t.Fatalf("products[0].Aliases = %v, want [argocd]", products[0].Aliases)
	}
}

func TestParseEOLProducts_InvalidJSON(t *testing.T) {
	_, err := parseEOLProducts([]byte(`{"result":[`))
	if err == nil {
		t.Fatal("parseEOLProducts() expected error for invalid JSON")
	}
}

func TestNormalizeFetchedContent_PreservesTableLikeLines(t *testing.T) {
	html := `
<html><body>
  <h2>Tested versions</h2>
  <table>
    <tr><th>Argo CD version</th><th>Kubernetes versions</th></tr>
    <tr><td>3.3</td><td>v1.34, v1.33, v1.32, v1.31</td></tr>
  </table>
</body></html>`

	got := normalizeFetchedContent(html, false)

	if !strings.Contains(got, "Tested versions") {
		t.Fatalf("normalizeFetchedContent() missing heading: %q", got)
	}
	if !strings.Contains(got, "Argo CD version") || !strings.Contains(got, "Kubernetes versions") {
		t.Fatalf("normalizeFetchedContent() missing table headers: %q", got)
	}
	if !strings.Contains(got, "v1.31") {
		t.Fatalf("normalizeFetchedContent() missing table cell content: %q", got)
	}
}
