package addon

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

//go:embed k8s_universal_addons.json
var addonsJSON []byte

// Addon represents a known Kubernetes addon in the embedded database.
type Addon struct {
	Name                    string              `json:"name"`
	ProjectURL              string              `json:"project_url"`
	Repository              string              `json:"repository"`
	CompatibilityMatrixURL  string              `json:"compatibility_matrix_url"`
	ChangelogLocation       string              `json:"changelog_location"`
	KubernetesCompatibility map[string][]string `json:"kubernetes_compatibility,omitempty"`
	KubernetesMinVersion    string              `json:"kubernetes_min_version,omitempty"`
	KubernetesMaxVersion    string              `json:"kubernetes_max_version,omitempty"`
}

// HasStoredCompatibility returns true if the addon has pre-populated
// Kubernetes compatibility data (either a full matrix or a minimum/maximum version).
func (a *Addon) HasStoredCompatibility() bool {
	return len(a.KubernetesCompatibility) > 0 || a.KubernetesMinVersion != "" || a.KubernetesMaxVersion != ""
}

type addonsFile struct {
	Addons []Addon `json:"addons"`
}

// EOLCycle represents a release cycle from the endoflife.date API.
type EOLCycle struct {
	Cycle             string `json:"cycle"`
	ReleaseDate       string `json:"releaseDate"`
	EOL               any    `json:"eol"`
	Latest            string `json:"latest"`
	LatestReleaseDate string `json:"latestReleaseDate"`
	LTS               any    `json:"lts"`
}

// EOLProductCatalogEntry represents a single product from endoflife.date v1.
type EOLProductCatalogEntry struct {
	Name    string   `json:"name"`
	Label   string   `json:"label"`
	Aliases []string `json:"aliases"`
}

// LoadAddons parses the embedded addon database.
func LoadAddons() ([]Addon, error) {
	var f addonsFile
	if err := json.Unmarshal(addonsJSON, &f); err != nil {
		return nil, fmt.Errorf("parsing embedded addons JSON: %w", err)
	}
	return f.Addons, nil
}

// addonAliases maps lowercase detected names to lowercase canonical DB names.
// Only truly irregular names that cannot be resolved by normalization belong here.
var addonAliases = map[string]string{
	"nodelocaldns":   "nodelocal dnscache",
	"node-local-dns": "nodelocal dnscache",
}

// componentRoleSuffixes are common K8s workload role suffixes that indicate a
// sub-component of a parent addon (e.g., "ebs-csi-node" → "ebs-csi").
var componentRoleSuffixes = map[string]bool{
	"node":        true,
	"controller":  true,
	"master":      true,
	"replica":     true,
	"replicas":    true,
	"server":      true,
	"agent":       true,
	"webhook":     true,
	"init":        true,
	"snapshotter": true,
	"operator":    true,
	"scheduler":   true,
	"driver":      true,
}

func normalizeName(name string) string {
	n := strings.ToLower(name)
	n = strings.ReplaceAll(n, "-", " ")
	n = strings.Join(strings.Fields(n), " ")
	if strings.HasPrefix(n, "amazon ") {
		n = "aws " + n[len("amazon "):]
	}
	return n
}

func stripRoleSuffix(normalized string) (string, bool) {
	idx := strings.LastIndex(normalized, " ")
	if idx <= 0 {
		return normalized, false
	}
	lastWord := normalized[idx+1:]
	if componentRoleSuffixes[lastWord] {
		return normalized[:idx], true
	}
	return normalized, false
}

func wordSubsetMatch(subset, superset string) bool {
	superWords := make(map[string]bool)
	for _, w := range strings.Fields(superset) {
		superWords[w] = true
	}
	for _, w := range strings.Fields(subset) {
		if !superWords[w] {
			return false
		}
	}
	return true
}

type eolSlugAliasGroup struct {
	productSlug string
	addonNames  []string
}

