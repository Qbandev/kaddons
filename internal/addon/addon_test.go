package addon

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLoadAddons(t *testing.T) {
	addons, err := LoadAddons()
	if err != nil {
		t.Fatalf("LoadAddons() error: %v", err)
	}
	if len(addons) == 0 {
		t.Fatal("LoadAddons() returned 0 addons")
	}

	first := addons[0]
	if first.Name == "" {
		t.Error("first addon has empty Name")
	}
}

func TestEmbeddedDatabase_DoesNotContainUpgradePathType(t *testing.T) {
	if strings.Contains(string(addonsJSON), "\"upgrade_path_type\"") {
		t.Fatal("embedded addon database should not include upgrade_path_type")
	}
}

func TestLookupAddon_ExactMatch(t *testing.T) {
	addons := []Addon{
		{Name: "Cert Manager"},
		{Name: "Istio"},
		{Name: "Prometheus"},
	}

	matches := LookupAddon("Istio", addons)
	if len(matches) != 1 {
		t.Fatalf("LookupAddon(Istio) returned %d matches, want 1", len(matches))
	}
	if matches[0].Name != "Istio" {
		t.Errorf("LookupAddon(Istio) = %q, want %q", matches[0].Name, "Istio")
	}
}

func TestLookupAddon_CaseInsensitive(t *testing.T) {
	addons := []Addon{
		{Name: "Cert Manager"},
		{Name: "Istio"},
	}

	matches := LookupAddon("istio", addons)
	if len(matches) != 1 {
		t.Fatalf("LookupAddon(istio) returned %d matches, want 1", len(matches))
	}
	if matches[0].Name != "Istio" {
		t.Errorf("LookupAddon(istio) = %q, want %q", matches[0].Name, "Istio")
	}
}

func TestLookupAddon_PrefixMatch(t *testing.T) {
	addons := []Addon{
		{Name: "cert-manager"},
		{Name: "cert-manager-csi-driver"},
	}

	matches := LookupAddon("cert", addons)
	if len(matches) != 2 {
		t.Fatalf("LookupAddon(cert) returned %d matches, want 2", len(matches))
	}
}

func TestLookupAddon_ShortNameSkipped(t *testing.T) {
	addons := []Addon{
		{Name: "aws-alb-controller"},
	}

	matches := LookupAddon("aws", addons)
	if len(matches) != 0 {
		t.Errorf("LookupAddon(aws) returned %d matches, want 0 (short name skip)", len(matches))
	}
}

func TestLookupAddon_NoMatch(t *testing.T) {
	addons := []Addon{
		{Name: "Istio"},
		{Name: "Prometheus"},
	}

	matches := LookupAddon("nonexistent", addons)
	if len(matches) != 0 {
		t.Errorf("LookupAddon(nonexistent) returned %d matches, want 0", len(matches))
	}
}

func TestLookupAddon_NormalizeAmazonPrefix(t *testing.T) {
	addons := []Addon{
		{Name: "AWS VPC CNI"},
		{Name: "AWS EBS CSI Driver"},
		{Name: "AWS EFS CSI Driver"},
	}

	tests := []struct {
		input string
		want  string
	}{
		{"amazon-vpc-cni", "AWS VPC CNI"},
		{"amazon-ebs-csi-driver", "AWS EBS CSI Driver"},
		{"amazon-efs-csi-driver", "AWS EFS CSI Driver"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			matches := LookupAddon(tt.input, addons)
			if len(matches) != 1 {
				t.Fatalf("LookupAddon(%q) returned %d matches, want 1", tt.input, len(matches))
			}
			if matches[0].Name != tt.want {
				t.Errorf("LookupAddon(%q) = %q, want %q", tt.input, matches[0].Name, tt.want)
			}
		})
	}
}

func TestLookupAddon_NormalizeHyphenToSpace(t *testing.T) {
	addons := []Addon{
		{Name: "AWS Network Policy Agent"},
		{Name: "Prometheus Node Exporter"},
	}

	tests := []struct {
		input string
		want  string
	}{
		{"aws-network-policy-agent", "AWS Network Policy Agent"},
		{"prometheus-node-exporter", "Prometheus Node Exporter"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			matches := LookupAddon(tt.input, addons)
			if len(matches) != 1 {
				t.Fatalf("LookupAddon(%q) returned %d matches, want 1", tt.input, len(matches))
			}
			if matches[0].Name != tt.want {
				t.Errorf("LookupAddon(%q) = %q, want %q", tt.input, matches[0].Name, tt.want)
			}
		})
	}
}

