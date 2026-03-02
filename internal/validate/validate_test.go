package validate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/qbandev/kaddons/internal/addon"
)

// --- Migrated from linkcheck_test.go ---

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func httpResponse(statusCode int) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
	}
}

func TestCheckURL_Success(t *testing.T) {
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return httpResponse(http.StatusOK), nil
		}),
	}
	if got := checkURL(context.Background(), client, "https://docs.example.com/page", ""); got != "ok" {
		t.Errorf("checkURL(200) = %q, want %q", got, "ok")
	}
}

func TestCheckURL_NotFound(t *testing.T) {
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return httpResponse(http.StatusNotFound), nil
		}),
	}
	if got := checkURL(context.Background(), client, "https://docs.example.com/page", ""); got != "HTTP 404" {
		t.Errorf("checkURL(404) = %q, want %q", got, "HTTP 404")
	}
}

func TestCheckURL_ServerError(t *testing.T) {
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return httpResponse(http.StatusInternalServerError), nil
		}),
	}
	if got := checkURL(context.Background(), client, "https://docs.example.com/page", ""); got != "HTTP 500" {
		t.Errorf("checkURL(500) = %q, want %q", got, "HTTP 500")
	}
}

func TestCheckURL_HeadRejectedFallsBackToGet(t *testing.T) {
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodHead {
				return httpResponse(http.StatusMethodNotAllowed), nil
			}
			return httpResponse(http.StatusOK), nil
		}),
	}
	if got := checkURL(context.Background(), client, "https://docs.example.com/page", ""); got != "ok" {
		t.Errorf("checkURL(HEAD 405 → GET 200) = %q, want %q", got, "ok")
	}
}

func TestCheckURL_ForbiddenFallsBackToGet(t *testing.T) {
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodHead {
				return httpResponse(http.StatusForbidden), nil
			}
			return httpResponse(http.StatusOK), nil
		}),
	}
	if got := checkURL(context.Background(), client, "https://docs.example.com/page", ""); got != "ok" {
		t.Errorf("checkURL(HEAD 403 → GET 200) = %q, want %q", got, "ok")
	}
}

func TestCheckURL_FallbackGetAlsoFails(t *testing.T) {
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodHead {
				return httpResponse(http.StatusMethodNotAllowed), nil
			}
			return httpResponse(http.StatusNotFound), nil
		}),
	}
	if got := checkURL(context.Background(), client, "https://docs.example.com/page", ""); got != "HTTP 404" {
		t.Errorf("checkURL(HEAD 405 → GET 404) = %q, want %q", got, "HTTP 404")
	}
}

func TestCheckURL_ConnectionError(t *testing.T) {
	client := &http.Client{
		Timeout: 1 * time.Second,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, context.DeadlineExceeded
		}),
	}
	got := checkURL(context.Background(), client, "https://docs.example.com/page", "")
	if got == "ok" {
		t.Error("checkURL(unreachable) should not return ok")
	}
}

func TestCheckURL_UnsupportedScheme(t *testing.T) {
	client := &http.Client{Timeout: 5 * time.Second}
	got := checkURL(context.Background(), client, "http://docs.example.com/page", "")
	if got == "ok" {
		t.Error("checkURL(unsupported scheme) should not return ok")
	}
}

func TestCheckURL_SetsUserAgent(t *testing.T) {
	var gotUA string
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotUA = req.Header.Get("User-Agent")
			return httpResponse(http.StatusOK), nil
		}),
	}
	checkURL(context.Background(), client, "https://docs.example.com/page", "")
	if gotUA != "kaddons-validate/1.0" {
		t.Errorf("User-Agent = %q, want %q", gotUA, "kaddons-validate/1.0")
	}
}

// --- Migrated from auditmatrix_test.go ---

func TestHasK8sMatrix_WithFullMatrix(t *testing.T) {
	page := "Compatibility matrix: supported Kubernetes versions: Kubernetes 1.28, Kubernetes 1.29"
	if !hasK8sMatrix(page) {
		t.Error("expected true for page with full compatibility matrix")
	}
}

