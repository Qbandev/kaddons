package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/qbandev/kaddons/internal/addon"
	"github.com/qbandev/kaddons/internal/cluster"
	"github.com/qbandev/kaddons/internal/fetch"
	"github.com/qbandev/kaddons/internal/output"
	"github.com/qbandev/kaddons/internal/resilience"
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

	// Phase 2b: Resolve stored-data addons deterministically (no fetch, no LLM)
	orderedAddonNames := make([]string, 0, len(bestByName))
	for addonName := range bestByName {
		orderedAddonNames = append(orderedAddonNames, addonName)
	}
	sort.Strings(orderedAddonNames)

	var storedResults []output.AddonCompatibility
	var runtimeAddons []string // addon names that need runtime resolution

	for _, addonName := range orderedAddonNames {
		entry := bestByName[addonName]
		info := entry.info
		if info.DBMatch != nil && info.DBMatch.HasStoredCompatibility() {
			result := resolveFromStoredData(info, k8sVersion)
			storedResults = append(storedResults, result)
			fmt.Fprintf(os.Stderr, "Resolved %s from stored data -> %s\n", info.Name, result.Compatible)
		} else {
			runtimeAddons = append(runtimeAddons, addonName)
		}
	}

	// Fetch compatibility pages and EOL data for addons without stored data
	fmt.Fprintf(os.Stderr, "Enriching %d addons (runtime)...\n", len(runtimeAddons))
	runtimeEOLSlugLookup := make(map[string]string)
	if len(runtimeAddons) > 0 {
		products, err := fetch.EOLProducts(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: EOL product catalog fetch failed, using static fallback aliases: %v\n", err)
		} else {
			runtimeEOLSlugLookup = addon.BuildRuntimeEOLSlugLookup(products)
		}
	}
	enriched := make([]addonWithInfo, 0, len(runtimeAddons))
	fetchedURLs := make(map[string]string) // cache: URL -> content
	for _, addonName := range runtimeAddons {
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

	if len(enriched) == 0 && len(storedResults) == 0 {
		if _, err := output.FormatOutput("[]", k8sVersion, outputFormat, outputPath); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Done: %d compatible, %d incompatible, %d unknown\n", 0, 0, 0)
		return nil
	}

	// Phase 3: LLM analysis — only for runtime addons
	var client *genai.Client
	if len(enriched) > 0 {
		if strings.TrimSpace(apiKey) == "" {
			return fmt.Errorf("gemini API key is required: set GEMINI_API_KEY env var or use --key flag")
		}
		client, err = genai.NewClient(ctx, &genai.ClientConfig{
			APIKey:  apiKey,
			Backend: genai.BackendGeminiAPI,
		})
		if err != nil {
			return fmt.Errorf("creating Gemini client: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Analyzing with %s...\n", model)
	}
	return analyzeCompatibility(ctx, client, model, k8sVersion, enriched, storedResults, outputFormat, outputPath)
}

// resolveFromStoredData produces a deterministic compatibility verdict from
// the addon's pre-populated KubernetesCompatibility or KubernetesMinVersion.
func resolveFromStoredData(info addonWithInfo, k8sVersion string) output.AddonCompatibility {
	result := output.AddonCompatibility{
		Name:             info.Name,
		Namespace:        info.Namespace,
		InstalledVersion: info.Version,
		DataSource:       output.DataSourceStored,
	}

	k8sMajorMinor := normalizeK8sVersion(k8sVersion)
	finalizeResult := func() output.AddonCompatibility {
		if info.DBMatch != nil {
			result.Note = appendSourceReference(result.Note, info.DBMatch.CompatibilityMatrixURL)
		}
		return result
	}

	// Full matrix: addon-version → []k8s-versions
	if len(info.DBMatch.KubernetesCompatibility) > 0 {
		installedNorm := strings.TrimPrefix(info.Version, "v")
		matchedKey, matchedK8sVersions, matched := findDirectMatrixCompatibilityMatch(
			info.DBMatch.KubernetesCompatibility,
			installedNorm,
		)

		if !matched {
			// Some matrices are threshold-style (e.g. ">= 1.0.5" or "1.2.x"),
			// where each key means "this addon version or newer".
			thresholdKey, _, thresholdFound := findThresholdCompatibilityMatch(
				info.DBMatch.KubernetesCompatibility,
				installedNorm,
				k8sMajorMinor,
			)
			if thresholdFound {
				result.Compatible = output.StatusTrue
				result.Note = fmt.Sprintf(
					"Addon version %s satisfies threshold %s for K8s %s per stored matrix",
					info.Version,
					thresholdKey,
					k8sMajorMinor,
				)
				result.LatestCompatibleVersion = findLatestCompatibleVersion(info.DBMatch.KubernetesCompatibility, k8sMajorMinor)
				return finalizeResult()
			}

			// Installed version not in matrix — fall back to min/max version if available.
			if resolveFromMinMaxVersion(info.DBMatch, k8sMajorMinor, &result) {
				result.Note = fmt.Sprintf("Installed version %s not found in stored matrix; %s", info.Version, result.Note)
				return finalizeResult()
			}

			result.Compatible = output.StatusUnknown
			latestKey := findLatestCompatibleVersion(info.DBMatch.KubernetesCompatibility, k8sMajorMinor)
			if latestKey != "" {
				result.LatestCompatibleVersion = latestKey
				result.Note = fmt.Sprintf("Installed version %s not found in stored matrix. Latest version supporting K8s %s: %s", info.Version, k8sMajorMinor, latestKey)
			} else {
				result.Note = fmt.Sprintf("Installed version %s not found in stored compatibility matrix", info.Version)
			}
			return finalizeResult()
		}

		// Check if target K8s version is in the supported list
		for _, v := range matchedK8sVersions {
			if normalizeK8sVersion(v) == k8sMajorMinor {
				result.Compatible = output.StatusTrue
				result.Note = fmt.Sprintf("Addon version %s supports K8s %s per stored matrix", matchedKey, k8sMajorMinor)
				return finalizeResult()
			}
		}

		result.Compatible = output.StatusFalse
		latestKey := findLatestCompatibleVersion(info.DBMatch.KubernetesCompatibility, k8sMajorMinor)
		if latestKey != "" {
			result.LatestCompatibleVersion = latestKey
		}
		result.Note = fmt.Sprintf("Addon version %s does not support K8s %s per stored matrix (supports: %s)", matchedKey, k8sMajorMinor, strings.Join(matchedK8sVersions, ", "))
		return finalizeResult()
	}

	// Min/max version check (no full matrix available)
	if resolveFromMinMaxVersion(info.DBMatch, k8sMajorMinor, &result) {
		return finalizeResult()
	}

	result.Compatible = output.StatusUnknown
	result.Note = "No stored compatibility data available"
	return finalizeResult()
}

func matrixKeyMatchesInstalledVersion(matrixKey string, installedVersion string) bool {
	keyNorm := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(matrixKey)), "v")
	installedNorm := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(installedVersion), "v"))
	if keyNorm == "" {
		return false
	}

	// Skip non-semver keys that can never match a cluster-reported version.
	if isNonSemverKey(keyNorm) {
		return false
	}

	// Version range: "2.0.0-2.1.3" or "0.6.0-0.12.0"
	if lo, hi, ok := parseVersionRangeKey(keyNorm); ok {
		return versionInRange(installedNorm, lo, hi)
	}

	// Exact, prefix, or pre-release match: "1.15" matches "1.15", "1.15.0", "1.15.0-rc1"
	if keyNorm == installedNorm || strings.HasPrefix(installedNorm, keyNorm+".") || strings.HasPrefix(installedNorm, keyNorm+"-") {
		return true
	}

	// Wildcard: "1.9.x" matches "1.9", "1.9.0", "1.9.5"
	if strings.HasSuffix(keyNorm, ".x") {
		keyBase := strings.TrimSuffix(keyNorm, ".x")
		if installedNorm == keyBase || strings.HasPrefix(installedNorm, keyBase+".") {
			return true
		}
	}

	return false
}

