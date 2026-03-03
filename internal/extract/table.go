package extract

import (
	"regexp"
	"strings"
)

// maxCells caps table processing to prevent pathological input from consuming memory.
const maxCells = 1000

// k8sVersionPattern matches Kubernetes version strings like "1.28", "1.30" (without v prefix).
var k8sVersionPattern = regexp.MustCompile(`^\d+\.\d+$`)

// addonVersionPattern matches semver-like addon version strings like "1.2.3", "v2.0.0", "0.18.x".
var addonVersionPattern = regexp.MustCompile(`^v?\d+\.\d+(?:\.\d+)?(?:[._-].*)?$`)

// ExtractMarkdownMatrix parses Markdown content for compatibility tables and returns
// a map of addon-version → []k8s-versions. Returns nil map and nil error when no
// parseable table is found — this is an expected path, not an error.
func ExtractMarkdownMatrix(content string) (map[string][]string, error) {
	tables := parseMarkdownTables(content)
	for _, table := range tables {
		if matrix := extractMatrixFromRows(table); len(matrix) > 0 {
			return matrix, nil
		}
	}
	return nil, nil
}

// ExtractHTMLMatrix parses HTML content for <table> elements containing compatibility
// data. Returns nil map and nil error when no parseable table is found.
func ExtractHTMLMatrix(content string) (map[string][]string, error) {
	tables := parseHTMLTables(content)
	for _, table := range tables {
		if matrix := extractMatrixFromRows(table); len(matrix) > 0 {
			return matrix, nil
		}
	}
	return nil, nil
}

// parseMarkdownTables extracts all Markdown tables from content.
// Each table is returned as a slice of rows, where each row is a slice of cell strings.
func parseMarkdownTables(content string) [][][]string {
	var tables [][][]string
	var currentTable [][]string
	tableCellCount := 0
	skipOversize := false

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !isMarkdownTableRow(trimmed) {
			if len(currentTable) > 0 {
				tables = append(tables, currentTable)
			}
			currentTable = nil
			tableCellCount = 0
			skipOversize = false
			continue
		}

		if skipOversize {
			continue
		}

		// Skip separator rows (e.g., "| --- | --- |" or "|:---:|:---|")
		if isMarkdownSeparatorRow(trimmed) {
			continue
		}

		cells := parseMarkdownRow(trimmed)
		tableCellCount += len(cells)
		if tableCellCount > maxCells {
			// Discard the entire over-limit table to avoid truncated matrices.
			currentTable = nil
			skipOversize = true
			continue
		}
		currentTable = append(currentTable, cells)
	}

	if len(currentTable) > 0 {
		tables = append(tables, currentTable)
	}
	return tables
}

// isMarkdownTableRow checks if a line looks like a Markdown table row.
// Requires at least two pipes and the line to start or end with a pipe,
// which avoids false positives from prose or code containing "|".
func isMarkdownTableRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.Count(trimmed, "|") < 2 {
		return false
	}
	return strings.HasPrefix(trimmed, "|") || strings.HasSuffix(trimmed, "|")
}

// isMarkdownSeparatorRow detects separator rows like "| --- | --- |" or "|:---:|".
func isMarkdownSeparatorRow(line string) bool {
	// Remove pipes and whitespace, check if only dashes, colons, and spaces remain
	cleaned := strings.ReplaceAll(line, "|", "")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return true
	}
	for _, ch := range cleaned {
		if ch != '-' && ch != ':' && ch != ' ' {
			return false
		}
	}
	return true
}

// parseMarkdownRow splits a Markdown table row into cells.
func parseMarkdownRow(line string) []string {
	// Remove leading/trailing pipe
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")

	parts := strings.Split(trimmed, "|")
	cells := make([]string, 0, len(parts))
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells
}

// htmlTableRe matches <table>...</table> blocks (non-greedy).
var htmlTableRe = regexp.MustCompile(`(?is)<table[^>]*>(.*?)</table>`)

// htmlRowRe matches <tr>...</tr> blocks.
var htmlRowRe = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)

// htmlCellRe matches <td> or <th> elements and captures their content.
var htmlCellRe = regexp.MustCompile(`(?is)<(?:td|th)[^>]*>(.*?)</(?:td|th)>`)

// htmlTagStripRe matches any HTML tag for stripping.
var htmlTagStripRe = regexp.MustCompile(`<[^>]*>`)

