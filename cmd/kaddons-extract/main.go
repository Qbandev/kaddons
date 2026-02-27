package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/qbandev/kaddons/internal/addon"
	"github.com/qbandev/kaddons/internal/fetch"
	"github.com/qbandev/kaddons/internal/validate"
)

const defaultWorkerCount = 10

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
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: kaddons-extract [--cache-root PATH] [--workers N] [--filter NAMES]\n\n")
		fmt.Fprintf(os.Stderr, "Fetches compatibility pages for addon matrix URLs,\n")
		fmt.Fprintf(os.Stderr, "classifies matrix quality, and writes a manifest for extraction subagents.\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  kaddons-extract --filter cert-manager\n")
		fmt.Fprintf(os.Stderr, "  kaddons-extract --filter \"cert-manager,karpenter,istio\"\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *workerCount <= 0 {
		fmt.Fprintln(os.Stderr, "Error: --workers must be greater than zero")
		os.Exit(2)
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
