package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/qbandev/kaddons/internal/addon"
	"github.com/qbandev/kaddons/internal/cluster"
	"github.com/qbandev/kaddons/internal/fetch"
	"github.com/qbandev/kaddons/internal/output"
	"google.golang.org/genai"
)

type addonWithInfo struct {
	cluster.DetectedAddon
	DBMatch              *addon.Addon     `json:"db_match,omitempty"`
	CompatibilityContent string           `json:"compatibility_content,omitempty"`
	CompatibilityURL     string           `json:"compatibility_url,omitempty"`
	FetchError           string           `json:"fetch_error,omitempty"`
	EOLData              []addon.EOLCycle `json:"eol_data,omitempty"`
}

// Run executes the Plan-and-Execute pipeline.
func Run(ctx context.Context, apiKey, model, namespace, k8sVersionOverride, addonsFilter, outputFormat, outputPath string) error {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return fmt.Errorf("creating Gemini client: %w", err)
	}

	addonDB, err := addon.LoadAddons()
	if err != nil {
		return fmt.Errorf("loading addon database: %w", err)
	}
	addonMatcher := addon.NewMatcher(addonDB)

	// Phase 1: Deterministic data collection (no LLM involved)
	k8sVersion := k8sVersionOverride
	if k8sVersion == "" {
		fmt.Fprintln(os.Stderr, "Detecting cluster version...")
		v, err := cluster.GetClusterVersion(ctx)
		if err != nil {
			return fmt.Errorf("getting cluster version: %w", err)
		}
		k8sVersion = v
	}
	fmt.Fprintf(os.Stderr, "Cluster version: %s\n", k8sVersion)

	detected, err := cluster.ListInstalledAddons(ctx, namespace)
	if err != nil {
		return fmt.Errorf("listing installed addons: %w", err)
	}

	// Apply addon filter if specified
	if addonsFilter != "" {
		filters := strings.Split(addonsFilter, ",")
		filterSet := make(map[string]bool, len(filters))
		for _, f := range filters {
			filterSet[strings.TrimSpace(strings.ToLower(f))] = true
		}
		var filtered []cluster.DetectedAddon
		for _, a := range detected {
			if filterSet[strings.ToLower(a.Name)] {
				filtered = append(filtered, a)
			}
		}
		detected = filtered
	}
	sort.SliceStable(detected, func(leftIndex int, rightIndex int) bool {
		left := detected[leftIndex]
		right := detected[rightIndex]
		leftKey := strings.ToLower(left.Name) + "|" + strings.ToLower(left.Namespace) + "|" + strings.ToLower(left.Version)
		rightKey := strings.ToLower(right.Name) + "|" + strings.ToLower(right.Namespace) + "|" + strings.ToLower(right.Version)
		return leftKey < rightKey
	})
	fmt.Fprintf(os.Stderr, "Discovered %d workloads\n", len(detected))

	// Phase 2: Match addons against DB, deduplicate by addon name (prefer entry with version)
	type enrichedEntry struct {
		info   addonWithInfo
		dbName string
	}
	bestByName := make(map[string]enrichedEntry)
	for _, a := range detected {
		matches := addonMatcher.Match(a.Name)
		if len(matches) == 0 {
			continue
		}

		dbName := strings.ToLower(matches[0].Name)
		existing, exists := bestByName[dbName]
		if exists && existing.info.Version != "" && a.Version == "" {
			continue // keep the one with a version
		}

		bestByName[dbName] = enrichedEntry{
			info: addonWithInfo{
				DetectedAddon: a,
				DBMatch:       &matches[0],
			},
			dbName: dbName,
		}
	}

	fmt.Fprintf(os.Stderr, "Matched %d known addons\n", len(bestByName))

	// Fetch compatibility pages and EOL data for matched addons
	fmt.Fprintf(os.Stderr, "Enriching %d addons...\n", len(bestByName))
	runtimeEOLSlugLookup := make(map[string]string)
	products, err := fetch.EOLProducts(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: EOL product catalog fetch failed, using static fallback aliases: %v\n", err)
	} else {
		runtimeEOLSlugLookup = addon.BuildRuntimeEOLSlugLookup(products)
	}
	enriched := make([]addonWithInfo, 0, len(bestByName))
	fetchedURLs := make(map[string]string) // cache: URL -> content
	orderedAddonNames := make([]string, 0, len(bestByName))
	for addonName := range bestByName {
		orderedAddonNames = append(orderedAddonNames, addonName)
	}
	sort.Strings(orderedAddonNames)
	for _, addonName := range orderedAddonNames {
		entry := bestByName[addonName]
		info := entry.info
		if info.DBMatch.CompatibilityMatrixURL != "" {
			info.CompatibilityURL = info.DBMatch.CompatibilityMatrixURL
			if cached, ok := fetchedURLs[info.CompatibilityURL]; ok {
				info.CompatibilityContent = cached
			} else {
				content, err := fetch.CompatibilityPage(ctx, info.DBMatch.CompatibilityMatrixURL)
				if err != nil {
					info.FetchError = err.Error()
				} else {
					info.CompatibilityContent = content
					fetchedURLs[info.CompatibilityURL] = content
				}
			}
		}
		if slug, ok := addon.LookupEOLSlugWithRuntime(info.Name, runtimeEOLSlugLookup); ok {
			cycles, err := fetch.EOLData(ctx, slug)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: EOL data fetch failed for %s: %v\n", info.Name, err)
			} else {
				info.EOLData = cycles
			}
		}
		enriched = append(enriched, info)
	}
	if len(enriched) == 0 {
		if _, err := output.FormatOutput("[]", k8sVersion, outputFormat, outputPath); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Done: %d compatible, %d incompatible, %d unknown\n", 0, 0, 0)
		return nil
	}

	// Phase 3: LLM analysis — single call with all data pre-collected
	fmt.Fprintf(os.Stderr, "Analyzing with %s...\n", model)
	return analyzeCompatibility(ctx, client, model, k8sVersion, enriched, outputFormat, outputPath)
}