func TestLookupAddon_StripRoleSuffix(t *testing.T) {
	addons := []Addon{
		{Name: "AWS EBS CSI Driver"},
		{Name: "AWS EFS CSI Driver"},
		{Name: "Redis"},
	}

	tests := []struct {
		input string
		want  string
	}{
		{"ebs-csi-node", "AWS EBS CSI Driver"},
		{"ebs-csi-controller", "AWS EBS CSI Driver"},
		{"ebs-csi-snapshotter", "AWS EBS CSI Driver"},
		{"efs-csi-controller", "AWS EFS CSI Driver"},
		{"efs-csi-node", "AWS EFS CSI Driver"},
		{"redis-master", "Redis"},
		{"redis-node", "Redis"},
		{"redis-replicas", "Redis"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			matches := LookupAddon(tt.input, addons)
			if len(matches) != 1 {
				t.Fatalf("LookupAddon(%q) returned %d matches, want 1", tt.input, len(matches))
			}
			if matches[0].Name != tt.want {
				t.Errorf("LookupAddon(%q) = %q, want %q", tt.input, matches[0].Name, tt.want)
			}
		})
	}
}

func TestLookupAddon_WordSubsetMatch(t *testing.T) {
	addons := []Addon{
		{Name: "Prometheus Node Exporter"},
		{Name: "AWS Network Policy Agent"},
	}

	tests := []struct {
		input string
		want  string
	}{
		{"node-exporter", "Prometheus Node Exporter"},
		{"network-policy-agent", "AWS Network Policy Agent"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			matches := LookupAddon(tt.input, addons)
			if len(matches) != 1 {
				t.Fatalf("LookupAddon(%q) returned %d matches, want 1", tt.input, len(matches))
			}
			if matches[0].Name != tt.want {
				t.Errorf("LookupAddon(%q) = %q, want %q", tt.input, matches[0].Name, tt.want)
			}
		})
	}
}

func TestLookupAddon_AliasNodeLocalDNS(t *testing.T) {
	addons := []Addon{
		{Name: "NodeLocal DNSCache"},
		{Name: "CoreDNS"},
	}

	for _, input := range []string{"nodelocaldns", "node-local-dns"} {
		t.Run(input, func(t *testing.T) {
			matches := LookupAddon(input, addons)
			if len(matches) != 1 {
				t.Fatalf("LookupAddon(%q) returned %d matches, want 1", input, len(matches))
			}
			if matches[0].Name != "NodeLocal DNSCache" {
				t.Errorf("LookupAddon(%q) = %q, want %q", input, matches[0].Name, "NodeLocal DNSCache")
			}
		})
	}
}

func TestLookupAddon_ReversePrefixMatch(t *testing.T) {
	addons := []Addon{
		{Name: "Prometheus"},
		{Name: "Grafana"},
	}

	matches := LookupAddon("prometheus-operator", addons)
	if len(matches) != 1 {
		t.Fatalf("LookupAddon(prometheus-operator) returned %d matches, want 1", len(matches))
	}
	if matches[0].Name != "Prometheus" {
		t.Errorf("LookupAddon(prometheus-operator) = %q, want %q", matches[0].Name, "Prometheus")
	}
}

func TestLookupAddon_NoMatchUnknown(t *testing.T) {
	addons := []Addon{
		{Name: "Istio"},
	}

	matches := LookupAddon("totally-unknown-addon", addons)
	if len(matches) != 0 {
		t.Errorf("LookupAddon(totally-unknown-addon) returned %d matches, want 0", len(matches))
	}
}

