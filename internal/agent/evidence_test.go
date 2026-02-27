package agent

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/qbandev/kaddons/internal/addon"
	"github.com/qbandev/kaddons/internal/cluster"
	"github.com/qbandev/kaddons/internal/output"
)

func TestPruneEvidenceText_IsDeterministicAndBounded(t *testing.T) {
	input := `
intro
this line has kubernetes compatibility matrix for k8s 1.29 1.30
another noise line
supported versions include 1.31 and 1.32
deprecated release note
`

	first := pruneEvidenceText(input, 120, 3)
	second := pruneEvidenceText(input, 120, 3)

	if first != second {
		t.Fatalf("pruneEvidenceText() is not deterministic:\nfirst=%q\nsecond=%q", first, second)
	}
	if len(first) > 120 {
		t.Fatalf("pruneEvidenceText() length = %d, want <= 120", len(first))
	}
}

func TestPruneEvidenceText_FallsBackWhenNoKeywordMatch(t *testing.T) {
	input := "line one\nline two\nline three"
	got := pruneEvidenceText(input, 40, 2)
	want := "line one\nline two"
	if got != want {
		t.Fatalf("pruneEvidenceText() = %q, want %q", got, want)
	}
}

func TestPruneEvidenceText_KeepsContextAroundKeywordLines(t *testing.T) {
	input := strings.Join([]string{
		"header line",
		"compatibility matrix",
		"1.30 -> supported",
		"1.31 -> supported",
		"1.32 -> unsupported",
		"tail line",
	}, "\n")

	got := pruneEvidenceText(input, 500, 20)
	if !strings.Contains(got, "compatibility matrix") {
		t.Fatalf("expected keyword line to be preserved, got %q", got)
	}
	if !strings.Contains(got, "1.31 -> supported") {
		t.Fatalf("expected nearby version row to be preserved, got %q", got)
	}
}

func TestPruneEvidenceText_PrioritizesStrongLateCompatibilitySignals(t *testing.T) {
	inputLines := []string{
		"intro line 1",
		"intro line 2",
		"intro line 3",
		"intro line 4",
		"intro line 5",
		"intro line 6",
		"intro line 7",
		"intro line 8",
		"intro line 9",
		"intro line 10",
		"intro line 11",
		"intro line 12",
		"random setup text",
		"another non-matrix line",
		"Tested versions",
		"Argo CD version | Kubernetes versions",
		"3.3 | v1.34, v1.33, v1.32, v1.31",
	}

	got := pruneEvidenceText(strings.Join(inputLines, "\n"), 4000, 16)
	if !strings.Contains(got, "v1.31") {
		t.Fatalf("expected late strong compatibility line to be included, got %q", got)
	}
}

func TestIsTransientLLMError_ClassifiesRetryable(t *testing.T) {
	if !isTransientLLMError(errors.New("unexpected EOF")) {
		t.Fatalf("expected EOF-like error to be retryable")
	}
	if !isTransientLLMError(errors.New("HTTP 429 resource_exhausted")) {
		t.Fatalf("expected 429 error to be retryable")
	}
	if isTransientLLMError(errors.New("invalid request body")) {
		t.Fatalf("expected terminal error to be non-retryable")
	}
}

// --- Stored data resolution tests ---