// parseHTMLTables extracts tables from HTML content.
func parseHTMLTables(content string) [][][]string {
	tableMatches := htmlTableRe.FindAllStringSubmatch(content, -1)
	if len(tableMatches) == 0 {
		return nil
	}

	var tables [][][]string

	for _, tableMatch := range tableMatches {
		tableContent := tableMatch[1]
		rowMatches := htmlRowRe.FindAllStringSubmatch(tableContent, -1)
		if len(rowMatches) == 0 {
			continue
		}

		var rows [][]string
		tableCellCount := 0
		for _, rowMatch := range rowMatches {
			rowContent := rowMatch[1]
			cellMatches := htmlCellRe.FindAllStringSubmatch(rowContent, -1)
			if len(cellMatches) == 0 {
				continue
			}

			var cells []string
			for _, cellMatch := range cellMatches {
				// Strip inner HTML tags and normalize whitespace
				text := htmlTagStripRe.ReplaceAllString(cellMatch[1], " ")
				text = strings.Join(strings.Fields(text), " ")
				text = strings.TrimSpace(text)
				cells = append(cells, text)
			}

			tableCellCount += len(cells)
			if tableCellCount > maxCells {
				// Discard the entire over-limit table to avoid truncated matrices.
				rows = nil
				break
			}
			rows = append(rows, cells)
		}

		if len(rows) > 0 {
			tables = append(tables, rows)
		}
	}
	return tables
}

// extractMatrixFromRows attempts to extract a K8s compatibility matrix from table rows.
// The first row is treated as headers. It looks for columns containing K8s versions
// and an addon version column.
func extractMatrixFromRows(rows [][]string) map[string][]string {
	if len(rows) < 2 {
		return nil
	}

	headers := rows[0]
	if len(headers) < 2 {
		return nil
	}

	// Identify column roles
	addonVersionCol := -1
	k8sVersionCols := identifyK8sVersionColumns(headers)

	// Strategy 1: Headers contain K8s version strings directly (e.g., "1.28", "1.29")
	// The addon version is in the first non-K8s-version column.
	if len(k8sVersionCols) > 0 {
		for i := range headers {
			if !k8sVersionCols[i] {
				addonVersionCol = i
				break
			}
		}
		if addonVersionCol < 0 {
			return nil
		}
		return buildMatrixFromVersionHeaders(rows, headers, addonVersionCol, k8sVersionCols)
	}

	// Strategy 2: Headers contain labels like "Kubernetes Version", "K8s Version"
	// and the data rows contain version strings.
	addonVersionCol, k8sCol := identifyLabeledColumns(headers)
	if addonVersionCol >= 0 && k8sCol >= 0 {
		return buildMatrixFromLabeledColumns(rows, addonVersionCol, k8sCol)
	}

	return nil
}

// identifyK8sVersionColumns returns a map of column indices whose header is a K8s version string.
func identifyK8sVersionColumns(headers []string) map[int]bool {
	cols := make(map[int]bool)
	for i, h := range headers {
		normalized := normalizeVersionCell(h)
		if isK8sVersion(normalized) {
			cols[i] = true
		}
	}
	return cols
}

// k8sHeaderPattern matches header labels that indicate a Kubernetes version column.
var k8sHeaderPattern = regexp.MustCompile(`(?i)(?:kubernetes|k8s)\s*(?:version)?`)

// addonVersionHeaderPattern matches header labels that indicate an addon version column.
var addonVersionHeaderPattern = regexp.MustCompile(`(?i)(?:version|release|addon|chart|app|operator)\s*(?:version)?`)

// identifyLabeledColumns finds addon-version and K8s-version columns by header labels.
func identifyLabeledColumns(headers []string) (addonCol int, k8sCol int) {
	addonCol = -1
	k8sCol = -1

	for i, h := range headers {
		lower := strings.ToLower(h)
		if k8sHeaderPattern.MatchString(h) {
			k8sCol = i
		} else if addonVersionHeaderPattern.MatchString(h) {
			addonCol = i
		} else if lower == "version" || lower == "release" {
			// Generic "Version" column — treat as addon version if we don't have one yet
			if addonCol < 0 {
				addonCol = i
			}
		}
	}

	return addonCol, k8sCol
}

