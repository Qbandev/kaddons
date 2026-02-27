package validate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/qbandev/kaddons/internal/addon"
	"github.com/qbandev/kaddons/internal/fetch"
	"github.com/qbandev/kaddons/internal/resilience"
)

// ErrValidationFailed is returned when one or more validation checks fail.
var ErrValidationFailed = errors.New("validation failed")

// Matrix classification tiers.
const (
	matrixTierStrict  = "matrix"
	matrixTierPartial = "partial-matrix"
	matrixTierNone    = "no-matrix"
)

var (
	// Strict patterns: "kubernetes/k8s" adjacent to a version number + formal matrix keyword.
	k8sVersionStrict = regexp.MustCompile(`(?i)(?:(?:kubernetes|k8s)\s*(?:version)?\s*v?\d+\.\d+|v?\d+\.\d+\s*(?:kubernetes|k8s))`)
	matrixKeyStrict  = regexp.MustCompile(`(?i)(?:compatibility\s+matrix|supported\s+(?:kubernetes\s+)?versions?|version\s+support|k8s\s+compatibility)`)

	// Loose patterns: "kubernetes/k8s" within 200 chars of a version number + broader keywords.
	k8sVersionLoose = regexp.MustCompile(`(?i)(?:(?:kubernetes|k8s)[\s\S]{0,200}v?\d+\.\d+|v?\d+\.\d+[\s\S]{0,200}(?:kubernetes|k8s))`)
	matrixKeyLoose  = regexp.MustCompile(`(?i)(?:compatibility\s+matrix|supported\s+(?:kubernetes\s+)?versions?|version\s+support|k8s\s+compatibility|requirements?|prerequisites?|minimum\s+(?:kubernetes\s+)?version|tested\s+(?:on|with|against)|works\s+with|compatible\s+with|requires?\s+(?:kubernetes|k8s)|platform\s+(?:support|notes?|requirements?))`)

	// Validation pattern for K8s version strings (e.g. "1.28", "1.30").
	k8sVersionFormat = regexp.MustCompile(`^\d+\.\d+$`)
)

type consumer struct {
	addonName string
	field     string // "project_url", "repository", "compatibility_matrix_url", "changelog_location"
}

type urlTask struct {
	url          string
	needsContent bool // true if any consumer uses this as compatibility_matrix_url
	consumers    []consumer
}

type urlResult struct {
	reachable  bool
	reachError string // "HTTP 404", "error: timeout", etc.
	matrixTier string // matrixTierStrict, matrixTierPartial, or matrixTierNone; only meaningful if needsContent was true
}

// harvest extracts all URLs from addons and builds a map of unique URL tasks.
func harvest(addons []addon.Addon) map[string]*urlTask {
	tasks := make(map[string]*urlTask)

	for _, a := range addons {
		for _, pair := range []struct {
			field string
			url   string
		}{
			{"project_url", a.ProjectURL},
			{"repository", a.Repository},
			{"compatibility_matrix_url", a.CompatibilityMatrixURL},
			{"changelog_location", a.ChangelogLocation},
		} {
			if pair.url == "" {
				continue
			}

			t, exists := tasks[pair.url]
			if !exists {
				t = &urlTask{url: pair.url}
				tasks[pair.url] = t
			}

			t.consumers = append(t.consumers, consumer{
				addonName: a.Name,
				field:     pair.field,
			})

			if pair.field == "compatibility_matrix_url" {
				t.needsContent = true
			}
		}
	}

	return tasks
}

// applyFlags mutates the task map based on CLI flags.
// --links: set needsContent=false on all tasks (HEAD-only, skip body fetch)
// --matrix: remove tasks where needsContent is false (only process matrix URLs)
func applyFlags(tasks map[string]*urlTask, linksOnly, matrixOnly bool) {
	if linksOnly {
		for _, t := range tasks {
			t.needsContent = false
		}
		return
	}

	if matrixOnly {
		for url, t := range tasks {
			if !t.needsContent {
				delete(tasks, url)
			}
		}
	}
}

// storedDataProblem records a validation issue with stored compatibility data.
type storedDataProblem struct {
	addonName string
	field     string
	value     string
	reason    string
}