var eolSlugAliasGroups = []eolSlugAliasGroup{
	{productSlug: "argo-cd", addonNames: []string{"argo-cd", "argocd", "argo cd"}},
	{productSlug: "argo-workflows", addonNames: []string{"argo-workflows"}},
	{productSlug: "calico", addonNames: []string{"calico", "project calico", "kubernetes network policy (calico)"}},
	{productSlug: "cert-manager", addonNames: []string{"cert-manager", "cert manager", "cert-manager trust manager", "cert-manager approver-policy"}},
	{productSlug: "cilium", addonNames: []string{"cilium", "cilium clustermesh", "cilium network policy", "cilium hubble", "cilium service mesh", "network policy editor (cilium)"}},
	{productSlug: "containerd", addonNames: []string{"containerd"}},
	{productSlug: "contour", addonNames: []string{"contour"}},
	{productSlug: "envoy", addonNames: []string{"envoy", "envoy gateway"}},
	{productSlug: "etcd", addonNames: []string{"etcd"}},
	{productSlug: "flux", addonNames: []string{"flux", "fluxcd", "flux notification controller", "flux image automation"}},
	{productSlug: "gatekeeper", addonNames: []string{"gatekeeper", "opa gatekeeper"}},
	{productSlug: "grafana", addonNames: []string{"grafana", "grafana oncall", "grafana mimir", "grafana pyroscope", "grafana tempo", "grafana alloy"}},
	{productSlug: "grafana-loki", addonNames: []string{"grafana-loki", "grafana loki", "loki"}},
	{productSlug: "harbor", addonNames: []string{"harbor"}},
	{productSlug: "istio", addonNames: []string{"istio", "istio ambient mesh", "istio operator"}},
	{productSlug: "keda", addonNames: []string{"keda", "keda http add-on"}},
	{productSlug: "kuma", addonNames: []string{"kuma"}},
	{productSlug: "kyverno", addonNames: []string{"kyverno", "kyverno policy reporter"}},
	{productSlug: "prometheus", addonNames: []string{"prometheus", "prometheus operator / kube-prometheus-stack", "prometheus adapter", "prometheus pushgateway", "prometheus blackbox exporter"}},
	{productSlug: "traefik", addonNames: []string{"traefik", "traefik mesh"}},
	{productSlug: "kubernetes", addonNames: []string{"kube-proxy"}},
	{productSlug: "redis", addonNames: []string{"redis", "redis-master", "redis-node", "redis-replicas"}},
}

// eolProductSlugs maps normalized addon names to endoflife.date product slugs.
var eolProductSlugs = buildEOLProductSlugs(eolSlugAliasGroups)

func buildEOLProductSlugs(groups []eolSlugAliasGroup) map[string]string {
	slugsByAddonName := make(map[string]string)
	for _, group := range groups {
		for _, addonName := range group.addonNames {
			slugsByAddonName[normalizeName(addonName)] = group.productSlug
		}
	}
	return slugsByAddonName
}

// LookupEOLSlug returns the endoflife.date product slug for a given addon name.
func LookupEOLSlug(addonName string) (string, bool) {
	slug, ok := eolProductSlugs[normalizeName(addonName)]
	return slug, ok
}

// BuildRuntimeEOLSlugLookup builds a normalized name->slug map from the live EOL catalog.
func BuildRuntimeEOLSlugLookup(products []EOLProductCatalogEntry) map[string]string {
	lookup := make(map[string]string)
	for _, product := range products {
		slug := strings.TrimSpace(strings.ToLower(product.Name))
		if slug == "" {
			continue
		}
		registerEOLLookupKey(lookup, slug, slug)
		registerEOLLookupKey(lookup, product.Label, slug)
		for _, alias := range product.Aliases {
			registerEOLLookupKey(lookup, alias, slug)
		}
	}
	return lookup
}

func registerEOLLookupKey(lookup map[string]string, rawName string, slug string) {
	normalized := normalizeName(rawName)
	if normalized == "" {
		return
	}
	if _, exists := lookup[normalized]; !exists {
		lookup[normalized] = slug
	}
}

// LookupEOLSlugWithRuntime resolves against live catalog first, then static fallback aliases.
func LookupEOLSlugWithRuntime(addonName string, runtimeLookup map[string]string) (string, bool) {
	normalizedAddonName := normalizeName(addonName)
	if runtimeLookup != nil {
		if slug, ok := runtimeLookup[normalizedAddonName]; ok {
			return slug, true
		}
	}
	return LookupEOLSlug(addonName)
}

type addonEntry struct {
	addon     Addon
	lowerName string
}

// Matcher precomputes normalized addon names for faster repeated lookups.
type Matcher struct {
	entries      []addonEntry
	firstByLower map[string]Addon
}