// buildMatrixFromVersionHeaders builds a matrix when K8s versions are column headers.
// Each data row maps an addon version to the K8s versions it supports (non-empty cells).
func buildMatrixFromVersionHeaders(rows [][]string, headers []string, addonCol int, k8sCols map[int]bool) map[string][]string {
	matrix := make(map[string][]string)

	for _, row := range rows[1:] {
		if addonCol >= len(row) {
			continue
		}
		addonVersion := normalizeVersionCell(row[addonCol])
		if !isAddonVersion(addonVersion) {
			continue
		}

		var k8sVersions []string
		// Iterate headers left-to-right for deterministic output order.
		for colIdx, hdr := range headers {
			if !k8sCols[colIdx] || colIdx >= len(row) {
				continue
			}
			cell := strings.TrimSpace(row[colIdx])
			if cell == "" {
				continue
			}
			// The cell is non-empty — this addon version supports this K8s version.
			// Common patterns: checkmark, "yes", "supported", version number, or any non-empty value.
			headerVersion := normalizeK8sVersionFromHeader(hdr)
			if headerVersion != "" {
				k8sVersions = append(k8sVersions, headerVersion)
			}
		}

		if len(k8sVersions) > 0 {
			matrix[addonVersion] = k8sVersions
		}
	}

	if len(matrix) == 0 {
		return nil
	}
	return matrix
}

// buildMatrixFromLabeledColumns builds a matrix when columns are labeled
// (e.g., "Addon Version" | "Kubernetes Version"). Each row maps one addon version
// to one or more K8s versions (which may be comma/space separated in a single cell).
func buildMatrixFromLabeledColumns(rows [][]string, addonCol int, k8sCol int) map[string][]string {
	matrix := make(map[string][]string)

	for _, row := range rows[1:] {
		if addonCol >= len(row) || k8sCol >= len(row) {
			continue
		}

		addonVersion := normalizeVersionCell(row[addonCol])
		if !isAddonVersion(addonVersion) {
			continue
		}

		k8sCell := row[k8sCol]
		k8sVersions := extractK8sVersionsFromCell(k8sCell)
		if len(k8sVersions) > 0 {
			existing := matrix[addonVersion]
			matrix[addonVersion] = appendUnique(existing, k8sVersions...)
		}
	}

	if len(matrix) == 0 {
		return nil
	}
	return matrix
}

// k8sCellVersionRe extracts version numbers from a cell that may contain ranges or lists.
var k8sCellVersionRe = regexp.MustCompile(`\d+\.\d+`)

// extractK8sVersionsFromCell parses a cell that may contain one or more K8s versions,
// possibly separated by commas, spaces, dashes (ranges), or other delimiters.
func extractK8sVersionsFromCell(cell string) []string {
	matches := k8sCellVersionRe.FindAllString(cell, -1)
	var versions []string
	for _, m := range matches {
		if isK8sVersion(m) {
			versions = append(versions, m)
		}
	}
	return versions
}

// normalizeVersionCell strips common prefixes/suffixes and whitespace from version cells.
func normalizeVersionCell(cell string) string {
	s := strings.TrimSpace(cell)
	// Strip backticks and inline code markers
	s = strings.Trim(s, "`")
	s = strings.TrimSpace(s)
	return s
}

// normalizeK8sVersionFromHeader extracts a major.minor version from a header cell.
func normalizeK8sVersionFromHeader(header string) string {
	normalized := normalizeVersionCell(header)
	normalized = strings.TrimPrefix(normalized, "v")
	// Extract just the major.minor portion
	matches := k8sCellVersionRe.FindString(normalized)
	if matches != "" && isK8sVersion(matches) {
		return matches
	}
	return ""
}

// isK8sVersion validates that a string looks like a K8s version (major.minor only).
func isK8sVersion(s string) bool {
	s = strings.TrimPrefix(s, "v")
	if !k8sVersionPattern.MatchString(s) {
		return false
	}
	// Reject versions where major is not 1 (K8s is always 1.x)
	parts := strings.SplitN(s, ".", 2)
	return parts[0] == "1"
}

// isAddonVersion validates that a string looks like an addon version.
// Rejects values that match K8s version format (1.x) to prevent misclassification
// in the version-header strategy where K8s versions could appear in data rows.
func isAddonVersion(s string) bool {
	if s == "" {
		return false
	}
	if isK8sVersion(s) {
		return false
	}
	return addonVersionPattern.MatchString(s)
}

// appendUnique appends values to a slice, skipping duplicates.
func appendUnique(slice []string, values ...string) []string {
	seen := make(map[string]bool, len(slice))
	for _, v := range slice {
		seen[v] = true
	}
	for _, v := range values {
		if !seen[v] {
			slice = append(slice, v)
			seen[v] = true
		}
	}
	return slice
}