// validateStoredData checks stored compatibility data for format correctness.
func validateStoredData(addons []addon.Addon) []storedDataProblem {
	var problems []storedDataProblem

	for _, a := range addons {
		if a.KubernetesMinVersion != "" && !k8sVersionFormat.MatchString(a.KubernetesMinVersion) {
			problems = append(problems, storedDataProblem{
				addonName: a.Name,
				field:     "kubernetes_min_version",
				value:     a.KubernetesMinVersion,
				reason:    "must match format X.Y (e.g. 1.28)",
			})
		}

		if a.KubernetesMaxVersion != "" && !k8sVersionFormat.MatchString(a.KubernetesMaxVersion) {
			problems = append(problems, storedDataProblem{
				addonName: a.Name,
				field:     "kubernetes_max_version",
				value:     a.KubernetesMaxVersion,
				reason:    "must match format X.Y (e.g. 1.28)",
			})
		}

		if a.KubernetesMinVersion != "" && a.KubernetesMaxVersion != "" &&
			k8sVersionFormat.MatchString(a.KubernetesMinVersion) && k8sVersionFormat.MatchString(a.KubernetesMaxVersion) {
			if compareK8sMinorVersions(a.KubernetesMinVersion, a.KubernetesMaxVersion) > 0 {
				problems = append(problems, storedDataProblem{
					addonName: a.Name,
					field:     "kubernetes_min_version / kubernetes_max_version",
					value:     a.KubernetesMinVersion + " / " + a.KubernetesMaxVersion,
					reason:    "min version must not exceed max version",
				})
			}
		}

		supportedCompatibilityKeyCount := 0
		nonEmptyCompatibilityKeyCount := 0
		for key, versions := range a.KubernetesCompatibility {
			if key == "" {
				problems = append(problems, storedDataProblem{
					addonName: a.Name,
					field:     "kubernetes_compatibility",
					value:     "(empty key)",
					reason:    "addon version key must be non-empty",
				})
				continue
			}
			nonEmptyCompatibilityKeyCount++
			if isResolverSupportedCompatibilityKey(key) {
				supportedCompatibilityKeyCount++
			}
			if len(versions) == 0 {
				problems = append(problems, storedDataProblem{
					addonName: a.Name,
					field:     "kubernetes_compatibility",
					value:     key,
					reason:    "K8s version list must be non-empty",
				})
				continue
			}
			for _, v := range versions {
				if !k8sVersionFormat.MatchString(v) {
					problems = append(problems, storedDataProblem{
						addonName: a.Name,
						field:     "kubernetes_compatibility[" + key + "]",
						value:     v,
						reason:    "K8s version must match format X.Y (e.g. 1.28)",
					})
				}
			}
		}
		if nonEmptyCompatibilityKeyCount > 0 && supportedCompatibilityKeyCount == 0 {
			problems = append(problems, storedDataProblem{
				addonName: a.Name,
				field:     "kubernetes_compatibility",
				value:     "(all keys unsupported)",
				reason:    "matrix must contain at least one key format supported by stored resolver",
			})
		}
	}

	return problems
}

func isResolverSupportedCompatibilityKey(rawKey string) bool {
	normalizedKey := strings.ToLower(strings.TrimSpace(rawKey))
	if normalizedKey == "" {
		return false
	}

	if leftRangeKey, rightRangeKey, isRangeKey := parseCompatibilityRangeKey(normalizedKey); isRangeKey {
		return isNumericVersionToken(leftRangeKey) && isNumericVersionToken(rightRangeKey)
	}

	normalizedKey = strings.TrimPrefix(normalizedKey, ">=")
	normalizedKey = strings.TrimPrefix(normalizedKey, "v")
	normalizedKey = strings.TrimSuffix(normalizedKey, "+")
	if normalizedKey == "" {
		return false
	}

	if strings.HasSuffix(normalizedKey, ".x") {
		return isNumericVersionToken(strings.TrimSuffix(normalizedKey, ".x"))
	}

	// Accept semver-like pre-release keys (for example: 1.5.0-rc1).
	if hyphenIndex := strings.Index(normalizedKey, "-"); hyphenIndex > 0 {
		normalizedKey = normalizedKey[:hyphenIndex]
	}

	return isNumericVersionToken(normalizedKey)
}

func parseCompatibilityRangeKey(rawKey string) (leftKey string, rightKey string, isRange bool) {
	hyphenIndex := strings.Index(rawKey, "-")
	if hyphenIndex <= 0 || hyphenIndex >= len(rawKey)-1 {
		return "", "", false
	}

	leftKey = strings.TrimPrefix(strings.TrimSpace(rawKey[:hyphenIndex]), "v")
	rightKey = strings.TrimPrefix(strings.TrimSpace(rawKey[hyphenIndex+1:]), "v")
	if leftKey == "" || rightKey == "" {
		return "", "", false
	}

	return leftKey, rightKey, true
}