// isNonSemverKey returns true for keys that don't start with a digit, meaning
// they represent branch names (master, HEAD, main, latest) or non-version
// identifiers (cis-1.6, ≥0.18.x). These are kept in the database for
// reference but skipped during version matching.
func isNonSemverKey(key string) bool {
	if key == "" {
		return true
	}
	first := key[0]
	return first < '0' || first > '9'
}

// parseVersionRangeKey parses keys like "2.0.0-v2.1.3" into a lo/hi pair.
// Returns false if the key is not a version range. Both sides are stripped
// of any "v" prefix.
func parseVersionRangeKey(key string) (lo string, hi string, ok bool) {
	dashIdx := strings.Index(key, "-")
	if dashIdx <= 0 || dashIdx >= len(key)-1 {
		return "", "", false
	}

	left := strings.TrimPrefix(key[:dashIdx], "v")
	right := strings.TrimPrefix(key[dashIdx+1:], "v")

	if left == "" || right == "" {
		return "", "", false
	}
	if left[0] < '0' || left[0] > '9' || right[0] < '0' || right[0] > '9' {
		return "", "", false
	}
	if !strings.Contains(left, ".") || !strings.Contains(right, ".") {
		return "", "", false
	}

	return left, right, true
}

// versionInRange checks if installedVersion is between lo and hi (inclusive),
// using numeric segment comparison.
func versionInRange(installed string, lo string, hi string) bool {
	instParts, instOK := parseAddonVersionFloor(installed)
	loParts, loOK := parseAddonVersionFloor(lo)
	hiParts, hiOK := parseAddonVersionFloor(hi)
	if !instOK || !loOK || !hiOK {
		return false
	}
	return compareAddonVersionFloors(instParts, loParts) >= 0 &&
		compareAddonVersionFloors(instParts, hiParts) <= 0
}

