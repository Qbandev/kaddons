package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/qbandev/kaddons/internal/addon"
	"github.com/qbandev/kaddons/internal/resilience"
)

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)
var blockTagRe = regexp.MustCompile(`(?i)</?(?:p|div|li|tr|td|th|h[1-6]|br|table|section|article|ul|ol)[^>]*>`)
var horizontalWhitespaceRe = regexp.MustCompile(`[ \t\r\f\v]+`)
var blankLineRe = regexp.MustCompile(`\n{2,}`)

// GitHubRawURL converts a GitHub URL to its raw.githubusercontent.com equivalent
// so that Markdown content is fetched directly instead of rendered HTML.
// Non-GitHub URLs, wiki URLs, release URLs, and org-only URLs are returned unchanged.
func GitHubRawURL(inputURL string) string {
	parsed, err := url.Parse(inputURL)
	if err != nil || parsed.Host != "github.com" {
		return inputURL
	}

	var segments []string
	for _, s := range strings.Split(parsed.Path, "/") {
		if s != "" {
			segments = append(segments, s)
		}
	}

	if len(segments) < 2 {
		return inputURL
	}

	owner := segments[0]
	repo := segments[1]

	if len(segments) >= 3 {
		switch segments[2] {
		case "wiki", "releases":
			return inputURL
		}
	}

	const rawHost = "https://raw.githubusercontent.com"

	switch {
	case len(segments) >= 4 && segments[2] == "blob":
		ref := segments[3]
		filePath := strings.Join(segments[4:], "/")
		return fmt.Sprintf("%s/%s/%s/%s/%s", rawHost, owner, repo, ref, filePath)

	case len(segments) >= 4 && segments[2] == "tree":
		ref := segments[3]
		if len(segments) > 4 {
			subPath := strings.Join(segments[4:], "/")
			return fmt.Sprintf("%s/%s/%s/%s/%s/README.md", rawHost, owner, repo, ref, subPath)
		}
		return fmt.Sprintf("%s/%s/%s/%s/README.md", rawHost, owner, repo, ref)

	case len(segments) == 2:
		return fmt.Sprintf("%s/%s/%s/HEAD/README.md", rawHost, owner, repo)

	default:
		return inputURL
	}
}

// CompatibilityPage fetches a URL and returns its content using a default HTTP client.
// GitHub URLs are automatically converted to raw.githubusercontent.com to fetch
// Markdown directly. Non-GitHub URLs go through HTML-strip path.
func CompatibilityPage(ctx context.Context, pageURL string) (string, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	return CompatibilityPageWithClient(ctx, client, pageURL)
}

// CompatibilityPageWithClient is like CompatibilityPage but uses the provided HTTP client,
// allowing callers to share a client with custom redirect policies or transport settings.
func CompatibilityPageWithClient(ctx context.Context, client *http.Client, pageURL string) (string, error) {
	if err := ValidatePublicHTTPSURL(pageURL); err != nil {
		return "", err
	}

	rawURL := GitHubRawURL(pageURL)
	isRaw := rawURL != pageURL
	if err := ValidatePublicHTTPSURL(rawURL); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.8")

	resp, err := doRequestWithRetry(ctx, client, req)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 2 MB cap
	if err != nil {
		return "", fmt.Errorf("reading response body: %w", err)
	}

	text := normalizeFetchedContent(string(body), isRaw)
	return text, nil
}

// EOLData fetches release lifecycle data from the endoflife.date API.
func EOLData(ctx context.Context, product string) ([]addon.EOLCycle, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	u := fmt.Sprintf("https://endoflife.date/api/%s.json", product)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := doRequestWithRetry(ctx, client, req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	var cycles []addon.EOLCycle
	if err := json.Unmarshal(body, &cycles); err != nil {
		return nil, fmt.Errorf("parsing EOL data: %w", err)
	}

	return cycles, nil
}

type eolProductsResponse struct {
	Result []addon.EOLProductCatalogEntry `json:"result"`
}

// EOLProducts fetches the endoflife.date v1 product catalog for runtime slug resolution.
func EOLProducts(ctx context.Context) ([]addon.EOLProductCatalogEntry, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	const catalogURL = "https://endoflife.date/api/v1/products"

	req, err := http.NewRequestWithContext(ctx, "GET", catalogURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := doRequestWithRetry(ctx, client, req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	products, err := parseEOLProducts(body)
	if err != nil {
		return nil, err
	}

	return products, nil
}

func parseEOLProducts(body []byte) ([]addon.EOLProductCatalogEntry, error) {
	var parsed eolProductsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parsing EOL product catalog: %w", err)
	}
	return parsed.Result, nil
}

func normalizeFetchedContent(rawText string, isRaw bool) string {
	text := rawText
	if !isRaw {
		// Preserve block boundaries as newlines so version matrices/tables remain extractable.
		text = blockTagRe.ReplaceAllString(text, "\n")
		text = htmlTagRe.ReplaceAllString(text, " ")
		text = horizontalWhitespaceRe.ReplaceAllString(text, " ")
		text = blankLineRe.ReplaceAllString(text, "\n")
		text = strings.TrimSpace(text)
	}

	// Keep a larger deterministic fetch budget because downstream pruning enforces
	// strict per-addon bounds before sending data to Gemini.
	if len(text) > 120000 {
		text = text[:120000]
	}
	return text
}

func doRequestWithRetry(ctx context.Context, client *http.Client, request *http.Request) (*http.Response, error) {
	policy := resilience.RetryPolicy{
		Attempts:     3,
		InitialDelay: 500 * time.Millisecond,
		MaxDelay:     time.Second,
		Multiplier:   2,
	}
	attemptCounter := 0
	return resilience.RetryWithResult(ctx, policy, isRetryableHTTPError, func(callCtx context.Context) (*http.Response, error) {
		attemptCounter++
		reqForAttempt := request.Clone(callCtx)
		resp, err := client.Do(reqForAttempt) // #nosec G704 -- request URLs are validated/fixed by callers
		if err != nil {
			return nil, err
		}
		if isRetryableHTTPStatus(resp.StatusCode) {
			if attemptCounter >= policy.Attempts {
				return resp, nil
			}
			_ = resp.Body.Close()
			return nil, fmt.Errorf("retryable HTTP status %d", resp.StatusCode)
		}
		return resp, nil
	})
}

func isRetryableHTTPStatus(statusCode int) bool {
	return resilience.IsRetryableHTTPStatus(statusCode)
}

func isRetryableHTTPError(err error) bool {
	return resilience.IsRetryableNetworkError(err) || strings.Contains(strings.ToLower(err.Error()), "retryable http status")
}