func TestNormalizeK8sVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"1.30", "1.30"},
		{"v1.30", "1.30"},
		{"1.30.2", "1.30"},
		{"v1.28.5", "1.28"},
		{"1", "1"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeK8sVersion(tt.input)
			if got != tt.want {
				t.Errorf("normalizeK8sVersion(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCompareK8sVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.30", "1.30", 0},
		{"1.31", "1.30", 1},
		{"1.29", "1.30", -1},
		{"2.0", "1.30", 1},
		{"1.0", "2.0", -1},
	}
	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			got := compareK8sVersions(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("compareK8sVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestCompareK8sVersions_MultiDigitMajor(t *testing.T) {
	if got := compareK8sVersions("10.1", "2.9"); got != 1 {
		t.Fatalf("compareK8sVersions(10.1, 2.9) = %d, want 1", got)
	}
	if got := compareK8sVersions("2.9", "10.1"); got != -1 {
		t.Fatalf("compareK8sVersions(2.9, 10.1) = %d, want -1", got)
	}
}

func TestMatrixKeyMatchesInstalledVersion_CaseInsensitiveWildcard(t *testing.T) {
	if !matrixKeyMatchesInstalledVersion("1.29.X", "1.29.3") {
		t.Fatal("expected mixed-case wildcard key to match installed version")
	}
}

func TestFindDirectMatrixCompatibilityMatch_PrefersMostSpecificDeterministically(t *testing.T) {
	matrix := map[string][]string{
		"1.15.x": {"1.31"},
		"1.15":   {"1.30"},
	}
	matchedKey, matchedVersions, found := findDirectMatrixCompatibilityMatch(matrix, "1.15.2")
	if !found {
		t.Fatal("expected a matching matrix key")
	}
	if matchedKey != "1.15" {
		t.Fatalf("expected key 1.15, got %q", matchedKey)
	}
	if len(matchedVersions) != 1 || matchedVersions[0] != "1.30" {
		t.Fatalf("unexpected matched versions: %#v", matchedVersions)
	}
}

func TestResolveFromStoredData_FullMatrix_Compatible(t *testing.T) {
	info := addonWithInfo{
		DetectedAddon: cluster.DetectedAddon{
			Name:      "cert-manager",
			Namespace: "cert-manager",
			Version:   "v1.15.0",
		},
		DBMatch: &addon.Addon{
			Name: "cert-manager",
			KubernetesCompatibility: map[string][]string{
				"1.15": {"1.28", "1.29", "1.30", "1.31"},
				"1.14": {"1.27", "1.28", "1.29", "1.30"},
			},
		},
	}

	result := resolveFromStoredData(info, "1.30")
	if result.Compatible != output.StatusTrue {
		t.Errorf("expected StatusTrue, got %q", result.Compatible)
	}
	if result.DataSource != output.DataSourceStored {
		t.Errorf("expected data_source=stored, got %q", result.DataSource)
	}
}

func TestResolveFromStoredData_FullMatrix_Incompatible(t *testing.T) {
	info := addonWithInfo{
		DetectedAddon: cluster.DetectedAddon{
			Name:      "cert-manager",
			Namespace: "cert-manager",
			Version:   "v1.15.0",
		},
		DBMatch: &addon.Addon{
			Name: "cert-manager",
			KubernetesCompatibility: map[string][]string{
				"1.15": {"1.28", "1.29", "1.30"},
			},
		},
	}

	result := resolveFromStoredData(info, "1.32")
	if result.Compatible != output.StatusFalse {
		t.Errorf("expected StatusFalse, got %q", result.Compatible)
	}
}

func TestResolveFromStoredData_FullMatrix_VersionNotInMatrix(t *testing.T) {
	info := addonWithInfo{
		DetectedAddon: cluster.DetectedAddon{
			Name:      "cert-manager",
			Namespace: "cert-manager",
			Version:   "v9.99.0",
		},
		DBMatch: &addon.Addon{
			Name: "cert-manager",
			KubernetesCompatibility: map[string][]string{
				"1.15": {"1.28", "1.29", "1.30"},
			},
		},
	}

	result := resolveFromStoredData(info, "1.30")
	if result.Compatible != output.StatusUnknown {
		t.Errorf("expected StatusUnknown, got %q", result.Compatible)
	}
}

func TestResolveFromStoredData_FullMatrix_ConstraintStyleThreshold_Compatible(t *testing.T) {
	info := addonWithInfo{
		DetectedAddon: cluster.DetectedAddon{
			Name:      "karpenter",
			Namespace: "karpenter",
			Version:   "v1.8.1",
		},
		DBMatch: &addon.Addon{
			Name: "karpenter",
			KubernetesCompatibility: map[string][]string{
				"1.9.x": {"1.29", "1.30", "1.31", "1.32", "1.33", "1.34", "1.35"},
				"1.6.x": {"1.29", "1.30", "1.31", "1.32", "1.33", "1.34"},
				"1.5.x": {"1.29", "1.30", "1.31", "1.32", "1.33"},
				"1.2.x": {"1.29", "1.30", "1.31", "1.32"},
				"1.0.5": {"1.29", "1.30", "1.31"},
				"0.37":  {"1.29", "1.30"},
				"0.34":  {"1.29"},
			},
		},
	}

	result := resolveFromStoredData(info, "1.31")
	if result.Compatible != output.StatusTrue {
		t.Errorf("expected StatusTrue, got %q (note: %q)", result.Compatible, result.Note)
	}
}

func TestResolveFromStoredData_FullMatrix_ConstraintStyleThreshold_Incompatible(t *testing.T) {
	info := addonWithInfo{
		DetectedAddon: cluster.DetectedAddon{
			Name:      "karpenter",
			Namespace: "karpenter",
			Version:   "v1.8.1",
		},
		DBMatch: &addon.Addon{
			Name: "karpenter",
			KubernetesCompatibility: map[string][]string{
				"1.9.x": {"1.29", "1.30", "1.31", "1.32", "1.33", "1.34", "1.35"},
				"1.6.x": {"1.29", "1.30", "1.31", "1.32", "1.33", "1.34"},
				"1.5.x": {"1.29", "1.30", "1.31", "1.32", "1.33"},
				"1.2.x": {"1.29", "1.30", "1.31", "1.32"},
				"1.0.5": {"1.29", "1.30", "1.31"},
				"0.37":  {"1.29", "1.30"},
				"0.34":  {"1.29"},
			},
		},
	}

	result := resolveFromStoredData(info, "1.35")
	if result.Compatible != output.StatusUnknown {
		t.Errorf("expected StatusUnknown, got %q (note: %q)", result.Compatible, result.Note)
	}
}

func TestResolveFromStoredData_MinVersion_Compatible(t *testing.T) {
	info := addonWithInfo{
		DetectedAddon: cluster.DetectedAddon{
			Name:      "karpenter",
			Namespace: "karpenter",
			Version:   "v0.35.0",
		},
		DBMatch: &addon.Addon{
			Name:                 "karpenter",
			KubernetesMinVersion: "1.23",
		},
	}

	result := resolveFromStoredData(info, "1.30")
	if result.Compatible != output.StatusTrue {
		t.Errorf("expected StatusTrue, got %q", result.Compatible)
	}
}

func TestResolveFromStoredData_MinVersion_Incompatible(t *testing.T) {
	info := addonWithInfo{
		DetectedAddon: cluster.DetectedAddon{
			Name:      "karpenter",
			Namespace: "karpenter",
			Version:   "v0.35.0",
		},
		DBMatch: &addon.Addon{
			Name:                 "karpenter",
			KubernetesMinVersion: "1.30",
		},
	}

	result := resolveFromStoredData(info, "1.28")
	if result.Compatible != output.StatusFalse {
		t.Errorf("expected StatusFalse, got %q", result.Compatible)
	}
}

func TestFindLatestCompatibleVersion(t *testing.T) {
	matrix := map[string][]string{
		"1.15": {"1.28", "1.29", "1.30"},
		"1.14": {"1.27", "1.28", "1.29"},
		"1.13": {"1.26", "1.27", "1.28"},
	}

	got := findLatestCompatibleVersion(matrix, "1.28")
	// All three versions support 1.28, so latest alphabetically is "1.15"
	if got != "1.15" {
		t.Errorf("findLatestCompatibleVersion(1.28) = %q, want %q", got, "1.15")
	}

	got = findLatestCompatibleVersion(matrix, "1.30")
	if got != "1.15" {
		t.Errorf("findLatestCompatibleVersion(1.30) = %q, want %q", got, "1.15")
	}

	got = findLatestCompatibleVersion(matrix, "1.35")
	if got != "" {
		t.Errorf("findLatestCompatibleVersion(1.35) = %q, want empty", got)
	}
}

func TestFindLatestCompatibleVersion_UsesNumericVersionOrdering(t *testing.T) {
	matrix := map[string][]string{
		"1.9":  {"1.31"},
		"1.10": {"1.31"},
		"1.8":  {"1.31"},
	}

	got := findLatestCompatibleVersion(matrix, "1.31")
	if got != "1.10" {
		t.Errorf("findLatestCompatibleVersion(1.31) = %q, want %q", got, "1.10")
	}
}

func TestTruncateToValidUTF8Prefix_RespectsRuneBoundary(t *testing.T) {
	text := "hello 😀 world"
	got := truncateToValidUTF8Prefix(text, 8) // truncates inside emoji byte sequence if naive
	if !utf8.ValidString(got) {
		t.Fatalf("truncateToValidUTF8Prefix() returned invalid UTF-8: %q", got)
	}
	if got != "hello " {
		t.Fatalf("truncateToValidUTF8Prefix() = %q, want %q", got, "hello ")
	}
}

func TestResolveFromStoredData_DeterministicKeyMatching(t *testing.T) {
	info := addonWithInfo{
		DetectedAddon: cluster.DetectedAddon{
			Name:      "knative",
			Namespace: "knative-serving",
			Version:   "v1.15.0",
		},
		DBMatch: &addon.Addon{
			Name: "knative",
			KubernetesCompatibility: map[string][]string{
				"1.1":  {"1.20"},
				"1.15": {"1.28"},
			},
		},
	}

	// Run multiple times to verify determinism — with unsorted map iteration
	// "1.1" could prefix-match "1.15.0" non-deterministically. The fix should
	// always pick "1.15" as the most specific match.
	for i := 0; i < 20; i++ {
		result := resolveFromStoredData(info, "1.28")
		if result.Compatible != output.StatusTrue {
			t.Fatalf("run %d: expected StatusTrue, got %q (note: %s)", i, result.Compatible, result.Note)
		}
		if !strings.Contains(result.Note, "1.15") {
			t.Fatalf("run %d: expected note to mention key '1.15', got: %s", i, result.Note)
		}
	}
}

func TestResolveFromStoredData_MaxVersion_Compatible(t *testing.T) {
	info := addonWithInfo{
		DetectedAddon: cluster.DetectedAddon{
			Name:      "old-addon",
			Namespace: "default",
			Version:   "v1.0.0",
		},
		DBMatch: &addon.Addon{
			Name:                 "old-addon",
			KubernetesMinVersion: "1.20",
			KubernetesMaxVersion: "1.28",
		},
	}

	result := resolveFromStoredData(info, "1.25")
	if result.Compatible != output.StatusTrue {
		t.Errorf("expected StatusTrue, got %q (note: %s)", result.Compatible, result.Note)
	}
}

func TestResolveFromStoredData_MaxVersion_TooNew(t *testing.T) {
	info := addonWithInfo{
		DetectedAddon: cluster.DetectedAddon{
			Name:      "old-addon",
			Namespace: "default",
			Version:   "v1.0.0",
		},
		DBMatch: &addon.Addon{
			Name:                 "old-addon",
			KubernetesMinVersion: "1.20",
			KubernetesMaxVersion: "1.28",
		},
	}

	result := resolveFromStoredData(info, "1.30")
	if result.Compatible != output.StatusFalse {
		t.Errorf("expected StatusFalse, got %q (note: %s)", result.Compatible, result.Note)
	}
}

func TestResolveFromStoredData_MaxVersionOnly(t *testing.T) {
	info := addonWithInfo{
		DetectedAddon: cluster.DetectedAddon{
			Name:      "old-addon",
			Namespace: "default",
			Version:   "v1.0.0",
		},
		DBMatch: &addon.Addon{
			Name:                 "old-addon",
			KubernetesMaxVersion: "1.26",
		},
	}

	result := resolveFromStoredData(info, "1.25")
	if result.Compatible != output.StatusTrue {
		t.Errorf("expected StatusTrue for K8s 1.25 <= max 1.26, got %q", result.Compatible)
	}

	result = resolveFromStoredData(info, "1.27")
	if result.Compatible != output.StatusFalse {
		t.Errorf("expected StatusFalse for K8s 1.27 > max 1.26, got %q", result.Compatible)
	}
}

func TestResolveFromStoredData_MatrixFallbackToMinVersion(t *testing.T) {
	info := addonWithInfo{
		DetectedAddon: cluster.DetectedAddon{
			Name:      "cert-manager",
			Namespace: "cert-manager",
			Version:   "v99.0.0",
		},
		DBMatch: &addon.Addon{
			Name: "cert-manager",
			KubernetesCompatibility: map[string][]string{
				"1.15": {"1.28", "1.29", "1.30"},
			},
			KubernetesMinVersion: "1.20",
		},
	}

	// Installed version v99.0.0 won't match any matrix key.
	// Should fall back to kubernetes_min_version instead of returning unknown.
	result := resolveFromStoredData(info, "1.25")
	if result.Compatible != output.StatusTrue {
		t.Errorf("expected fallback to min_version -> StatusTrue, got %q (note: %s)", result.Compatible, result.Note)
	}
}

func TestMatrixKeyMatchesInstalledVersion_SkipsNonSemver(t *testing.T) {
	nonSemverKeys := []string{"master", "HEAD", "main", "latest", "cis-1.6", "cis-1.11", "≥0.18.x", "≤0.9.x"}
	for _, key := range nonSemverKeys {
		if matrixKeyMatchesInstalledVersion(key, "1.0.0") {
			t.Errorf("non-semver key %q should never match, but it did", key)
		}
		if matrixKeyMatchesInstalledVersion(key, "0.18.0") {
			t.Errorf("non-semver key %q should never match, but it did", key)
		}
	}
}

func TestMatrixKeyMatchesInstalledVersion_VersionRange(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		installed string
		want      bool
	}{
		{"in full range", "v2.0.0-v2.1.3", "2.1.0", true},
		{"at range low bound", "v2.0.0-v2.1.3", "2.0.0", true},
		{"at range high bound", "v2.0.0-v2.1.3", "2.1.3", true},
		{"below range", "v2.0.0-v2.1.3", "1.9.0", false},
		{"above range", "v2.0.0-v2.1.3", "2.2.0", false},
		{"kots range match", "1.124.17-1.128.3", "1.125.0", true},
		{"kots range below", "1.124.17-1.128.3", "1.124.16", false},
		{"kots range above", "1.124.17-1.128.3", "1.128.4", false},
		{"clusternet range", "v0.6.0-v0.12.0", "0.9.0", true},
		{"clusternet at high", "v0.6.0-v0.12.0", "0.12.0", true},
		{"clusternet above", "v0.6.0-v0.12.0", "0.13.0", false},
		{"not a range without dots", "0.3.4-7", "0.3.5", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matrixKeyMatchesInstalledVersion(tt.key, tt.installed)
			if got != tt.want {
				t.Errorf("matrixKeyMatchesInstalledVersion(%q, %q) = %v, want %v", tt.key, tt.installed, got, tt.want)
			}
		})
	}
}