// sortedMatrixKeys returns the keys of a compatibility matrix in sorted order
// to ensure deterministic iteration.
func sortedMatrixKeys(matrix map[string][]string) []string {
	keys := make([]string, 0, len(matrix))
	for key := range matrix {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func findDirectMatrixCompatibilityMatch(matrix map[string][]string, installedVersion string) (string, []string, bool) {
	installedNorm := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(installedVersion), "v"))
	if installedNorm == "" {
		return "", nil, false
	}

	bestKey := ""
	bestScore := -1
	for _, key := range sortedMatrixKeys(matrix) {
		matchScore, matched := scoreMatrixKeyMatch(key, installedNorm)
		if !matched {
			continue
		}
		if matchScore > bestScore {
			bestScore = matchScore
			bestKey = key
		}
	}
	if bestKey == "" {
		return "", nil, false
	}
	return bestKey, matrix[bestKey], true
}

func scoreMatrixKeyMatch(matrixKey string, installedVersion string) (int, bool) {
	keyNorm := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(matrixKey)), "v")
	if keyNorm == "" {
		return 0, false
	}

	switch {
	case keyNorm == installedVersion:
		return 300 + len(keyNorm), true
	case strings.HasPrefix(installedVersion, keyNorm+"."),
		strings.HasPrefix(installedVersion, keyNorm+"-"):
		return 200 + len(keyNorm), true
	case strings.HasSuffix(keyNorm, ".x"):
		keyBase := strings.TrimSuffix(keyNorm, ".x")
		if installedVersion == keyBase || strings.HasPrefix(installedVersion, keyBase+".") {
			return 100 + len(keyNorm), true
		}
	case isPatchLevelKeyCompatible(keyNorm, installedVersion):
		// Matrix tables often list one patch per compatible minor line.
		return 150 + len(keyNorm), true
	}
	if matrixKeyMatchesInstalledVersion(matrixKey, installedVersion) {
		// Keep explicit/prefix/wildcard precedence above, but still allow
		// deterministic selection for supported advanced key forms (e.g. ranges).
		return 50 + len(keyNorm), true
	}
	return 0, false
}