const analysisSystemPrompt = `You are a Kubernetes addon compatibility analyzer. You will receive pre-collected data about addons installed in a cluster, including their compatibility matrix page content when available.

Your ONLY job is to analyze the provided data and determine compatibility. You must produce a strict JSON array (no markdown code fences, no extra text before or after) with this schema for EACH addon provided:

{
  "name": "addon-name",
  "namespace": "namespace",
  "installed_version": "v1.2.3",
  "compatible": "true"|"false"|"unknown",
  "latest_compatible_version": "v1.2.5",
  "note": "explanation with source citation, upgrade status, and support dates"
}

You MUST use exact strings "true", "false", or "unknown" — not boolean literals or null.

Rules:
- You MUST include ALL addons from the input — do not skip any
- "compatible" must be "unknown" if you cannot determine compatibility from the provided data
- Only include "latest_compatible_version" if you found evidence in the compatibility page or EOL data

Notes (source-cited, comprehensive):
- "note" must cite the source URL and include all relevant context in a single field
- Include the compatibility source URL citation: e.g. "The compatibility matrix at <URL> states v1.14 supports K8s 1.24-1.31."
- When determinable from eol_data, include the supported-until date: e.g. "Supported until 2025-09-10."
- When the installed version is confirmed incompatible, state upgrade-required status: e.g. "Upgrade required: <URL> states v0.14.x only supports K8s 1.32."
- If the compatibility page lacks structured version data, explain what the page contained: e.g. "The page at <URL> is an installation guide without a version compatibility matrix."
- If eol_data is provided, use it to determine version support status and latest version. Prefer the compatibility page for K8s-specific compatibility; use EOL data as supplementary context for version lifecycle
- Base your analysis ONLY on the page content and EOL data provided — do not hallucinate compatibility information`

func analyzeCompatibility(ctx context.Context, client *genai.Client, model string, k8sVersion string, addons []addonWithInfo, outputFormat string, outputPath string) error {
	dataPayload := struct {
		K8sVersion string          `json:"k8s_version"`
		Addons     []addonWithInfo `json:"addons"`
	}{
		K8sVersion: k8sVersion,
		Addons:     addons,
	}

	dataJSON, err := json.MarshalIndent(dataPayload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling addon data: %w", err)
	}

	deterministicTemperature := float32(0)
	deterministicTopP := float32(1)
	deterministicTopK := float32(1)
	deterministicSeed := int32(42)
	config := &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(analysisSystemPrompt, genai.RoleUser),
		Temperature:       &deterministicTemperature,
		TopP:              &deterministicTopP,
		TopK:              &deterministicTopK,
		Seed:              &deterministicSeed,
		CandidateCount:    1,
		ResponseMIMEType:  "application/json",
	}

	resp, err := generateContentWithRetry(ctx, client, model, []*genai.Content{
		genai.NewContentFromText(
			fmt.Sprintf("Analyze compatibility for these addons:\n\n%s", string(dataJSON)),
			genai.RoleUser,
		),
	}, config)
	if err != nil {
		return fmt.Errorf("LLM analysis failed: %w", err)
	}

	raw := collectTextResponse(resp)
	extracted := output.ExtractJSON(raw)

	results, err := output.FormatOutput(extracted, k8sVersion, outputFormat, outputPath)
	if err != nil {
		return err
	}

	var compatible, incompatible, unknown int
	for _, result := range results {
		switch result.Compatible {
		case output.StatusTrue:
			compatible++
		case output.StatusFalse:
			incompatible++
		default:
			unknown++
		}
	}
	fmt.Fprintf(os.Stderr, "Done: %d compatible, %d incompatible, %d unknown\n", compatible, incompatible, unknown)

	return nil
}

func collectTextResponse(resp *genai.GenerateContentResponse) string {
	if resp == nil {
		return ""
	}
	var sb strings.Builder
	for _, cand := range resp.Candidates {
		if cand.Content == nil {
			continue
		}
		for _, part := range cand.Content.Parts {
			if part.Text != "" {
				sb.WriteString(part.Text)
			}
		}
	}
	return sb.String()
}

func generateContentWithRetry(
	ctx context.Context,
	client *genai.Client,
	model string,
	contents []*genai.Content,
	config *genai.GenerateContentConfig,
) (*genai.GenerateContentResponse, error) {
	const maxAttempts = 3
	backoff := []time.Duration{0, time.Second, 2 * time.Second}
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if backoff[attempt-1] > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff[attempt-1]):
			}
		}

		resp, err := client.Models.GenerateContent(ctx, model, contents, config)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !isTransientLLMError(err) {
			return nil, err
		}
	}

	return nil, lastErr
}

func isTransientLLMError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unexpected eof") ||
		strings.Contains(message, "timeout") ||
		strings.Contains(message, "temporary") ||
		strings.Contains(message, "connection reset by peer")
}