func TestMatrixKeyMatchesInstalledVersion_ExistingBehavior(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		installed string
		want      bool
	}{
		{"exact match", "1.15", "1.15", true},
		{"prefix match", "1.15", "1.15.0", true},
		{"prefix with patch", "1.15", "1.15.3", true},
		{"wildcard match", "1.9.x", "1.9.5", true},
		{"wildcard exact", "1.9.x", "1.9", true},
		{"no match", "1.15", "1.16.0", false},
		{"v prefix in key", "v1.15", "1.15.0", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matrixKeyMatchesInstalledVersion(tt.key, tt.installed)
			if got != tt.want {
				t.Errorf("matrixKeyMatchesInstalledVersion(%q, %q) = %v, want %v", tt.key, tt.installed, got, tt.want)
			}
		})
	}
}

func TestResolveFromStoredData_VersionRange(t *testing.T) {
	info := addonWithInfo{
		DetectedAddon: cluster.DetectedAddon{
			Name:      "aws-load-balancer-controller",
			Namespace: "kube-system",
			Version:   "v2.1.0",
		},
		DBMatch: &addon.Addon{
			Name: "AWS Load Balancer Controller",
			KubernetesCompatibility: map[string][]string{
				"v2.0.0-v2.1.3": {"1.16", "1.17", "1.18", "1.19", "1.20", "1.21"},
				"v2.4.0-v2.4.7": {"1.22", "1.23", "1.24", "1.25"},
			},
		},
	}

	result := resolveFromStoredData(info, "1.19")
	if result.Compatible != output.StatusTrue {
		t.Errorf("expected StatusTrue for v2.1.0 in range v2.0.0-v2.1.3 on K8s 1.19, got %q (note: %s)", result.Compatible, result.Note)
	}

	result = resolveFromStoredData(info, "1.25")
	if result.Compatible != output.StatusFalse {
		t.Errorf("expected StatusFalse for v2.1.0 on K8s 1.25 (not in range's K8s list), got %q (note: %s)", result.Compatible, result.Note)
	}
}