func TestMatcher_Match(t *testing.T) {
	addons := []Addon{
		{Name: "AWS EBS CSI Driver"},
		{Name: "Prometheus Node Exporter"},
		{Name: "NodeLocal DNSCache"},
	}
	matcher := NewMatcher(addons)

	tests := []struct {
		input string
		want  string
	}{
		{input: "ebs-csi-node", want: "AWS EBS CSI Driver"},
		{input: "node-exporter", want: "Prometheus Node Exporter"},
		{input: "node-local-dns", want: "NodeLocal DNSCache"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			matches := matcher.Match(tt.input)
			if len(matches) != 1 {
				t.Fatalf("Match(%q) returned %d matches, want 1", tt.input, len(matches))
			}
			if matches[0].Name != tt.want {
				t.Fatalf("Match(%q) = %q, want %q", tt.input, matches[0].Name, tt.want)
			}
		})
	}
}

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"amazon-vpc-cni", "aws vpc cni"},
		{"amazon-ebs-csi-driver", "aws ebs csi driver"},
		{"aws-network-policy-agent", "aws network policy agent"},
		{"prometheus-node-exporter", "prometheus node exporter"},
		{"redis-master", "redis master"},
		{"kube-proxy", "kube proxy"},
		{"Istio", "istio"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeName(tt.input)
			if got != tt.want {
				t.Errorf("normalizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStripRoleSuffix(t *testing.T) {
	tests := []struct {
		input    string
		want     string
		stripped bool
	}{
		{"ebs csi node", "ebs csi", true},
		{"ebs csi controller", "ebs csi", true},
		{"ebs csi snapshotter", "ebs csi", true},
		{"redis master", "redis", true},
		{"redis replicas", "redis", true},
		{"aws vpc cni init", "aws vpc cni", true},
		{"prometheus node exporter", "prometheus node exporter", false},
		{"redis", "redis", false},
		{"cert manager", "cert manager", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, stripped := stripRoleSuffix(tt.input)
			if got != tt.want || stripped != tt.stripped {
				t.Errorf("stripRoleSuffix(%q) = (%q, %v), want (%q, %v)", tt.input, got, stripped, tt.want, tt.stripped)
			}
		})
	}
}

func TestWordSubsetMatch(t *testing.T) {
	tests := []struct {
		subset   string
		superset string
		want     bool
	}{
		{"ebs csi", "aws ebs csi driver", true},
		{"node exporter", "prometheus node exporter", true},
		{"network policy agent", "aws network policy agent", true},
		{"redis", "redis", true},
		{"foo bar", "aws ebs csi driver", false},
		{"ebs csi baz", "aws ebs csi driver", false},
	}
	for _, tt := range tests {
		t.Run(tt.subset+"_in_"+tt.superset, func(t *testing.T) {
			got := wordSubsetMatch(tt.subset, tt.superset)
			if got != tt.want {
				t.Errorf("wordSubsetMatch(%q, %q) = %v, want %v", tt.subset, tt.superset, got, tt.want)
			}
		})
	}
}

func TestLookupEOLSlug_Found(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"cert-manager", "cert-manager"},
		{"istio", "istio"},
		{"grafana", "grafana"},
		{"traefik", "traefik"},
		{"loki", "grafana-loki"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slug, ok := LookupEOLSlug(tt.name)
			if !ok {
				t.Errorf("LookupEOLSlug(%q) not found", tt.name)
			}
			if slug != tt.want {
				t.Errorf("LookupEOLSlug(%q) = %q, want %q", tt.name, slug, tt.want)
			}
		})
	}
}

func TestLookupEOLSlug_NotFound(t *testing.T) {
	_, ok := LookupEOLSlug("nonexistent-addon")
	if ok {
		t.Error("LookupEOLSlug(nonexistent-addon) should return false")
	}
}

func TestLookupEOLSlug_CaseInsensitive(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"Cert-Manager", "cert-manager"},
		{"ISTIO", "istio"},
		{"Grafana", "grafana"},
		{"ArgoCD", "argo-cd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slug, ok := LookupEOLSlug(tt.name)
			if !ok {
				t.Errorf("LookupEOLSlug(%q) not found", tt.name)
			}
			if slug != tt.want {
				t.Errorf("LookupEOLSlug(%q) = %q, want %q", tt.name, slug, tt.want)
			}
		})
	}
}