func isNumericVersionToken(rawToken string) bool {
	if rawToken == "" {
		return false
	}
	if rawToken[0] < '0' || rawToken[0] > '9' {
		return false
	}
	if !strings.Contains(rawToken, ".") {
		return false
	}

	segments := strings.Split(rawToken, ".")
	for _, segment := range segments {
		if segment == "" {
			return false
		}
		for _, character := range segment {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}

// compareK8sMinorVersions compares two "X.Y" version strings numerically.
// Returns negative if a < b, 0 if equal, positive if a > b.
func compareK8sMinorVersions(a, b string) int {
	aParts := strings.SplitN(a, ".", 2)
	bParts := strings.SplitN(b, ".", 2)
	aMajor, _ := strconv.Atoi(aParts[0])
	bMajor, _ := strconv.Atoi(bParts[0])
	if aMajor != bMajor {
		return aMajor - bMajor
	}
	aMinor, bMinor := 0, 0
	if len(aParts) > 1 {
		aMinor, _ = strconv.Atoi(aParts[1])
	}
	if len(bParts) > 1 {
		bMinor, _ = strconv.Atoi(bParts[1])
	}
	return aMinor - bMinor
}

func validateStoredDataOnly(addons []addon.Addon) error {
	storedProblems := validateStoredData(addons)
	if len(storedProblems) > 0 {
		fmt.Printf("Found **%d** stored compatibility data problems.\n\n", len(storedProblems))
		fmt.Println("| Addon Name | Field | Value | Reason |")
		fmt.Println("|------------|-------|-------|--------|")
		for _, p := range storedProblems {
			fmt.Printf("| %s | `%s` | %s | %s |\n", p.addonName, p.field, p.value, p.reason)
		}
		fmt.Println()
		return ErrValidationFailed
	}
	fmt.Println("Stored compatibility data validation passed.")
	return nil
}

// Run executes the validate command.
func Run(linksOnly, matrixOnly, storedOnly bool) error {
	addons, err := addon.LoadAddons()
	if err != nil {
		return fmt.Errorf("failed to load addon database: %w", err)
	}

	if storedOnly {
		return validateStoredDataOnly(addons)
	}
	// Validate stored compatibility data format alongside URL checks.
	storedProblems := validateStoredData(addons)
	hasStoredProblems := len(storedProblems) > 0
	if hasStoredProblems {
		fmt.Printf("Found **%d** stored compatibility data problems.\n\n", len(storedProblems))
		fmt.Println("| Addon Name | Field | Value | Reason |")
		fmt.Println("|------------|-------|-------|--------|")
		for _, p := range storedProblems {
			fmt.Printf("| %s | `%s` | %s | %s |\n", p.addonName, p.field, p.value, p.reason)
		}
		fmt.Println()
	}

	tasks := harvest(addons)
	applyFlags(tasks, linksOnly, matrixOnly)

	if len(tasks) == 0 {
		if hasStoredProblems {
			return ErrValidationFailed
		}
		fmt.Println("No URLs to validate.")
		return nil
	}

	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	results := make(map[string]*urlResult, len(tasks))
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, 10)
	)

	fmt.Fprintf(os.Stderr, "Validating %d unique URLs across %d addons...\n", len(tasks), len(addons))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	for _, t := range tasks {
		wg.Add(1)
		go func(task *urlTask) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			result := executeTask(ctx, client, task)

			mu.Lock()
			results[task.url] = result
			mu.Unlock()
		}(t)
	}
	wg.Wait()

	urlErr := report(tasks, results, linksOnly)
	if urlErr != nil {
		return urlErr
	}
	if hasStoredProblems {
		return ErrValidationFailed
	}
	return nil
}

// executeTask processes a single URL task.
func executeTask(ctx context.Context, client *http.Client, task *urlTask) *urlResult {
	if task.needsContent {
		content, err := fetch.CompatibilityPageWithClient(ctx, client, task.url)
		if err != nil {
			return &urlResult{reachable: false, reachError: fmt.Sprintf("error: %v", err)}
		}
		return &urlResult{
			reachable:  true,
			matrixTier: ClassifyK8sMatrix(content),
		}
	}

	status := checkURL(ctx, client, task.url)
	if status != "ok" {
		return &urlResult{reachable: false, reachError: status}
	}
	return &urlResult{reachable: true}
}

