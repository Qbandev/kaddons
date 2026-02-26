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