func TestHasK8sMatrix_WithVersionSupportKeyword(t *testing.T) {
	page := "Version support: Kubernetes 1.30 is supported"
	if !hasK8sMatrix(page) {
		t.Error("expected true for page with version support keyword")
	}
}

func TestHasK8sMatrix_K8sCompatibility(t *testing.T) {
	page := "k8s compatibility: k8s 1.28 and k8s 1.29"
	if !hasK8sMatrix(page) {
		t.Error("expected true for page with k8s compatibility")
	}
}

func TestHasK8sMatrix_OnlyVersionNoKeyword(t *testing.T) {
	page := "Install on Kubernetes 1.28 using helm install"
	if hasK8sMatrix(page) {
		t.Error("expected false for page with version but no matrix keyword")
	}
}

func TestHasK8sMatrix_OnlyKeywordNoVersion(t *testing.T) {
	page := "See our compatibility matrix for details"
	if hasK8sMatrix(page) {
		t.Error("expected false for page with keyword but no version")
	}
}

func TestHasK8sMatrix_NoMatch(t *testing.T) {
	page := "This is a general product overview page"
	if hasK8sMatrix(page) {
		t.Error("expected false for generic page")
	}
}

func TestHasK8sMatrix_EmptyPage(t *testing.T) {
	if hasK8sMatrix("") {
		t.Error("expected false for empty page")
	}
}

// --- ClassifyK8sMatrix tiered tests ---

func TestClassifyK8sMatrix_Strict(t *testing.T) {
	tests := []struct {
		name string
		page string
	}{
		{"full matrix", "Compatibility matrix: supported Kubernetes versions: Kubernetes 1.28, Kubernetes 1.29"},
		{"version support", "Version support: Kubernetes 1.30 is supported"},
		{"k8s compat", "k8s compatibility: k8s 1.28 and k8s 1.29"},
		{"v-prefix strict", "Supported versions: Kubernetes v1.28, v1.29, v1.30"},
		{"reversed order strict", "Compatibility matrix: v1.28 Kubernetes and v1.29 Kubernetes are supported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyK8sMatrix(tt.page); got != matrixTierStrict {
				t.Errorf("ClassifyK8sMatrix() = %q, want %q", got, matrixTierStrict)
			}
		})
	}
}

func TestClassifyK8sMatrix_Partial(t *testing.T) {
	tests := []struct {
		name string
		page string
	}{
		{"requirements keyword", "Requirements: Kubernetes cluster running v1.25 or later"},
		{"prerequisites keyword", "Prerequisites: a Kubernetes 1.26+ cluster is required"},
		{"tested with", "Tested with Kubernetes clusters version 1.27 and 1.28"},
		{"requires k8s", "Requires Kubernetes >= 1.22 with CSI support"},
		{"platform requirements", "Platform requirements: Kubernetes v1.24 minimum"},
		{"compatible with", "Compatible with Kubernetes versions from 1.25 to 1.30"},
		{"loose distance", "This addon runs on Kubernetes. Check the requirements for version 1.28 or newer."},
		{"newline and reversed order", "Requirements:\nv1.28 is validated on Kubernetes clusters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyK8sMatrix(tt.page); got != matrixTierPartial {
				t.Errorf("ClassifyK8sMatrix() = %q, want %q", got, matrixTierPartial)
			}
		})
	}
}

func TestClassifyK8sMatrix_None(t *testing.T) {
	tests := []struct {
		name string
		page string
	}{
		{"no k8s mention", "Install using helm install my-chart"},
		{"version but no keyword", "Install on Kubernetes 1.28 using helm install"},
		{"keyword but no version", "See our compatibility matrix for details"},
		{"empty page", ""},
		{"generic overview", "This is a general product overview page"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyK8sMatrix(tt.page); got != matrixTierNone {
				t.Errorf("ClassifyK8sMatrix() = %q, want %q", got, matrixTierNone)
			}
		})
	}
}

// --- New tests: aggregation and flag logic ---

