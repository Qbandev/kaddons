package validate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/qbandev/kaddons/internal/addon"
	"github.com/qbandev/kaddons/internal/fetch"
	"github.com/qbandev/kaddons/internal/resilience"
)

// ErrValidationFailed is returned when one or more validation checks fail.
var ErrValidationFailed = errors.New("validation failed")

var (
	k8sVersionPattern = regexp.MustCompile(`(?i)(?:kubernetes|k8s)\s*(?:version)?\s*\d+\.\d+`)
	matrixKeyword     = regexp.MustCompile(`(?i)(?:compatibility\s+matrix|supported\s+(?:kubernetes\s+)?versions?|version\s+support|k8s\s+compatibility)`)
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
	reachable    bool
	reachError   string // "HTTP 404", "error: timeout", etc.
	contentValid bool   // only meaningful if needsContent was true
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

// Run executes the validate command.
func Run(linksOnly, matrixOnly bool) error {
	addons, err := addon.LoadAddons()
	if err != nil {
		return fmt.Errorf("failed to load addon database: %w", err)
	}

	tasks := harvest(addons)
	applyFlags(tasks, linksOnly, matrixOnly)

	if len(tasks) == 0 {
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

	return report(tasks, results, linksOnly)
}

// executeTask processes a single URL task.
func executeTask(ctx context.Context, client *http.Client, task *urlTask) *urlResult {
	if task.needsContent {
		content, err := fetch.CompatibilityPageWithClient(ctx, client, task.url)
		if err != nil {
			return &urlResult{reachable: false, reachError: fmt.Sprintf("error: %v", err)}
		}
		return &urlResult{
			reachable:    true,
			contentValid: hasK8sMatrix(content),
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
	attemptCounter := 0
	return resilience.RetryWithResult(ctx, policy, isRetryableRequestError, func(callCtx context.Context) (*http.Response, error) {
		attemptCounter++
		reqForAttempt := request.Clone(callCtx)
		response, err := client.Do(reqForAttempt) // #nosec G704 -- rawURL is pre-validated by ValidatePublicHTTPSURL
		if err != nil {
			return nil, err
		}
		if isRetryableStatusCode(response.StatusCode) {
			if attemptCounter >= policy.Attempts {
				return response, nil
			}
			_ = response.Body.Close()
			return nil, fmt.Errorf("retryable HTTP status %d", response.StatusCode)
		}
		return response, nil
	})
}

func isRetryableStatusCode(statusCode int) bool {
	return resilience.IsRetryableHTTPStatus(statusCode)
}

func isRetryableRequestError(err error) bool {
	return resilience.IsRetryableNetworkError(err) || strings.Contains(strings.ToLower(err.Error()), "retryable http status")
}

// hasK8sMatrix checks whether page content contains K8s version compatibility data.
func hasK8sMatrix(pageText string) bool {
	return k8sVersionPattern.MatchString(pageText) && matrixKeyword.MatchString(pageText)
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

		// Reachable but content invalid: only report compatibility_matrix_url consumers
		if t.needsContent && !r.contentValid {
			for _, c := range t.consumers {
				if c.field == "compatibility_matrix_url" {
					matrixProblems = append(matrixProblems, matrixProblem{
						addonName: c.addonName,
						url:       t.url,
						status:    "no-matrix",
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
