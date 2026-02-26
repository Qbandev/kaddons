package validate

import (
	"context"
	"errors"
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
	if got := checkURL(context.Background(), client, "https://docs.example.com/page"); got != "ok" {
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
	if got := checkURL(context.Background(), client, "https://docs.example.com/page"); got != "HTTP 404" {
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
	if got := checkURL(context.Background(), client, "https://docs.example.com/page"); got != "HTTP 500" {
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
	if got := checkURL(context.Background(), client, "https://docs.example.com/page"); got != "ok" {
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
	if got := checkURL(context.Background(), client, "https://docs.example.com/page"); got != "ok" {
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
	if got := checkURL(context.Background(), client, "https://docs.example.com/page"); got != "HTTP 404" {
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
	got := checkURL(context.Background(), client, "https://docs.example.com/page")
	if got == "ok" {
		t.Error("checkURL(unreachable) should not return ok")
	}
}

func TestCheckURL_UnsupportedScheme(t *testing.T) {
	client := &http.Client{Timeout: 5 * time.Second}
	got := checkURL(context.Background(), client, "http://docs.example.com/page")
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
	checkURL(context.Background(), client, "https://docs.example.com/page")
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

// --- classifyK8sMatrix tiered tests ---

func TestClassifyK8sMatrix_Strict(t *testing.T) {
	tests := []struct {
		name string
		page string
	}{
		{"full matrix", "Compatibility matrix: supported Kubernetes versions: Kubernetes 1.28, Kubernetes 1.29"},
		{"version support", "Version support: Kubernetes 1.30 is supported"},
		{"k8s compat", "k8s compatibility: k8s 1.28 and k8s 1.29"},
		{"v-prefix strict", "Supported versions: Kubernetes v1.28, v1.29, v1.30"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyK8sMatrix(tt.page); got != matrixTierStrict {
				t.Errorf("classifyK8sMatrix() = %q, want %q", got, matrixTierStrict)
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyK8sMatrix(tt.page); got != matrixTierPartial {
				t.Errorf("classifyK8sMatrix() = %q, want %q", got, matrixTierPartial)
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
			if got := classifyK8sMatrix(tt.page); got != matrixTierNone {
				t.Errorf("classifyK8sMatrix() = %q, want %q", got, matrixTierNone)
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