// NewMatcher builds a reusable matcher for repeated addon name lookups.
func NewMatcher(addons []Addon) *Matcher {
	entries := make([]addonEntry, len(addons))
	firstByLower := make(map[string]Addon, len(addons))
	for i, addon := range addons {
		lowerName := strings.ToLower(addon.Name)
		entries[i] = addonEntry{
			addon:     addon,
			lowerName: lowerName,
		}
		if _, exists := firstByLower[lowerName]; !exists {
			firstByLower[lowerName] = addon
		}
	}
	return &Matcher{
		entries:      entries,
		firstByLower: firstByLower,
	}
}

// Match resolves a detected addon name to known addon definitions.
func (matcher *Matcher) Match(name string) []Addon {
	lower := strings.ToLower(name)

	// Pass 0: Alias resolution — only for truly irregular names
	if canonical, ok := addonAliases[lower]; ok {
		lower = canonical
	}

	// Pass 1: Exact match (case-insensitive on raw name)
	if exact, ok := matcher.firstByLower[lower]; ok {
		return []Addon{exact}
	}

	// Pass 2: Normalize and try exact match
	normalized := normalizeName(name)
	if normalized != lower {
		if exact, ok := matcher.firstByLower[normalized]; ok {
			return []Addon{exact}
		}
	}

	// Pass 3: Strip role suffix from normalized name and try exact match
	core, stripped := stripRoleSuffix(normalized)
	if stripped {
		if exact, ok := matcher.firstByLower[core]; ok {
			return []Addon{exact}
		}
	}

	// Skip fuzzy matching for very short or generic names
	if len(normalized) < 4 {
		return nil
	}

	// Pass 4: Forward prefix — DB name starts with detected/normalized name + separator
	for _, candidate := range []string{lower, normalized, core} {
		var matches []Addon
		for _, entry := range matcher.entries {
			if strings.HasPrefix(entry.lowerName, candidate+" ") || strings.HasPrefix(entry.lowerName, candidate+"-") {
				matches = append(matches, entry.addon)
			}
		}
		if len(matches) > 0 {
			return matches
		}
	}

	// Pass 5: Reverse prefix — detected/normalized name starts with DB name + separator
	var matches []Addon
	for _, entry := range matcher.entries {
		if len(entry.lowerName) >= 4 {
			if strings.HasPrefix(normalized, entry.lowerName+" ") || strings.HasPrefix(normalized, entry.lowerName+"-") {
				matches = append(matches, entry.addon)
			}
		}
	}
	if len(matches) > 0 {
		return matches
	}

	// Pass 6: Word-subset match — all words of the core name appear in a DB name
	if len(strings.Fields(core)) >= 2 {
		for _, entry := range matcher.entries {
			if wordSubsetMatch(core, entry.lowerName) {
				matches = append(matches, entry.addon)
			}
		}
	}
	return matches
}

// LookupAddon matches a detected workload name against the addon database.
func LookupAddon(name string, addons []Addon) []Addon {
	return NewMatcher(addons).Match(name)
}

// ResolveEOLStatus matches an installed version against EOL cycles and returns
// the support status and support-until date.
func ResolveEOLStatus(installedVersion string, cycles []EOLCycle) (*bool, string) {
	if len(cycles) == 0 {
		return nil, ""
	}

	for _, c := range cycles {
		if versionMatchesCycle(installedVersion, c.Cycle) {
			return parseEOLField(c.EOL)
		}
	}

	return nil, ""
}

func versionMatchesCycle(version, cycle string) bool {
	v := strings.TrimPrefix(version, "v")

	if v == cycle {
		return true
	}

	vParts := strings.SplitN(v, ".", 3)
	cParts := strings.SplitN(cycle, ".", 3)

	if len(vParts) < 2 || len(cParts) < 1 {
		return false
	}

	if len(cParts) == 1 {
		return vParts[0] == cParts[0]
	}

	return vParts[0] == cParts[0] && vParts[1] == cParts[1]
}

func parseEOLField(eol any) (*bool, string) {
	switch v := eol.(type) {
	case bool:
		supported := !v
		return &supported, ""
	case string:
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return nil, ""
		}
		supported := time.Now().Before(t)
		return &supported, v
	default:
		return nil, ""
	}
}