// resolveFromMinMaxVersion checks kubernetes_min_version and kubernetes_max_version
// to produce a compatibility verdict. Returns true if a verdict was produced.
func resolveFromMinMaxVersion(a *addon.Addon, k8sMajorMinor string, result *output.AddonCompatibility) bool {
	if a.KubernetesMinVersion == "" && a.KubernetesMaxVersion == "" {
		return false
	}

	aboveMin := true
	belowMax := true

	if a.KubernetesMinVersion != "" {
		aboveMin = compareK8sVersions(k8sMajorMinor, a.KubernetesMinVersion) >= 0
	}
	if a.KubernetesMaxVersion != "" {
		belowMax = compareK8sVersions(k8sMajorMinor, a.KubernetesMaxVersion) <= 0
	}

	if aboveMin && belowMax {
		result.Compatible = output.StatusTrue
		switch {
		case a.KubernetesMinVersion != "" && a.KubernetesMaxVersion != "":
			result.Note = fmt.Sprintf("K8s %s is within supported range %s–%s", k8sMajorMinor, a.KubernetesMinVersion, a.KubernetesMaxVersion)
		case a.KubernetesMinVersion != "":
			result.Note = fmt.Sprintf("K8s %s >= minimum required %s", k8sMajorMinor, a.KubernetesMinVersion)
		default:
			result.Note = fmt.Sprintf("K8s %s <= maximum supported %s", k8sMajorMinor, a.KubernetesMaxVersion)
		}
	} else {
		result.Compatible = output.StatusFalse
		switch {
		case !aboveMin:
			result.Note = fmt.Sprintf("K8s %s < minimum required %s", k8sMajorMinor, a.KubernetesMinVersion)
		default:
			result.Note = fmt.Sprintf("K8s %s > maximum supported %s", k8sMajorMinor, a.KubernetesMaxVersion)
		}
	}
	return true
}

func findThresholdCompatibilityMatch(matrix map[string][]string, installedVersion string, targetK8sVersion string) (string, []string, bool) {
	if !looksLikeThresholdStyleMatrix(matrix) {
		return "", nil, false
	}

	installedParts, installedOK := parseAddonVersionFloor(installedVersion)
	if !installedOK {
		return "", nil, false
	}

	bestKey := ""
	var bestVersions []string
	var bestParts []int
	for key, versions := range matrix {
		if !supportsK8sVersion(versions, targetK8sVersion) {
			continue
		}
		keyParts, keyOK := parseAddonVersionFloor(key)
		if !keyOK {
			continue
		}
		if compareAddonVersionFloors(installedParts, keyParts) < 0 {
			continue
		}
		if bestParts == nil || compareAddonVersionFloors(keyParts, bestParts) > 0 {
			bestKey = key
			bestVersions = versions
			bestParts = keyParts
		}
	}
	if bestKey == "" {
		return "", nil, false
	}
	return bestKey, bestVersions, true
}

func looksLikeThresholdStyleMatrix(matrix map[string][]string) bool {
	for key := range matrix {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if strings.HasPrefix(normalizedKey, ">=") || strings.Contains(normalizedKey, ".x") || strings.HasSuffix(normalizedKey, "+") {
			return true
		}
	}
	return false
}

func supportsK8sVersion(supportedVersions []string, targetK8sVersion string) bool {
	for _, v := range supportedVersions {
		if normalizeK8sVersion(v) == targetK8sVersion {
			return true
		}
	}
	return false
}

func parseAddonVersionFloor(rawVersion string) ([]int, bool) {
	version := strings.ToLower(strings.TrimSpace(rawVersion))
	version = strings.TrimPrefix(version, ">=")
	version = strings.TrimPrefix(version, "v")
	version = strings.TrimSpace(version)
	version = strings.TrimSuffix(version, ".x")
	version = strings.TrimSuffix(version, "+")
	if version == "" {
		return nil, false
	}

	segments := strings.Split(version, ".")
	parts := make([]int, 0, len(segments))
	for _, segment := range segments {
		if segment == "" {
			break
		}
		digitEnd := 0
		for digitEnd < len(segment) && segment[digitEnd] >= '0' && segment[digitEnd] <= '9' {
			digitEnd++
		}
		if digitEnd == 0 {
			break
		}
		numericValue, err := strconv.Atoi(segment[:digitEnd])
		if err != nil {
			return nil, false
		}
		parts = append(parts, numericValue)
	}
	if len(parts) == 0 {
		return nil, false
	}
	return parts, true
}

func compareAddonVersionFloors(a []int, b []int) int {
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	for i := 0; i < maxLen; i++ {
		aPart := 0
		if i < len(a) {
			aPart = a[i]
		}
		bPart := 0
		if i < len(b) {
			bPart = b[i]
		}
		if aPart < bPart {
			return -1
		}
		if aPart > bPart {
			return 1
		}
	}
	return 0
}