func TestResolveFromStoredData_NonSemverKeysSkipped(t *testing.T) {
	info := addonWithInfo{
		DetectedAddon: cluster.DetectedAddon{
			Name:      "istio",
			Namespace: "istio-system",
			Version:   "1.29.0",
		},
		DBMatch: &addon.Addon{
			Name: "Istio",
			KubernetesCompatibility: map[string][]string{
				"master": {"1.31", "1.32", "1.33", "1.34", "1.35"},
				"1.29":   {"1.31", "1.32", "1.33", "1.34", "1.35"},
				"1.28":   {"1.30", "1.31", "1.32", "1.33", "1.34"},
			},
		},
	}

	// Should match "1.29" not "master", even though both are in the map.
	result := resolveFromStoredData(info, "1.32")
	if result.Compatible != output.StatusTrue {
		t.Errorf("expected StatusTrue, got %q (note: %s)", result.Compatible, result.Note)
	}
	if strings.Contains(result.Note, "master") {
		t.Errorf("expected note to reference '1.29', not 'master': %s", result.Note)
	}
}

func TestResolveFromStoredData_ThresholdPlusKey(t *testing.T) {
	info := addonWithInfo{
		DetectedAddon: cluster.DetectedAddon{
			Name:      "aws-load-balancer-controller",
			Namespace: "kube-system",
			Version:   "v2.11.0",
		},
		DBMatch: &addon.Addon{
			Name:                   "AWS Load Balancer Controller",
			CompatibilityMatrixURL: "https://example.com/alb-compatibility",
			KubernetesCompatibility: map[string][]string{
				"v2.5.0+": {"1.22", "1.23", "1.24", "1.25", "1.26", "1.27", "1.28", "1.29", "1.30", "1.31", "1.32", "1.33", "1.34"},
			},
		},
	}

	result := resolveFromStoredData(info, "1.31")
	if result.Compatible != output.StatusTrue {
		t.Fatalf("expected StatusTrue for + threshold key, got %q (note: %s)", result.Compatible, result.Note)
	}
	if !strings.Contains(result.Note, "Source: https://example.com/alb-compatibility") {
		t.Fatalf("expected stored note to include source URL, got: %s", result.Note)
	}
}