func TestAggregation_SharedURL(t *testing.T) {
	addons := []addon.Addon{
		{Name: "addon-a", ProjectURL: "https://example.com/shared"},
		{Name: "addon-b", ProjectURL: "https://example.com/shared"},
	}

	tasks := harvest(addons)

	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	task := tasks["https://example.com/shared"]
	if task == nil {
		t.Fatal("expected task for shared URL")
	}

	if len(task.consumers) != 2 {
		t.Errorf("expected 2 consumers, got %d", len(task.consumers))
	}

	if task.needsContent {
		t.Error("expected needsContent=false for project_url-only consumers")
	}
}

func TestAggregation_MatrixAndLink(t *testing.T) {
	addons := []addon.Addon{
		{Name: "addon-a", Repository: "https://example.com/repo"},
		{Name: "addon-b", CompatibilityMatrixURL: "https://example.com/repo"},
	}

	tasks := harvest(addons)

	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	task := tasks["https://example.com/repo"]
	if task == nil {
		t.Fatal("expected task for shared URL")
	}

	if !task.needsContent {
		t.Error("expected needsContent=true when URL is used as compatibility_matrix_url")
	}

	if len(task.consumers) != 2 {
		t.Errorf("expected 2 consumers, got %d", len(task.consumers))
	}
}

func TestReporting_MixedConsumers(t *testing.T) {
	tasks := map[string]*urlTask{
		"https://example.com/page": {
			url:          "https://example.com/page",
			needsContent: true,
			consumers: []consumer{
				{addonName: "addon-a", field: "repository"},
				{addonName: "addon-b", field: "compatibility_matrix_url"},
			},
		},
	}

	results := map[string]*urlResult{
		"https://example.com/page": {
			reachable:  true,
			matrixTier: matrixTierNone,
		},
	}

	// report() returns ErrValidationFailed when problems exist (no os.Exit).
	err := report(tasks, results, false)
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed, got %v", err)
	}
}

func TestReporting_AllPassing(t *testing.T) {
	tasks := map[string]*urlTask{
		"https://example.com/page": {
			url:          "https://example.com/page",
			needsContent: false,
			consumers: []consumer{
				{addonName: "addon-a", field: "repository"},
			},
		},
	}

	results := map[string]*urlResult{
		"https://example.com/page": {
			reachable: true,
		},
	}

	err := report(tasks, results, false)
	if err != nil {
		t.Fatalf("expected nil error for passing checks, got %v", err)
	}
}

func TestReporting_MissingResultHandledAsFailure(t *testing.T) {
	tasks := map[string]*urlTask{
		"https://example.com/page": {
			url:          "https://example.com/page",
			needsContent: false,
			consumers: []consumer{
				{addonName: "addon-a", field: "repository"},
			},
		},
	}

	err := report(tasks, map[string]*urlResult{}, false)
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed for missing result, got %v", err)
	}
}

func TestLinksOnlyFlag(t *testing.T) {
	addons := []addon.Addon{
		{Name: "addon-a", CompatibilityMatrixURL: "https://example.com/matrix"},
		{Name: "addon-b", ProjectURL: "https://example.com/project"},
	}

	tasks := harvest(addons)
	applyFlags(tasks, true, false)

	for _, task := range tasks {
		if task.needsContent {
			t.Errorf("expected needsContent=false after --links flag, got true for %s", task.url)
		}
	}

	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks (--links keeps all URLs), got %d", len(tasks))
	}
}

// --- validateStoredData tests ---

func TestValidateStoredData_ValidFullMatrix(t *testing.T) {
	addons := []addon.Addon{
		{
			Name: "cert-manager",
			KubernetesCompatibility: map[string][]string{
				"1.15": {"1.28", "1.29", "1.30"},
			},
		},
	}
	problems := validateStoredData(addons)
	if len(problems) != 0 {
		t.Fatalf("expected 0 problems, got %d: %+v", len(problems), problems)
	}
}

func TestValidateStoredData_ValidMinVersion(t *testing.T) {
	addons := []addon.Addon{
		{
			Name:                 "karpenter",
			KubernetesMinVersion: "1.23",
		},
	}
	problems := validateStoredData(addons)
	if len(problems) != 0 {
		t.Fatalf("expected 0 problems, got %d: %+v", len(problems), problems)
	}
}

