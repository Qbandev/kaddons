package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/qbandev/kaddons/internal/addon"
	"github.com/qbandev/kaddons/internal/extract"
	"github.com/qbandev/kaddons/internal/fetch"
	"github.com/qbandev/kaddons/internal/validate"
)

const defaultWorkerCount = 10

type syncUpdateRecord struct {
	name       string
	entryCount int
}

type syncSkipRecord struct {
	name   string
	reason string
}

type fetchResult struct {
	cacheFilePath  string
	classifiedTier string
	fetchError     string
}

type manifestEntry struct {
	URL   string `json:"url,omitempty"`
	File  string `json:"file,omitempty"`
	Tier  string `json:"tier"`
	Error string `json:"error,omitempty"`
}

func main() {
	cacheRootPath := flag.String("cache-root", ".cache/matrix-extract", "Root directory for extracted matrix cache files")
	workerCount := flag.Int("workers", defaultWorkerCount, "Number of parallel fetch workers")
	filterFlag := flag.String("filter", "", "Comma-separated addon names to process (case-insensitive substring match)")
	syncMode := flag.Bool("sync", false, "Extract matrices and write back to addon database JSON")
	dbPath := flag.String("db-path", "internal/addon/k8s_universal_addons.json", "Path to addon database JSON file")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: kaddons-extract [--cache-root PATH] [--workers N] [--filter NAMES]\n")
		fmt.Fprintf(os.Stderr, "       kaddons-extract --sync [--db-path PATH] [--workers N] [--filter NAMES]\n\n")
		fmt.Fprintf(os.Stderr, "Fetches compatibility pages for addon matrix URLs,\n")
		fmt.Fprintf(os.Stderr, "classifies matrix quality, and writes a manifest for extraction subagents.\n\n")
		fmt.Fprintf(os.Stderr, "With --sync, extracts matrices and writes them back to the addon database.\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  kaddons-extract --filter cert-manager\n")
		fmt.Fprintf(os.Stderr, "  kaddons-extract --sync\n")
		fmt.Fprintf(os.Stderr, "  kaddons-extract --sync --db-path path/to/addons.json\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *workerCount <= 0 {
		fmt.Fprintln(os.Stderr, "Error: --workers must be greater than zero")
		os.Exit(2)
	}

	if *syncMode {
		exitCode := runSync(*dbPath, *workerCount)
		os.Exit(exitCode)
	}

	var filters []string
	if *filterFlag != "" {
		for _, f := range strings.Split(*filterFlag, ",") {
			f = strings.TrimSpace(f)
			if f != "" {
				filters = append(filters, strings.ToLower(f))
			}
		}
	}

	if err := run(*cacheRootPath, *workerCount, filters); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func run(cacheRootPath string, workerCount int, filters []string) error {
	allAddons, err := addon.LoadAddons()
	if err != nil {
		return fmt.Errorf("failed to load addon database: %w", err)
	}

	addons := filterAddons(allAddons, filters)
	if len(addons) == 0 {
		return fmt.Errorf("no addons matched filter %s (total in DB: %d)", strings.Join(filters, ", "), len(allAddons))
	}

	pagesCacheDirectory := filepath.Join(cacheRootPath, "pages")
	if err := os.MkdirAll(pagesCacheDirectory, 0o750); err != nil {
		return fmt.Errorf("creating pages cache directory: %w", err)
	}

	urlToAddonNames := collectCompatibilityURLs(addons)
	uniqueCompatibilityURLs := sortedURLKeys(urlToAddonNames)

	if len(filters) > 0 {
		fmt.Fprintf(os.Stderr, "Filter matched %d addons (from %d total)\n", len(addons), len(allAddons))
	}
	fmt.Fprintf(
		os.Stderr,
		"Processing %d addons with %d unique compatibility URLs\n",
		len(addons),
		len(uniqueCompatibilityURLs),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	resultsByURL := fetchAndClassifyAll(
		ctx,
		client,
		uniqueCompatibilityURLs,
		pagesCacheDirectory,
		workerCount,
	)
	manifestByAddonName := buildManifest(addons, resultsByURL)

	manifestPath := filepath.Join(cacheRootPath, "manifest.json")
	if err := writeManifest(manifestPath, manifestByAddonName); err != nil {
		return err
	}

	fetchedSuccessCount, fetchedFailureCount := summarizeFetchResults(resultsByURL)
	fmt.Fprintf(os.Stderr, "Fetched pages: %d succeeded, %d failed\n", fetchedSuccessCount, fetchedFailureCount)
	fmt.Fprintf(os.Stderr, "Manifest written: %s\n", manifestPath)

	return nil
}

func filterAddons(addons []addon.Addon, filters []string) []addon.Addon {
	if len(filters) == 0 {
		return addons
	}
	var matched []addon.Addon
	for _, a := range addons {
		nameLower := strings.ToLower(a.Name)
		for _, f := range filters {
			if strings.Contains(nameLower, f) {
				matched = append(matched, a)
				break
			}
		}
	}
	return matched
}

func collectCompatibilityURLs(addons []addon.Addon) map[string][]string {
	urlToAddonNames := make(map[string][]string)
	for _, addonEntry := range addons {
		if addonEntry.CompatibilityMatrixURL == "" {
			continue
		}
		urlToAddonNames[addonEntry.CompatibilityMatrixURL] = append(
			urlToAddonNames[addonEntry.CompatibilityMatrixURL],
			addonEntry.Name,
		)
	}
	return urlToAddonNames
}

func sortedURLKeys(urlToAddonNames map[string][]string) []string {
	keys := make([]string, 0, len(urlToAddonNames))
	for url := range urlToAddonNames {
		keys = append(keys, url)
	}
	sort.Strings(keys)
	return keys
}

func fetchAndClassifyAll(
	ctx context.Context,
	client *http.Client,
	uniqueURLs []string,
	pagesCacheDirectory string,
	workerCount int,
) map[string]fetchResult {
	resultsByURL := make(map[string]fetchResult, len(uniqueURLs))
	var (
		resultMutex sync.Mutex
		workerGroup sync.WaitGroup
	)

	workQueue := make(chan string, len(uniqueURLs))
	for _, pageURL := range uniqueURLs {
		workQueue <- pageURL
	}
	close(workQueue)

	for i := 0; i < workerCount; i++ {
		workerGroup.Add(1)
		go func() {
			defer workerGroup.Done()
			for pageURL := range workQueue {
				result := fetchAndClassifyOne(ctx, client, pageURL, pagesCacheDirectory)
				resultMutex.Lock()
				resultsByURL[pageURL] = result
				resultMutex.Unlock()
			}
		}()
	}

	workerGroup.Wait()
	return resultsByURL
}

func fetchAndClassifyOne(
	ctx context.Context,
	client *http.Client,
	pageURL string,
	pagesCacheDirectory string,
) fetchResult {
	content, err := fetch.CompatibilityPageWithClient(ctx, client, pageURL)
	if err != nil {
		return fetchResult{
			classifiedTier: "fetch-error",
			fetchError:     err.Error(),
		}
	}

	cacheFileName := sha256Hex(pageURL) + ".txt"
	cacheFilePath := filepath.Join(pagesCacheDirectory, cacheFileName)
	if writeErr := os.WriteFile(cacheFilePath, []byte(content), 0o600); writeErr != nil {
		return fetchResult{
			classifiedTier: "cache-write-error",
			fetchError:     writeErr.Error(),
		}
	}

	return fetchResult{
		cacheFilePath:  cacheFilePath,
		classifiedTier: validate.ClassifyK8sMatrix(content),
	}
}

func buildManifest(addons []addon.Addon, resultsByURL map[string]fetchResult) map[string]manifestEntry {
	manifestByAddonName := make(map[string]manifestEntry, len(addons))
	for _, addonEntry := range addons {
		pageURL := addonEntry.CompatibilityMatrixURL
		if pageURL == "" {
			manifestByAddonName[addonEntry.Name] = manifestEntry{
				Tier:  "no-url",
				Error: "compatibility_matrix_url is empty",
			}
			continue
		}

		result := resultsByURL[pageURL]
		manifestByAddonName[addonEntry.Name] = manifestEntry{
			URL:   pageURL,
			File:  result.cacheFilePath,
			Tier:  result.classifiedTier,
			Error: result.fetchError,
		}
	}
	return manifestByAddonName
}

func summarizeFetchResults(resultsByURL map[string]fetchResult) (successCount int, failureCount int) {
	for _, result := range resultsByURL {
		if result.fetchError != "" {
			failureCount++
			continue
		}
		successCount++
	}
	return successCount, failureCount
}

func writeManifest(manifestPath string, manifestByAddonName map[string]manifestEntry) error {
	manifestBytes, err := json.MarshalIndent(manifestByAddonName, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}
	if err := os.WriteFile(manifestPath, manifestBytes, 0o600); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}
	return nil
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func runSync(dbPath string, workerCount int) int {
	dbPath = filepath.Clean(dbPath)
	if _, err := os.Stat(dbPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: database file not found: %s\n", dbPath)
		return 2
	}

	addons, err := addon.LoadAddonsFromDisk(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		return 2
	}

	// Filter to candidates: no stored compatibility data and has a compatibility URL
	type candidate struct {
		index int
		url   string
	}
	var candidates []candidate
	urlSet := make(map[string]bool)
	for i := range addons {
		if addons[i].HasStoredCompatibility() || addons[i].CompatibilityMatrixURL == "" {
			continue
		}
		candidates = append(candidates, candidate{index: i, url: addons[i].CompatibilityMatrixURL})
		urlSet[addons[i].CompatibilityMatrixURL] = true
	}

	if len(candidates) == 0 {
		fmt.Fprintln(os.Stderr, "No addons without stored data to process.")
		return 0
	}

	// Deduplicate URLs for fetching
	uniqueURLs := make([]string, 0, len(urlSet))
	for u := range urlSet {
		uniqueURLs = append(uniqueURLs, u)
	}
	sort.Strings(uniqueURLs)

	fmt.Fprintf(os.Stderr, "Candidates: %d addons without stored data\n", len(candidates))
	fmt.Fprintf(os.Stderr, "Fetching %d unique URLs with %d workers...\n", len(uniqueURLs), workerCount)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	// Fetch pages with worker pool
	type pageResult struct {
		url  string
		page fetch.FetchedPage
		err  error
	}
	pageResults := make(map[string]pageResult, len(uniqueURLs))
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	workQueue := make(chan string, len(uniqueURLs))
	for _, u := range uniqueURLs {
		workQueue <- u
	}
	close(workQueue)

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for pageURL := range workQueue {
				page, fetchErr := fetch.CompatibilityPageFullWithClient(ctx, client, pageURL)
				mu.Lock()
				pageResults[pageURL] = pageResult{url: pageURL, page: page, err: fetchErr}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// Count fetch results
	fetchSuccess, fetchFail := 0, 0
	var fetchFailures []string
	for _, r := range pageResults {
		if r.err != nil {
			fetchFail++
			fetchFailures = append(fetchFailures, fmt.Sprintf("  - %s: %v", r.url, r.err))
		} else {
			fetchSuccess++
		}
	}
	sort.Strings(fetchFailures)

	// Extract matrices and apply to addons
	var updated []syncUpdateRecord
	var skipped []syncSkipRecord

	for _, c := range candidates {
		r, ok := pageResults[c.url]
		if !ok || r.err != nil {
			continue
		}

		var matrix map[string][]string
		var extractErr error
		if r.page.IsRaw {
			matrix, extractErr = extract.ExtractMarkdownMatrix(r.page.Raw)
		} else {
			matrix, extractErr = extract.ExtractHTMLMatrix(r.page.Raw)
		}
		if extractErr != nil || len(matrix) == 0 {
			continue
		}

		// Tentatively apply and validate
		original := addons[c.index].KubernetesCompatibility
		addons[c.index].KubernetesCompatibility = matrix

		problems := validate.ValidateStoredData([]addon.Addon{addons[c.index]})
		if len(problems) > 0 {
			addons[c.index].KubernetesCompatibility = original
			skipped = append(skipped, syncSkipRecord{
				name:   addons[c.index].Name,
				reason: fmt.Sprintf("%s: %s", problems[0].Field, problems[0].Reason),
			})
			continue
		}

		updated = append(updated, syncUpdateRecord{
			name:       addons[c.index].Name,
			entryCount: len(matrix),
		})
	}

	if len(updated) == 0 {
		fmt.Fprintln(os.Stderr, "No new data extracted.")
		printSyncReport(os.Stderr, dbPath, len(candidates), fetchSuccess, fetchFail, updated, skipped, fetchFailures)
		return 0
	}

	// Write back
	if err := addon.SaveAddonsToDisk(dbPath, addons); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing database: %s\n", err)
		return 2
	}

	printSyncReport(os.Stderr, dbPath, len(candidates), fetchSuccess, fetchFail, updated, skipped, fetchFailures)
	return 1
}

func printSyncReport(w io.Writer, dbPath string, candidateCount, fetchSuccess, fetchFail int, updated []syncUpdateRecord, skipped []syncSkipRecord, fetchFailures []string) {
	_, _ = fmt.Fprintln(w, "DB Sync Summary")
	_, _ = fmt.Fprintf(w, "  Candidates:  %d addons without stored data\n", candidateCount)
	_, _ = fmt.Fprintf(w, "  Fetched:     %d pages (%d failed)\n", fetchSuccess, fetchFail)
	_, _ = fmt.Fprintf(w, "  Extracted:   %d new compatibility matrices\n", len(updated)+len(skipped))
	_, _ = fmt.Fprintf(w, "  Validated:   %d passed, %d skipped (validation errors)\n", len(updated), len(skipped))
	if len(updated) > 0 {
		_, _ = fmt.Fprintf(w, "  Updated:     %d addons written to %s\n", len(updated), dbPath)
	}

	if len(updated) > 0 {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "Updated addons:")
		for _, u := range updated {
			_, _ = fmt.Fprintf(w, "  - %s: %d version entries\n", u.name, u.entryCount)
		}
	}

	if len(skipped) > 0 {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "Skipped (validation errors):")
		for _, s := range skipped {
			_, _ = fmt.Fprintf(w, "  - %s: %s\n", s.name, s.reason)
		}
	}

	if len(fetchFailures) > 0 {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "Fetch failures:")
		for _, f := range fetchFailures {
			_, _ = fmt.Fprintln(w, f)
		}
	}
}