// normalizeK8sVersion extracts the major.minor portion from a K8s version string.
func normalizeK8sVersion(version string) string {
	v := strings.TrimPrefix(version, "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return v
}

// compareK8sVersions compares two major.minor version strings.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func compareK8sVersions(a, b string) int {
	aParts := strings.SplitN(a, ".", 2)
	bParts := strings.SplitN(b, ".", 2)
	if len(aParts) < 2 || len(bParts) < 2 {
		aLeading := parseLeadingInt(a)
		bLeading := parseLeadingInt(b)
		if aLeading < bLeading {
			return -1
		}
		if aLeading > bLeading {
			return 1
		}
		return 0
	}
	aMajor := parseLeadingInt(aParts[0])
	bMajor := parseLeadingInt(bParts[0])
	if aMajor != bMajor {
		if aMajor < bMajor {
			return -1
		}
		return 1
	}
	aMin := parseLeadingInt(aParts[1])
	bMin := parseLeadingInt(bParts[1])
	if aMin < bMin {
		return -1
	}
	if aMin > bMin {
		return 1
	}
	return 0
}

func parseLeadingInt(value string) int {
	numericValue := 0
	for _, char := range value {
		if char < '0' || char > '9' {
			break
		}
		numericValue = numericValue*10 + int(char-'0')
	}
	return numericValue
}

func appendSourceReference(note string, sourceURL string) string {
	if sourceURL == "" || strings.Contains(note, sourceURL) {
		return note
	}
	if note == "" {
		return fmt.Sprintf("Source: %s", sourceURL)
	}
	return fmt.Sprintf("%s Source: %s", note, sourceURL)
}

func isPatchLevelKeyCompatible(matrixKey string, installedVersion string) bool {
	if !isSimpleSemverTriplet(matrixKey) {
		return false
	}
	matrixParts, matrixOK := parseAddonVersionFloor(matrixKey)
	installedParts, installedOK := parseAddonVersionFloor(installedVersion)
	if !matrixOK || !installedOK || len(matrixParts) < 3 || len(installedParts) < 3 {
		return false
	}
	if matrixParts[0] != installedParts[0] || matrixParts[1] != installedParts[1] {
		return false
	}
	return compareAddonVersionFloors(installedParts, matrixParts) >= 0
}

func isSimpleSemverTriplet(version string) bool {
	dotCount := 0
	for _, character := range version {
		if character == '.' {
			dotCount++
			continue
		}
		if character < '0' || character > '9' {
			return false
		}
	}
	return dotCount == 2
}

// findLatestCompatibleVersion finds the latest addon version in the matrix
// that supports the given K8s version.
func findLatestCompatibleVersion(matrix map[string][]string, k8sVersion string) string {
	var latest string
	var latestParts []int
	for addonVersion, k8sVersions := range matrix {
		for _, v := range k8sVersions {
			if normalizeK8sVersion(v) == k8sVersion {
				currentParts, currentOK := parseAddonVersionFloor(addonVersion)
				if latest == "" {
					latest = addonVersion
					if currentOK {
						latestParts = currentParts
					}
					break
				}

				if latestParts != nil && currentOK {
					if compareAddonVersionFloors(currentParts, latestParts) > 0 {
						latest = addonVersion
						latestParts = currentParts
					}
					break
				}

				if latestParts == nil && currentOK {
					latest = addonVersion
					latestParts = currentParts
					break
				}

				if latestParts == nil && !currentOK && addonVersion > latest {
					latest = addonVersion
				}
				break
			}
		}
	}
	return latest
}

const analysisSystemPrompt = `You are a deterministic Kubernetes addon compatibility analyzer.
You will receive exactly ONE addon at a time with structured evidence.
Return exactly one JSON object (no markdown, no code fences, no extra keys):
{
  "name": "addon-name",
  "namespace": "namespace",
  "installed_version": "v1.2.3",
  "compatible": "true"|"false"|"unknown",
  "latest_compatible_version": "v1.2.5",
  "note": "source-cited explanation"
}

Rules:
- Use only evidence provided in input.
- "compatible" must be string "true" | "false" | "unknown" only.
- If evidence is insufficient, set "compatible" to "unknown".
- Include source URL(s) in note when available.
- Preserve addon identity fields exactly from input.
- Output must be valid JSON object only.`