func TestValidateStoredData_InvalidMinVersion(t *testing.T) {
	addons := []addon.Addon{
		{
			Name:                 "bad-addon",
			KubernetesMinVersion: "v1.23",
		},
	}
	problems := validateStoredData(addons)
	if len(problems) != 1 {
		t.Fatalf("expected 1 problem, got %d: %+v", len(problems), problems)
	}
	if problems[0].field != "kubernetes_min_version" {
		t.Errorf("expected field kubernetes_min_version, got %q", problems[0].field)
	}
}

func TestValidateStoredData_InvalidK8sVersionInMatrix(t *testing.T) {
	addons := []addon.Addon{
		{
			Name: "bad-matrix",
			KubernetesCompatibility: map[string][]string{
				"1.5": {"1.28", "v1.29", "latest"},
			},
		},
	}
	problems := validateStoredData(addons)
	if len(problems) != 2 {
		t.Fatalf("expected 2 problems, got %d: %+v", len(problems), problems)
	}
}

func TestValidateStoredData_EmptyKey(t *testing.T) {
	addons := []addon.Addon{
		{
			Name: "empty-key",
			KubernetesCompatibility: map[string][]string{
				"": {"1.28"},
			},
		},
	}
	problems := validateStoredData(addons)
	if len(problems) != 1 {
		t.Fatalf("expected 1 problem, got %d: %+v", len(problems), problems)
	}
	if problems[0].reason != "addon version key must be non-empty" {
		t.Errorf("unexpected reason: %q", problems[0].reason)
	}
}

func TestValidateStoredData_EmptyVersionList(t *testing.T) {
	addons := []addon.Addon{
		{
			Name: "empty-versions",
			KubernetesCompatibility: map[string][]string{
				"1.5": {},
			},
		},
	}
	problems := validateStoredData(addons)
	if len(problems) != 1 {
		t.Fatalf("expected 1 problem, got %d: %+v", len(problems), problems)
	}
	if problems[0].reason != "K8s version list must be non-empty" {
		t.Errorf("unexpected reason: %q", problems[0].reason)
	}
}

func TestValidateStoredData_NoStoredData(t *testing.T) {
	addons := []addon.Addon{
		{Name: "no-data"},
	}
	problems := validateStoredData(addons)
	if len(problems) != 0 {
		t.Fatalf("expected 0 problems, got %d: %+v", len(problems), problems)
	}
}

func TestValidateStoredData_UnsupportedKeysOnly(t *testing.T) {
	addons := []addon.Addon{
		{
			Name: "unsupported-only",
			KubernetesCompatibility: map[string][]string{
				"master": {"1.31"},
			},
		},
	}
	problems := validateStoredData(addons)
	if len(problems) != 1 {
		t.Fatalf("expected 1 problem, got %d: %+v", len(problems), problems)
	}
	if problems[0].reason != "matrix must contain at least one key format supported by stored resolver" {
		t.Fatalf("unexpected reason: %q", problems[0].reason)
	}
}

func TestValidateStoredData_MixedKeysAtLeastOneSupported(t *testing.T) {
	addons := []addon.Addon{
		{
			Name: "mixed-keys",
			KubernetesCompatibility: map[string][]string{
				"master": {"1.31"},
				"1.13.x": {"1.31"},
			},
		},
	}
	problems := validateStoredData(addons)
	if len(problems) != 0 {
		t.Fatalf("expected 0 problems, got %d: %+v", len(problems), problems)
	}
}

func TestValidateStoredData_SupportedRangeAndThresholdPlusKeys(t *testing.T) {
	addons := []addon.Addon{
		{
			Name: "advanced-keys",
			KubernetesCompatibility: map[string][]string{
				"v2.0.0-v2.1.3": {"1.31"},
				"v2.5.0+":       {"1.31"},
			},
		},
	}
	problems := validateStoredData(addons)
	if len(problems) != 0 {
		t.Fatalf("expected 0 problems, got %d: %+v", len(problems), problems)
	}
}