// checkURL performs an HTTP HEAD request with GET fallback on 405/403.
func checkURL(ctx context.Context, client *http.Client, rawURL string) string {
	if err := fetch.ValidatePublicHTTPSURL(rawURL); err != nil {
		return err.Error()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return fmt.Sprintf("invalid URL: %v", err)
	}
	req.Header.Set("User-Agent", "kaddons-validate/1.0")

	resp, err := doRequestWithRetry(ctx, client, req)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}

	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusForbidden {
		if err := resp.Body.Close(); err != nil {
			return fmt.Sprintf("error: closing response body: %v", err)
		}
		getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return fmt.Sprintf("invalid URL: %v", err)
		}
		getReq.Header.Set("User-Agent", "kaddons-validate/1.0")

		resp2, err := doRequestWithRetry(ctx, client, getReq)
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		if err := resp2.Body.Close(); err != nil {
			return fmt.Sprintf("error: closing response body: %v", err)
		}

		if resp2.StatusCode >= 400 {
			return fmt.Sprintf("HTTP %d", resp2.StatusCode)
		}
		return "ok"
	}
	if err := resp.Body.Close(); err != nil {
		return fmt.Sprintf("error: closing response body: %v", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return "ok"
}

func doRequestWithRetry(ctx context.Context, client *http.Client, request *http.Request) (*http.Response, error) {
	policy := resilience.RetryPolicy{
		Attempts:     3,
		InitialDelay: 500 * time.Millisecond,
		MaxDelay:     time.Second,
		Multiplier:   2,
	}
	return resilience.DoHTTPRequestWithRetry(ctx, client, request, policy)
}

// ClassifyK8sMatrix returns the tier of K8s compatibility data found in page content.
func ClassifyK8sMatrix(pageText string) string {
	if k8sVersionStrict.MatchString(pageText) && matrixKeyStrict.MatchString(pageText) {
		return matrixTierStrict
	}
	if k8sVersionLoose.MatchString(pageText) && matrixKeyLoose.MatchString(pageText) {
		return matrixTierPartial
	}
	return matrixTierNone
}

// hasK8sMatrix checks whether page content contains K8s version compatibility data.
func hasK8sMatrix(pageText string) bool {
	return ClassifyK8sMatrix(pageText) != matrixTierNone
}

type brokenLink struct {
	addonName string
	field     string
	url       string
	errMsg    string
}

type matrixProblem struct {
	addonName string
	url       string
	status    string // "no-matrix"
}

// report outputs results as two separate tables and returns ErrValidationFailed if failures exist.
func report(tasks map[string]*urlTask, results map[string]*urlResult, linksOnly bool) error {
	var broken []brokenLink
	var matrixProblems []matrixProblem

	for _, t := range tasks {
		r := results[t.url]
		if r == nil {
			for _, c := range t.consumers {
				broken = append(broken, brokenLink{
					addonName: c.addonName,
					field:     c.field,
					url:       t.url,
					errMsg:    "internal error: missing validation result",
				})
			}
			continue
		}

		if !r.reachable {
			// Unreachable URL: report all consumers
			for _, c := range t.consumers {
				broken = append(broken, brokenLink{
					addonName: c.addonName,
					field:     c.field,
					url:       t.url,
					errMsg:    r.reachError,
				})
			}
			continue
		}

		// Reachable but no K8s matrix content: only report compatibility_matrix_url consumers
		if t.needsContent && r.matrixTier == matrixTierNone {
			for _, c := range t.consumers {
				if c.field == "compatibility_matrix_url" {
					matrixProblems = append(matrixProblems, matrixProblem{
						addonName: c.addonName,
						url:       t.url,
						status:    matrixTierNone,
					})
				}
			}
		}
	}

	hasFailures := len(broken) > 0 || len(matrixProblems) > 0

	if !hasFailures {
		if linksOnly {
			fmt.Println("All links are healthy.")
		} else {
			fmt.Println("All validations passed.")
		}
		return nil
	}

	if len(broken) > 0 {
		addonSet := make(map[string]struct{})
		for _, b := range broken {
			addonSet[b.addonName] = struct{}{}
		}
		fmt.Printf("Found **%d** broken links across **%d** addons.\n\n", len(broken), len(addonSet))
		fmt.Println("| Addon Name | Field | URL | Error |")
		fmt.Println("|------------|-------|-----|-------|")
		for _, b := range broken {
			fmt.Printf("| %s | `%s` | %s | %s |\n", b.addonName, b.field, b.url, b.errMsg)
		}
	}

	if len(matrixProblems) > 0 {
		if len(broken) > 0 {
			fmt.Println()
		}
		fmt.Printf("Found **%d** addons with missing K8s matrix data.\n\n", len(matrixProblems))
		fmt.Println("| Addon Name | URL | Status |")
		fmt.Println("|------------|-----|--------|")
		for _, p := range matrixProblems {
			fmt.Printf("| %s | %s | %s |\n", p.addonName, p.url, p.status)
		}
	}

	return ErrValidationFailed
}