func TestResolveLocalOnly_ProducesUnknownWithLocalSource(t *testing.T) {
	addons := []addonWithInfo{
		{
			DetectedAddon: cluster.DetectedAddon{
				Name:      "goldilocks",
				Namespace: "kube-system",
				Version:   "8.0.0",
			},
			DBMatch: &addon.Addon{
				Name:                   "Goldilocks",
				CompatibilityMatrixURL: "https://example.com/docs/compat",
			},
			EOLData: []addon.EOLCycle{
				{Cycle: "8.0", Latest: "v8.0.2", EOL: false, ReleaseDate: "2025-01-15"},
			},
		},
	}

	results := resolveLocalOnly(addons, "1.30")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Compatible != output.StatusUnknown {
		t.Errorf("expected compatible=unknown, got %q", r.Compatible)
	}
	if r.DataSource != output.DataSourceLocal {
		t.Errorf("expected data_source=local, got %q", r.DataSource)
	}
	if !strings.Contains(r.Note, "LLM analysis not configured") {
		t.Errorf("expected note to contain 'LLM analysis not configured', got %q", r.Note)
	}
	if !strings.Contains(r.Note, "v8.0.2") {
		t.Errorf("expected note to contain latest release version, got %q", r.Note)
	}
	if !strings.Contains(r.Note, "cycle 8.0") {
		t.Errorf("expected note to contain cycle info, got %q", r.Note)
	}
	if !strings.Contains(r.Note, "https://example.com/docs/compat") {
		t.Errorf("expected note to contain source URL, got %q", r.Note)
	}
	if r.LatestCompatibleVersion != "" {
		t.Errorf("expected empty latest_compatible_version, got %q", r.LatestCompatibleVersion)
	}
}