func TestValidateStoredData_ValidMaxVersion(t *testing.T) {
	addons := []addon.Addon{
		{
			Name:                 "old-addon",
			KubernetesMinVersion: "1.20",
			KubernetesMaxVersion: "1.28",
		},
	}
	problems := validateStoredData(addons)
	if len(problems) != 0 {
		t.Fatalf("expected 0 problems, got %d: %+v", len(problems), problems)
	}
}

func TestValidateStoredData_InvalidMaxVersion(t *testing.T) {
	addons := []addon.Addon{
		{
			Name:                 "bad-addon",
			KubernetesMaxVersion: "v1.28",
		},
	}
	problems := validateStoredData(addons)
	if len(problems) != 1 {
		t.Fatalf("expected 1 problem, got %d: %+v", len(problems), problems)
	}
	if problems[0].field != "kubernetes_max_version" {
		t.Errorf("expected field kubernetes_max_version, got %q", problems[0].field)
	}
}

func TestValidateStoredData_MinExceedsMax(t *testing.T) {
	addons := []addon.Addon{
		{
			Name:                 "inverted-addon",
			KubernetesMinVersion: "1.30",
			KubernetesMaxVersion: "1.20",
		},
	}
	problems := validateStoredData(addons)
	if len(problems) != 1 {
		t.Fatalf("expected 1 problem, got %d: %+v", len(problems), problems)
	}
	if problems[0].reason != "min version must not exceed max version" {
		t.Errorf("unexpected reason: %q", problems[0].reason)
	}
}

func TestValidateStoredData_MinExceedsMaxNumeric(t *testing.T) {
	// "1.28" < "1.9" lexicographically, but 1.28 > 1.9 numerically.
	addons := []addon.Addon{
		{
			Name:                 "numeric-edge",
			KubernetesMinVersion: "1.28",
			KubernetesMaxVersion: "1.9",
		},
	}
	problems := validateStoredData(addons)
	if len(problems) != 1 {
		t.Fatalf("expected 1 problem for min=1.28 > max=1.9, got %d: %+v", len(problems), problems)
	}
	if problems[0].reason != "min version must not exceed max version" {
		t.Errorf("unexpected reason: %q", problems[0].reason)
	}
}

func TestMatrixOnlyFlag(t *testing.T) {
	addons := []addon.Addon{
		{Name: "addon-a", CompatibilityMatrixURL: "https://example.com/matrix"},
		{Name: "addon-b", ProjectURL: "https://example.com/project"},
	}

	tasks := harvest(addons)
	applyFlags(tasks, false, true)

	if len(tasks) != 1 {
		t.Fatalf("expected 1 task after --matrix flag, got %d", len(tasks))
	}

	task := tasks["https://example.com/matrix"]
	if task == nil {
		t.Fatal("expected matrix URL task to remain")
	}
	if !task.needsContent {
		t.Error("expected needsContent=true for matrix task")
	}
}

// --- isGitHubURL tests ---

func TestIsGitHubURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://github.com/org/repo", true},
		{"https://github.com/org/repo/releases", true},
		{"https://raw.githubusercontent.com/org/repo/main/README.md", true},
		{"https://GITHUB.COM/org/repo", true},
		{"https://docs.example.com/page", false},
		{"https://fakegithub.com/evil", false},
		{"https://notgithub.com", false},
		{"not-a-url", false},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			if got := isGitHubURL(tt.url); got != tt.want {
				t.Errorf("isGitHubURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

// --- checkURL GitHub auth header tests ---

func TestCheckURL_SetsGitHubAuthHeader(t *testing.T) {
	var gotAuth string
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotAuth = req.Header.Get("Authorization")
			return httpResponse(http.StatusOK), nil
		}),
	}

	checkURL(context.Background(), client, "https://github.com/org/repo", "test-token-123")
	if gotAuth != "Bearer test-token-123" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer test-token-123")
	}

	gotAuth = ""
	checkURL(context.Background(), client, "https://docs.example.com/page", "test-token-123")
	if gotAuth != "" {
		t.Errorf("Authorization header for non-GitHub URL = %q, want empty", gotAuth)
	}

	gotAuth = ""
	checkURL(context.Background(), client, "https://github.com/org/repo", "")
	if gotAuth != "" {
		t.Errorf("Authorization header with empty token = %q, want empty", gotAuth)
	}
}