func TestBuildRuntimeEOLSlugLookup(t *testing.T) {
	products := []EOLProductCatalogEntry{
		{
			Name:    "argo-cd",
			Label:   "Argo CD",
			Aliases: []string{"argocd", "argo"},
		},
		{
			Name:    "grafana-loki",
			Label:   "Grafana Loki",
			Aliases: []string{"loki"},
		},
	}

	lookup := BuildRuntimeEOLSlugLookup(products)

	tests := []struct {
		name string
		want string
	}{
		{name: "argo-cd", want: "argo-cd"},
		{name: "Argo CD", want: "argo-cd"},
		{name: "argocd", want: "argo-cd"},
		{name: "loki", want: "grafana-loki"},
		{name: "Grafana Loki", want: "grafana-loki"},
	}

	for _, tt := range tests {
		got, ok := lookup[normalizeName(tt.name)]
		if !ok {
			t.Fatalf("runtime lookup missing key for %q", tt.name)
		}
		if got != tt.want {
			t.Fatalf("runtime lookup for %q = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestLookupEOLSlugWithRuntime_Fallback(t *testing.T) {
	runtimeLookup := map[string]string{
		normalizeName("Argocd"): "argo-cd",
	}

	if got, ok := LookupEOLSlugWithRuntime("argocd", runtimeLookup); !ok || got != "argo-cd" {
		t.Fatalf("LookupEOLSlugWithRuntime(argocd) = (%q, %v), want (%q, true)", got, ok, "argo-cd")
	}

	if got, ok := LookupEOLSlugWithRuntime("istio", runtimeLookup); !ok || got != "istio" {
		t.Fatalf("LookupEOLSlugWithRuntime(istio) = (%q, %v), want (%q, true)", got, ok, "istio")
	}
}

func TestResolveEOLStatus_BoolEOLFalse(t *testing.T) {
	cycles := []EOLCycle{
		{Cycle: "1.14", EOL: false},
	}
	supported, until := ResolveEOLStatus("v1.14.2", cycles)
	if supported == nil || !*supported {
		t.Error("ResolveEOLStatus should return supported=true for eol=false")
	}
	if until != "" {
		t.Errorf("ResolveEOLStatus until = %q, want empty", until)
	}
}

func TestResolveEOLStatus_BoolEOLTrue(t *testing.T) {
	cycles := []EOLCycle{
		{Cycle: "1.12", EOL: true},
	}
	supported, until := ResolveEOLStatus("v1.12.0", cycles)
	if supported == nil || *supported {
		t.Error("ResolveEOLStatus should return supported=false for eol=true")
	}
	if until != "" {
		t.Errorf("ResolveEOLStatus until = %q, want empty", until)
	}
}

func TestResolveEOLStatus_DateEOLFuture(t *testing.T) {
	cycles := []EOLCycle{
		{Cycle: "1.14", EOL: "2099-12-31"},
	}
	supported, until := ResolveEOLStatus("v1.14.2", cycles)
	if supported == nil || !*supported {
		t.Error("ResolveEOLStatus should return supported=true for future EOL date")
	}
	if until != "2099-12-31" {
		t.Errorf("ResolveEOLStatus until = %q, want %q", until, "2099-12-31")
	}
}

func TestResolveEOLStatus_DateEOLExpired(t *testing.T) {
	cycles := []EOLCycle{
		{Cycle: "1.10", EOL: "2020-01-01"},
	}
	supported, until := ResolveEOLStatus("v1.10.5", cycles)
	if supported == nil || *supported {
		t.Error("ResolveEOLStatus should return supported=false for expired EOL date")
	}
	if until != "2020-01-01" {
		t.Errorf("ResolveEOLStatus until = %q, want %q", until, "2020-01-01")
	}
}

func TestResolveEOLStatus_NoMatch(t *testing.T) {
	cycles := []EOLCycle{
		{Cycle: "1.14", EOL: false},
	}
	supported, until := ResolveEOLStatus("v2.0.0", cycles)
	if supported != nil {
		t.Error("ResolveEOLStatus should return nil for no matching cycle")
	}
	if until != "" {
		t.Errorf("ResolveEOLStatus until = %q, want empty", until)
	}
}

func TestResolveEOLStatus_EmptyCycles(t *testing.T) {
	supported, until := ResolveEOLStatus("v1.14.2", nil)
	if supported != nil {
		t.Error("ResolveEOLStatus should return nil for empty cycles")
	}
	if until != "" {
		t.Errorf("ResolveEOLStatus until = %q, want empty", until)
	}
}

func TestHasStoredCompatibility_FullMatrix(t *testing.T) {
	a := &Addon{
		Name: "test-addon",
		KubernetesCompatibility: map[string][]string{
			"1.5": {"1.28", "1.29"},
		},
	}
	if !a.HasStoredCompatibility() {
		t.Error("HasStoredCompatibility() should return true for addon with full matrix")
	}
}

func TestHasStoredCompatibility_MinVersionOnly(t *testing.T) {
	a := &Addon{
		Name:                 "test-addon",
		KubernetesMinVersion: "1.20",
	}
	if !a.HasStoredCompatibility() {
		t.Error("HasStoredCompatibility() should return true for addon with min version")
	}
}

func TestHasStoredCompatibility_NoData(t *testing.T) {
	a := &Addon{
		Name: "test-addon",
	}
	if a.HasStoredCompatibility() {
		t.Error("HasStoredCompatibility() should return false for addon with no stored data")
	}
}

func TestHasStoredCompatibility_MaxVersionOnly(t *testing.T) {
	a := &Addon{
		Name:                 "test-addon",
		KubernetesMaxVersion: "1.28",
	}
	if !a.HasStoredCompatibility() {
		t.Error("HasStoredCompatibility() should return true for addon with max version")
	}
}

func TestHasStoredCompatibility_EmptyMatrix(t *testing.T) {
	a := &Addon{
		Name:                    "test-addon",
		KubernetesCompatibility: map[string][]string{},
	}
	if a.HasStoredCompatibility() {
		t.Error("HasStoredCompatibility() should return false for addon with empty matrix")
	}
}

func TestAddonJSONRoundTrip_WithStoredData(t *testing.T) {
	original := Addon{
		Name:                   "cert-manager",
		ProjectURL:             "https://cert-manager.io",
		Repository:             "https://github.com/cert-manager/cert-manager",
		CompatibilityMatrixURL: "https://cert-manager.io/docs/releases/",
		ChangelogLocation:      "https://github.com/cert-manager/cert-manager/releases",
		KubernetesCompatibility: map[string][]string{
			"1.15": {"1.28", "1.29", "1.30", "1.31"},
			"1.14": {"1.27", "1.28", "1.29", "1.30"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded Addon
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.Name != original.Name {
		t.Errorf("Name = %q, want %q", decoded.Name, original.Name)
	}
	if len(decoded.KubernetesCompatibility) != 2 {
		t.Fatalf("KubernetesCompatibility has %d entries, want 2", len(decoded.KubernetesCompatibility))
	}
	if len(decoded.KubernetesCompatibility["1.15"]) != 4 {
		t.Fatalf("KubernetesCompatibility[1.15] has %d entries, want 4", len(decoded.KubernetesCompatibility["1.15"]))
	}
}

func TestAddonJSONRoundTrip_OmitsEmptyStoredData(t *testing.T) {
	original := Addon{
		Name:       "test-addon",
		ProjectURL: "https://example.com",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	jsonStr := string(data)
	if strings.Contains(jsonStr, "kubernetes_compatibility") {
		t.Error("JSON should omit empty kubernetes_compatibility")
	}
	if strings.Contains(jsonStr, "kubernetes_min_version") {
		t.Error("JSON should omit empty kubernetes_min_version")
	}
}

func TestVersionMatchesCycle(t *testing.T) {
	tests := []struct {
		version string
		cycle   string
		want    bool
	}{
		{"v1.14.2", "1.14", true},
		{"1.14.2", "1.14", true},
		{"v1.14", "1.14", true},
		{"v2.0.0", "1.14", false},
		{"v8.2.1", "8", true},
		{"v8.2.1", "7", false},
		{"v1.14.2", "1.15", false},
	}
	for _, tt := range tests {
		t.Run(tt.version+"_"+tt.cycle, func(t *testing.T) {
			got := versionMatchesCycle(tt.version, tt.cycle)
			if got != tt.want {
				t.Errorf("versionMatchesCycle(%q, %q) = %v, want %v", tt.version, tt.cycle, got, tt.want)
			}
		})
	}
}