func TestResolveLocalOnly_MinimalAddon(t *testing.T) {
	addons := []addonWithInfo{
		{
			DetectedAddon: cluster.DetectedAddon{
				Name:      "unknown-addon",
				Namespace: "default",
				Version:   "1.0.0",
			},
			DBMatch: &addon.Addon{
				Name: "unknown-addon",
			},
		},
	}

	results := resolveLocalOnly(addons, "1.30")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Compatible != output.StatusUnknown {
		t.Errorf("expected compatible=unknown, got %q", r.Compatible)
	}
	if r.DataSource != output.DataSourceLocal {
		t.Errorf("expected data_source=local, got %q", r.DataSource)
	}
	if r.Note != "LLM analysis not configured" {
		t.Errorf("expected minimal note, got %q", r.Note)
	}
	if r.Name != "unknown-addon" {
		t.Errorf("expected name=unknown-addon, got %q", r.Name)
	}
	if r.InstalledVersion != "1.0.0" {
		t.Errorf("expected installed_version=1.0.0, got %q", r.InstalledVersion)
	}
}

func TestResolveFromStoredData_PatchLineCompatibility(t *testing.T) {
	info := addonWithInfo{
		DetectedAddon: cluster.DetectedAddon{
			Name:      "coredns",
			Namespace: "kube-system",
			Version:   "v1.11.4-eksbuild.28",
		},
		DBMatch: &addon.Addon{
			Name: "CoreDNS",
			KubernetesCompatibility: map[string][]string{
				"v1.11.3": {"1.31", "1.32"},
			},
		},
	}

	result := resolveFromStoredData(info, "1.31")
	if result.Compatible != output.StatusTrue {
		t.Fatalf("expected StatusTrue for compatible patch line, got %q (note: %s)", result.Compatible, result.Note)
	}
}