var evidenceLinePattern = regexp.MustCompile(`(?i)(kubernetes|k8s|compat|support|version|matrix|tested|eol|lts|deprecated|upgrade|required)`)
var evidenceK8sPattern = regexp.MustCompile(`(?i)\b(kubernetes|k8s)\b`)
var evidenceVersionPattern = regexp.MustCompile(`(?i)\bv?\d+\.\d+(?:\.\d+)?\b`)
var evidenceSupportPattern = regexp.MustCompile(`(?i)(compat|support|matrix|tested|require|recommended)`)
var evidenceNegationPattern = regexp.MustCompile(`(?i)(non[- ]matrix|without (?:a )?(?:compatibility|support|version|matrix)|no (?:compatibility|support|version|matrix)|does not (?:contain|include)|lacks?)`)

func analyzeCompatibility(ctx context.Context, client *genai.Client, model string, k8sVersion string, addons []addonWithInfo, storedResults []output.AddonCompatibility, outputFormat string, outputPath string) error {
	results := make([]output.AddonCompatibility, 0, len(storedResults)+len(addons))
	results = append(results, storedResults...)
	for addonIndex, addonInfo := range addons {
		fmt.Fprintf(
			os.Stderr,
			"Analyzing addon %d/%d: %s (%s)\n",
			addonIndex+1,
			len(addons),
			addonInfo.Name,
			addonInfo.Namespace,
		)
		result, err := analyzeSingleAddon(ctx, client, model, k8sVersion, addonInfo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: fallback to unknown for %s/%s due to analysis error: %v\n", addonInfo.Name, addonInfo.Namespace, err)
			result = output.AddonCompatibility{
				Name:             addonInfo.Name,
				Namespace:        addonInfo.Namespace,
				InstalledVersion: addonInfo.Version,
				Compatible:       output.StatusUnknown,
				Note:             fmt.Sprintf("Analysis error: %v", err),
			}
			if addonInfo.CompatibilityURL != "" {
				result.Note += fmt.Sprintf(" Source: %s", addonInfo.CompatibilityURL)
			}
		}
		result.DataSource = output.DataSourceRuntime
		fmt.Fprintf(
			os.Stderr,
			"Completed addon %d/%d: %s -> %s\n",
			addonIndex+1,
			len(addons),
			result.Name,
			result.Compatible,
		)
		results = append(results, result)
	}

	resultsJSON, err := json.Marshal(results)
	if err != nil {
		return fmt.Errorf("marshaling linear analysis results: %w", err)
	}
	formattedResults, err := output.FormatOutput(string(resultsJSON), k8sVersion, outputFormat, outputPath)
	if err != nil {
		return err
	}

	var compatible, incompatible, unknown int
	for _, result := range formattedResults {
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

type singleAddonAnalysisInput struct {
	K8sVersion string                 `json:"k8s_version"`
	Addon      map[string]interface{} `json:"addon"`
}

func analyzeSingleAddon(ctx context.Context, client *genai.Client, model, k8sVersion string, addonInfo addonWithInfo) (output.AddonCompatibility, error) {
	prunedCompatibilityEvidence := pruneEvidenceText(addonInfo.CompatibilityContent, 7000, 60)
	eolSummary := make([]string, 0, len(addonInfo.EOLData))
	for index, cycle := range addonInfo.EOLData {
		if index >= 6 {
			break
		}
		eolSummary = append(eolSummary, fmt.Sprintf("cycle=%s latest=%s eol=%v releaseDate=%s", cycle.Cycle, cycle.Latest, cycle.EOL, cycle.ReleaseDate))
	}

	analysisInput := singleAddonAnalysisInput{
		K8sVersion: k8sVersion,
		Addon: map[string]interface{}{
			"name":                    addonInfo.Name,
			"namespace":               addonInfo.Namespace,
			"installed_version":       addonInfo.Version,
			"source":                  addonInfo.Source,
			"compatibility_url":       addonInfo.CompatibilityURL,
			"compatibility_excerpt":   prunedCompatibilityEvidence,
			"fetch_error":             addonInfo.FetchError,
			"eol_summary":             eolSummary,
			"db_name":                 "",
			"db_project_url":          "",
			"db_repository":           "",
			"db_compatibility_source": "",
		},
	}
	if addonInfo.DBMatch != nil {
		analysisInput.Addon["db_name"] = addonInfo.DBMatch.Name
		analysisInput.Addon["db_project_url"] = addonInfo.DBMatch.ProjectURL
		analysisInput.Addon["db_repository"] = addonInfo.DBMatch.Repository
		analysisInput.Addon["db_compatibility_source"] = addonInfo.DBMatch.CompatibilityMatrixURL
	}

	inputJSON, err := json.MarshalIndent(analysisInput, "", "  ")
	if err != nil {
		return output.AddonCompatibility{}, fmt.Errorf("marshaling single-addon payload: %w", err)
	}

	deterministicTemperature := float32(0)
	deterministicTopP := float32(1)
	deterministicTopK := float32(1)
	deterministicSeed := int32(42)
	responseJSONSchema := map[string]interface{}{
		"type": "object",
		"required": []string{
			"name",
			"namespace",
			"installed_version",
			"compatible",
			"note",
		},
		"properties": map[string]interface{}{
			"name":                      map[string]interface{}{"type": "string"},
			"namespace":                 map[string]interface{}{"type": "string"},
			"installed_version":         map[string]interface{}{"type": "string"},
			"compatible":                map[string]interface{}{"type": "string", "enum": []string{"true", "false", "unknown"}},
			"latest_compatible_version": map[string]interface{}{"type": "string"},
			"note":                      map[string]interface{}{"type": "string"},
		},
		"additionalProperties": false,
	}

	config := &genai.GenerateContentConfig{
		SystemInstruction:  genai.NewContentFromText(analysisSystemPrompt, genai.RoleUser),
		Temperature:        &deterministicTemperature,
		TopP:               &deterministicTopP,
		TopK:               &deterministicTopK,
		Seed:               &deterministicSeed,
		CandidateCount:     1,
		ResponseMIMEType:   "application/json",
		ResponseJsonSchema: responseJSONSchema,
	}

	resp, err := generateContentWithRetry(ctx, client, model, []*genai.Content{
		genai.NewContentFromText(
			fmt.Sprintf("Analyze compatibility for this addon:\n\n%s", string(inputJSON)),
			genai.RoleUser,
		),
	}, config)
	if err != nil {
		return output.AddonCompatibility{}, err
	}
	raw := collectTextResponse(resp)
	extracted := output.ExtractJSON(raw)
	var result output.AddonCompatibility
	if err := json.Unmarshal([]byte(extracted), &result); err != nil {
		return output.AddonCompatibility{}, fmt.Errorf("parsing single-addon JSON output: %w", err)
	}
	if result.Name == "" {
		result.Name = addonInfo.Name
	}
	if result.Namespace == "" {
		result.Namespace = addonInfo.Namespace
	}
	if result.InstalledVersion == "" {
		result.InstalledVersion = addonInfo.Version
	}
	return result, nil
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

func pruneEvidenceText(input string, maxChars int, maxLines int) string {
	trimmedInput := strings.TrimSpace(input)
	if trimmedInput == "" {
		return ""
	}
	normalizedLines := make([]string, 0)
	for _, line := range strings.Split(trimmedInput, "\n") {
		cleanLine := strings.Join(strings.Fields(strings.TrimSpace(line)), " ")
		if cleanLine != "" {
			normalizedLines = append(normalizedLines, cleanLine)
		}
	}
	if len(normalizedLines) == 0 {
		return ""
	}

	type scoredAnchor struct {
		index int
		score int
	}
	lineBudget := maxLines
	if lineBudget <= 0 || lineBudget > len(normalizedLines) {
		lineBudget = len(normalizedLines)
	}
	selectedByIndex := make(map[int]bool, lineBudget)
	selectedCount := 0
	addLine := func(lineIndex int) {
		if lineIndex < 0 || lineIndex >= len(normalizedLines) {
			return
		}
		if selectedCount >= lineBudget {
			return
		}
		if selectedByIndex[lineIndex] {
			return
		}
		selectedByIndex[lineIndex] = true
		selectedCount++
	}

	// Keep a deterministic header slice for document context.
	const leadingContextLines = 12
	for lineIndex := 0; lineIndex < len(normalizedLines) && lineIndex < leadingContextLines; lineIndex++ {
		addLine(lineIndex)
	}

	anchors := make([]scoredAnchor, 0, len(normalizedLines))
	for lineIndex, line := range normalizedLines {
		score := 0
		hasK8s := evidenceK8sPattern.MatchString(line)
		hasVersion := evidenceVersionPattern.MatchString(line)
		hasSupportSignal := evidenceSupportPattern.MatchString(line)
		if hasK8s && hasVersion {
			score += 5
		}
		if hasSupportSignal {
			score += 3
		}
		if hasVersion {
			score += 2
		}
		if evidenceLinePattern.MatchString(line) {
			score += 1
		}
		if evidenceNegationPattern.MatchString(line) {
			score -= 4
		}
		if score > 0 {
			anchors = append(anchors, scoredAnchor{
				index: lineIndex,
				score: score,
			})
		}
	}
	sort.SliceStable(anchors, func(leftIndex, rightIndex int) bool {
		left := anchors[leftIndex]
		right := anchors[rightIndex]
		if left.score == right.score {
			return left.index < right.index
		}
		return left.score > right.score
	})

	// Preserve table rows and neighboring values around strongest anchors first.
	const contextBefore = 2
	const contextAfter = 2
	for _, anchor := range anchors {
		if selectedCount >= lineBudget {
			break
		}
		// Add anchor first, then nearest context lines around it.
		addLine(anchor.index)
		for offset := 1; offset <= contextBefore || offset <= contextAfter; offset++ {
			if offset <= contextAfter {
				addLine(anchor.index + offset)
			}
			if offset <= contextBefore {
				addLine(anchor.index - offset)
			}
		}
	}

	// Fill remaining slots deterministically from the start.
	for lineIndex := 0; lineIndex < len(normalizedLines) && selectedCount < lineBudget; lineIndex++ {
		addLine(lineIndex)
	}

	orderedLineIndexes := make([]int, 0, len(selectedByIndex))
	for lineIndex := range selectedByIndex {
		orderedLineIndexes = append(orderedLineIndexes, lineIndex)
	}
	sort.Ints(orderedLineIndexes)
	selectedLines := make([]string, 0, len(orderedLineIndexes))
	for _, lineIndex := range orderedLineIndexes {
		selectedLines = append(selectedLines, normalizedLines[lineIndex])
	}
	if len(selectedLines) == 0 {
		selectedLines = normalizedLines
	}

	var builder strings.Builder
	for _, line := range selectedLines {
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		if maxChars > 0 && builder.Len()+len(line) > maxChars {
			remaining := maxChars - builder.Len()
			if remaining > 0 {
				builder.WriteString(truncateToValidUTF8Prefix(line, remaining))
			}
			break
		}
		builder.WriteString(line)
	}
	return strings.TrimSpace(builder.String())
}

func truncateToValidUTF8Prefix(text string, maxBytes int) string {
	if maxBytes <= 0 || text == "" {
		return ""
	}
	if len(text) <= maxBytes {
		return text
	}

	truncateIndex := maxBytes
	for truncateIndex > 0 && !utf8.ValidString(text[:truncateIndex]) {
		truncateIndex--
	}
	if truncateIndex <= 0 {
		return ""
	}
	return text[:truncateIndex]
}

func generateContentWithRetry(
	ctx context.Context,
	client *genai.Client,
	model string,
	contents []*genai.Content,
	config *genai.GenerateContentConfig,
) (*genai.GenerateContentResponse, error) {
	const perAttemptTimeout = 90 * time.Second
	policy := resilience.RetryPolicy{
		Attempts:     3,
		InitialDelay: time.Second,
		MaxDelay:     2 * time.Second,
		Multiplier:   2,
	}
	attemptCounter := 0
	return resilience.RetryWithResult(ctx, policy, isTransientLLMError, func(callCtx context.Context) (*genai.GenerateContentResponse, error) {
		attemptCounter++
		attemptContext, cancelAttempt := context.WithTimeout(callCtx, perAttemptTimeout)
		response, err := client.Models.GenerateContent(attemptContext, model, contents, config)
		cancelAttempt()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Gemini analysis attempt %d/%d failed: %v\n", attemptCounter, policy.Attempts, err)
			if isTransientLLMError(err) && attemptCounter < policy.Attempts {
				fmt.Fprintln(os.Stderr, "Retrying Gemini analysis...")
			}
			return nil, err
		}
		return response, nil
	})
}

func isTransientLLMError(err error) bool {
	if resilience.IsRetryableNetworkError(err) {
		return true
	}
	errorText := strings.ToLower(err.Error())
	return strings.Contains(errorText, "resource_exhausted") ||
		strings.Contains(errorText, "429") ||
		strings.Contains(errorText, "503") ||
		strings.Contains(errorText, "500") ||
		strings.Contains(errorText, "unavailable")
}
