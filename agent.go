package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"google.golang.org/genai"
)

func runAgent(ctx context.Context, client *genai.Client, model string, namespace string, k8sVersionOverride string, addonsFilter string, outputFormat string) error {
	addonDB, err := loadAddons()
	if err != nil {
		return fmt.Errorf("loading addon database: %w", err)
	}

	// Phase 1: Deterministic data collection (no LLM involved)
	fmt.Fprintln(os.Stderr, "Detecting cluster version...")
	k8sVersion := k8sVersionOverride
	if k8sVersion == "" {
		v, err := getClusterVersion(ctx)
		if err != nil {
			return fmt.Errorf("getting cluster version: %w", err)
		}
		k8sVersion = v
	}
	fmt.Fprintf(os.Stderr, "Cluster version: %s\n", k8sVersion)

	fmt.Fprintln(os.Stderr, "Discovering installed addons...")
	detected, err := listInstalledAddons(ctx, namespace)
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
		var filtered []detectedAddon
		for _, a := range detected {
			if filterSet[strings.ToLower(a.Name)] {
				filtered = append(filtered, a)
			}
		}
		detected = filtered
	}
	fmt.Fprintf(os.Stderr, "Discovered %d workloads\n", len(detected))

	// Phase 2: Match addons against DB, deduplicate by addon name (prefer entry with version)
	type enrichedEntry struct {
		info    addonWithInfo
		dbName  string
	}
	bestByName := make(map[string]enrichedEntry)
	for _, a := range detected {
		matches := lookupAddon(a.Name, addonDB)
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
				detectedAddon: a,
				DBMatch:       &matches[0],
			},
			dbName: dbName,
		}
	}

	// Phase 3: Fetch compatibility pages for matched addons
	enriched := make([]addonWithInfo, 0, len(bestByName))
	fetchedURLs := make(map[string]string) // cache: URL -> content
	for _, entry := range bestByName {
		info := entry.info
		if info.DBMatch.CompatibilityMatrixURL != "" {
			info.CompatibilityURL = info.DBMatch.CompatibilityMatrixURL
			if cached, ok := fetchedURLs[info.CompatibilityURL]; ok {
				info.CompatibilityContent = cached
			} else {
				fmt.Fprintf(os.Stderr, "Fetching compatibility page for %s...\n", info.Name)
				content, err := fetchCompatibilityPage(ctx, info.DBMatch.CompatibilityMatrixURL)
				if err != nil {
					info.FetchError = err.Error()
				} else {
					info.CompatibilityContent = content
					fetchedURLs[info.CompatibilityURL] = content
				}
			}
		}
		enriched = append(enriched, info)
	}

	fmt.Fprintf(os.Stderr, "Matched %d known addons\n", len(enriched))
	if len(enriched) == 0 {
		return formatOutput("[]", outputFormat)
	}

	// Phase 3: LLM analysis — single call with all data pre-collected
	fmt.Fprintln(os.Stderr, "Analyzing compatibility...")
	return analyzeCompatibility(ctx, client, model, k8sVersion, enriched, outputFormat)
}

const analysisSystemPrompt = `You are a Kubernetes addon compatibility analyzer. You will receive pre-collected data about addons installed in a cluster, including their compatibility matrix page content when available.

Your ONLY job is to analyze the provided data and determine compatibility. You must produce a strict JSON array (no markdown code fences, no extra text before or after) with this schema for EACH addon provided:

{
  "name": "addon-name",
  "namespace": "namespace",
  "installed_version": "v1.2.3",
  "k8s_version": "1.29",
  "compatible": true|false|null,
  "latest_compatible_version": "v1.2.5",
  "compatibility_source": "https://...",
  "note": "brief explanation"
}

Rules:
- You MUST include ALL addons from the input — do not skip any
- "compatible" must be null if you cannot determine compatibility from the provided data
- "note" should be a brief explanation of your analysis
- Only include "latest_compatible_version" if you found evidence in the compatibility page
- "compatibility_source" should be the URL of the compatibility page if one was provided
- Base your analysis ONLY on the page content provided — do not hallucinate compatibility information`

func analyzeCompatibility(ctx context.Context, client *genai.Client, model string, k8sVersion string, addons []addonWithInfo, outputFormat string) error {
	// Build the data payload for the LLM
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

	config := &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(analysisSystemPrompt, genai.RoleUser),
	}

	resp, err := client.Models.GenerateContent(ctx, model, []*genai.Content{
		genai.NewContentFromText(
			fmt.Sprintf("Analyze compatibility for these addons:\n\n%s", string(dataJSON)),
			genai.RoleUser,
		),
	}, config)
	if err != nil {
		return fmt.Errorf("LLM analysis failed: %w", err)
	}

	raw := collectTextResponse(resp)
	return formatOutput(extractJSON(raw), outputFormat)
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