// --- redirect auth stripping tests ---

func TestRedirectStripsAuthOnCrossHost(t *testing.T) {
	// Simulate the CheckRedirect function from Run().
	checkRedirect := func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		original := via[0]
		sameOrigin := req.URL.Scheme == original.URL.Scheme &&
			req.URL.Hostname() == original.URL.Hostname() &&
			portOrDefault(req.URL) == portOrDefault(original.URL)
		if !sameOrigin || req.URL.Scheme != "https" {
			req.Header.Del("Authorization")
		}
		return nil
	}

	tests := []struct {
		name       string
		origURL    string
		redirURL   string
		wantAuth   bool
	}{
		{"same host HTTPS", "https://github.com/a", "https://github.com/b", true},
		{"same host explicit port", "https://github.com/a", "https://github.com:443/b", true},
		{"cross host", "https://github.com/a", "https://cdn.example.com/b", false},
		{"scheme downgrade", "https://github.com/a", "http://github.com/a", false},
		{"cross host and scheme", "https://github.com/a", "http://evil.com/b", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origReq, _ := http.NewRequest("GET", tt.origURL, nil)
			origReq.Header.Set("Authorization", "Bearer token")

			redirReq, _ := http.NewRequest("GET", tt.redirURL, nil)
			redirReq.Header.Set("Authorization", "Bearer token")

			err := checkRedirect(redirReq, []*http.Request{origReq})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			hasAuth := redirReq.Header.Get("Authorization") != ""
			if hasAuth != tt.wantAuth {
				t.Errorf("Authorization present = %v, want %v", hasAuth, tt.wantAuth)
			}
		})
	}
}

// --- classifyError tests ---

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name      string
		errMsg    string
		wantClass string
	}{
		{"HTTP 408", "HTTP 408", "transient"},
		{"HTTP 429", "HTTP 429", "transient"},
		{"HTTP 500", "HTTP 500", "transient"},
		{"HTTP 502", "HTTP 502", "transient"},
		{"HTTP 503", "HTTP 503", "transient"},
		{"timeout error", "error: context deadline exceeded", "transient"},
		{"connection refused", "error: dial tcp: connection refused", "transient"},
		{"connection reset", "error: read: connection reset by peer", "transient"},
		{"HTTP 404", "HTTP 404", "permanent"},
		{"HTTP 403", "HTTP 403", "permanent"},
		{"DNS error", "error: no such host", "permanent"},
		{"TLS error", "error: tls: certificate has expired", "permanent"},
		{"too many redirects", "error: too many redirects", "permanent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyError(tt.errMsg); got != tt.wantClass {
				t.Errorf("classifyError(%q) = %q, want %q", tt.errMsg, got, tt.wantClass)
			}
		})
	}
}

// --- report transient/permanent separation tests ---

func TestReport_SeparatesTransientAndPermanent(t *testing.T) {
	tasks := map[string]*urlTask{
		"https://github.com/org/repo": {
			url:          "https://github.com/org/repo",
			needsContent: false,
			consumers:    []consumer{{addonName: "addon-a", field: "repository"}},
		},
		"https://example.com/dead": {
			url:          "https://example.com/dead",
			needsContent: false,
			consumers:    []consumer{{addonName: "addon-b", field: "project_url"}},
		},
	}
	results := map[string]*urlResult{
		"https://github.com/org/repo": {reachable: false, reachError: "HTTP 429"},
		"https://example.com/dead":    {reachable: false, reachError: "HTTP 404"},
	}

	err := report(tasks, results, true)
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed, got %v", err)
	}
}

func TestReport_AllTransient(t *testing.T) {
	tasks := map[string]*urlTask{
		"https://github.com/org/repo": {
			url:          "https://github.com/org/repo",
			needsContent: false,
			consumers:    []consumer{{addonName: "addon-a", field: "repository"}},
		},
	}
	results := map[string]*urlResult{
		"https://github.com/org/repo": {reachable: false, reachError: "HTTP 429"},
	}

	err := report(tasks, results, true)
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed, got %v", err)
	}
}
