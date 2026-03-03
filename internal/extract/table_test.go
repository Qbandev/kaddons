package extract

import (
	"fmt"
	"strings"
	"testing"
)

func TestExtractMarkdownMatrix_VersionHeaders(t *testing.T) {
	content := `# Compatibility

| Addon Version | 1.28 | 1.29 | 1.30 |
| --- | --- | --- | --- |
| v1.5.0 | Yes | Yes | Yes |
| v1.4.0 | Yes | Yes | |
| v1.3.0 | Yes | | |
`
	matrix, err := ExtractMarkdownMatrix(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matrix == nil {
		t.Fatal("expected non-nil matrix")
	}

	if got := matrix["v1.5.0"]; len(got) != 3 {
		t.Errorf("v1.5.0 supports %d versions, want 3: %v", len(got), got)
	}
	if got := matrix["v1.4.0"]; len(got) != 2 {
		t.Errorf("v1.4.0 supports %d versions, want 2: %v", len(got), got)
	}
	if got := matrix["v1.3.0"]; len(got) != 1 {
		t.Errorf("v1.3.0 supports %d versions, want 1: %v", len(got), got)
	}
}

func TestExtractMarkdownMatrix_LabeledColumns(t *testing.T) {
	content := `## Supported versions

| Release | Kubernetes Version |
|---------|-------------------|
| v2.1.0 | 1.28, 1.29, 1.30 |
| v2.0.0 | 1.27, 1.28 |
`
	matrix, err := ExtractMarkdownMatrix(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matrix == nil {
		t.Fatal("expected non-nil matrix")
	}

	if got := matrix["v2.1.0"]; len(got) != 3 {
		t.Errorf("v2.1.0 supports %d versions, want 3: %v", len(got), got)
	}
	if got := matrix["v2.0.0"]; len(got) != 2 {
		t.Errorf("v2.0.0 supports %d versions, want 2: %v", len(got), got)
	}
}

func TestExtractMarkdownMatrix_NoTable(t *testing.T) {
	content := `# Just some docs

This addon is great. No tables here.
`
	matrix, err := ExtractMarkdownMatrix(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matrix != nil {
		t.Errorf("expected nil matrix for content without tables, got %v", matrix)
	}
}

func TestExtractMarkdownMatrix_NoVersionColumns(t *testing.T) {
	content := `| Feature | Status | Notes |
| --- | --- | --- |
| Auth | GA | Stable |
| Metrics | Beta | Testing |
`
	matrix, err := ExtractMarkdownMatrix(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matrix != nil {
		t.Errorf("expected nil matrix for table without version columns, got %v", matrix)
	}
}

func TestExtractMarkdownMatrix_EmptyCells(t *testing.T) {
	content := `| Version | 1.28 | 1.29 | 1.30 |
|---------|------|------|------|
| v1.0.0 | | | Yes |
| v0.9.0 | | | |
`
	matrix, err := ExtractMarkdownMatrix(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matrix == nil {
		t.Fatal("expected non-nil matrix")
	}

	if got := matrix["v1.0.0"]; len(got) != 1 {
		t.Errorf("v1.0.0 supports %d versions, want 1: %v", len(got), got)
	}
	// v0.9.0 has all empty cells, should not appear
	if got, ok := matrix["v0.9.0"]; ok {
		t.Errorf("v0.9.0 should not be in matrix (all empty cells), got %v", got)
	}
}

func TestExtractMarkdownMatrix_Checkmarks(t *testing.T) {
	content := `| Chart Version | 1.28 | 1.29 | 1.30 |
| --- | --- | --- | --- |
| 3.2.1 | ✓ | ✓ | ✓ |
| 3.1.0 | ✓ | ✓ | |
`
	matrix, err := ExtractMarkdownMatrix(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matrix == nil {
		t.Fatal("expected non-nil matrix")
	}
	if got := matrix["3.2.1"]; len(got) != 3 {
		t.Errorf("3.2.1 supports %d versions, want 3: %v", len(got), got)
	}
}

func TestExtractHTMLMatrix_BasicTable(t *testing.T) {
	content := `<html><body>
<table>
<tr><th>Version</th><th>1.28</th><th>1.29</th><th>1.30</th></tr>
<tr><td>v2.0.0</td><td>Yes</td><td>Yes</td><td>Yes</td></tr>
<tr><td>v1.9.0</td><td>Yes</td><td>Yes</td><td></td></tr>
</table>
</body></html>`

	matrix, err := ExtractHTMLMatrix(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matrix == nil {
		t.Fatal("expected non-nil matrix")
	}
	if got := matrix["v2.0.0"]; len(got) != 3 {
		t.Errorf("v2.0.0 supports %d versions, want 3: %v", len(got), got)
	}
	if got := matrix["v1.9.0"]; len(got) != 2 {
		t.Errorf("v1.9.0 supports %d versions, want 2: %v", len(got), got)
	}
}

func TestExtractHTMLMatrix_LabeledColumns(t *testing.T) {
	content := `<table>
<tr><th>App Version</th><th>Kubernetes Version</th></tr>
<tr><td>v3.0.0</td><td>1.29 - 1.31</td></tr>
<tr><td>v2.5.0</td><td>1.28, 1.29</td></tr>
</table>`

	matrix, err := ExtractHTMLMatrix(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matrix == nil {
		t.Fatal("expected non-nil matrix")
	}
	// "1.29 - 1.31" extracts the two endpoint versions; ranges are not expanded.
	if got := matrix["v3.0.0"]; len(got) != 2 {
		t.Errorf("v3.0.0 supports %d versions, want 2 (range endpoints): %v", len(got), got)
	}
	if got := matrix["v2.5.0"]; len(got) != 2 {
		t.Errorf("v2.5.0 supports %d versions, want 2: %v", len(got), got)
	}
}

func TestExtractHTMLMatrix_NoTable(t *testing.T) {
	content := `<html><body><p>No tables here</p></body></html>`

	matrix, err := ExtractHTMLMatrix(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matrix != nil {
		t.Errorf("expected nil matrix, got %v", matrix)
	}
}

func TestExtractHTMLMatrix_NestedTags(t *testing.T) {
	content := `<table>
<tr><th>Release</th><th>1.28</th><th>1.29</th></tr>
<tr><td><code>v1.0.0</code></td><td><strong>✓</strong></td><td>✓</td></tr>
</table>`

	matrix, err := ExtractHTMLMatrix(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matrix == nil {
		t.Fatal("expected non-nil matrix")
	}
	if got := matrix["v1.0.0"]; len(got) != 2 {
		t.Errorf("v1.0.0 supports %d versions, want 2: %v", len(got), got)
	}
}

func TestExtractMarkdownMatrix_CellCap(t *testing.T) {
	// Build a table that exceeds maxCells
	var sb strings.Builder
	sb.WriteString("| Version | 1.28 | 1.29 |\n")
	sb.WriteString("| --- | --- | --- |\n")
	for i := 0; i < 400; i++ {
		sb.WriteString("| v1.0.")
		sb.WriteString(strings.Repeat("0", 1))
		sb.WriteString(" | Yes | Yes |\n")
	}
	// The function should not panic or consume excessive memory
	_, err := ExtractMarkdownMatrix(sb.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractHTMLMatrix_MalformedHTML(t *testing.T) {
	content := `<table><tr><th>Version<th>1.28<tr><td>v1.0.0<td>Yes</table>`

	// Should not panic on malformed HTML
	_, err := ExtractHTMLMatrix(content)
	if err != nil {
		t.Fatalf("unexpected error on malformed HTML: %v", err)
	}
}

func TestIsK8sVersion(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"1.28", true},
		{"1.30", true},
		{"v1.29", true},
		{"2.0", false},   // K8s major is always 1
		{"1", false},     // missing minor
		{"abc", false},   // not a version
		{"1.28.0", false}, // too many parts (we want major.minor only)
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := isK8sVersion(tt.input); got != tt.want {
				t.Errorf("isK8sVersion(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsAddonVersion(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"v1.0.0", true},
		{"1.2.3", true},
		{"v2.5", true},
		{"0.18.x", true},
		{"3.2.1", true},
		{"v1.0.0-rc1", true},
		{"", false},
		{"latest", false},
		{"HEAD", false},
		{"1.28", false},  // K8s version format (1.x) rejected
		{"1.30", false},  // K8s version format (1.x) rejected
		{"v1.29", false}, // K8s version with v prefix rejected
		{"2.0", true},    // major != 1, not a K8s version
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := isAddonVersion(tt.input); got != tt.want {
				t.Errorf("isAddonVersion(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractK8sVersionsFromCell(t *testing.T) {
	tests := []struct {
		cell string
		want int
	}{
		{"1.28, 1.29, 1.30", 3},
		{"1.28 - 1.30", 2},      // range endpoints only (no expansion)
		{"1.28", 1},              // single version
		{"v1.28+", 1},            // with suffix
		{"no versions here", 0},  // no version strings
		{"", 0},                  // empty
		{">=1.28", 1},            // with prefix
	}
	for _, tt := range tests {
		t.Run(tt.cell, func(t *testing.T) {
			got := extractK8sVersionsFromCell(tt.cell)
			if len(got) != tt.want {
				t.Errorf("extractK8sVersionsFromCell(%q) returned %d versions, want %d: %v", tt.cell, len(got), tt.want, got)
			}
		})
	}
}

func TestAppendUnique(t *testing.T) {
	result := appendUnique([]string{"1.28"}, "1.29", "1.28", "1.30")
	if len(result) != 3 {
		t.Errorf("appendUnique returned %d items, want 3: %v", len(result), result)
	}
}

func TestExtractMarkdownMatrix_MultipleTablesPicksFirst(t *testing.T) {
	content := `# Table 1 - feature matrix (no versions)
| Feature | Status |
| --- | --- |
| Auth | GA |

# Table 2 - compatibility
| Version | 1.28 | 1.29 |
| --- | --- | --- |
| v1.0.0 | Yes | Yes |
`
	matrix, err := ExtractMarkdownMatrix(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matrix == nil {
		t.Fatal("expected non-nil matrix from second table")
	}
	if _, ok := matrix["v1.0.0"]; !ok {
		t.Error("expected v1.0.0 in matrix")
	}
}

func TestExtractMarkdownMatrix_K8sVersionRange(t *testing.T) {
	content := `| Version | K8s Version |
|---------|------------|
| v1.5.0 | 1.28 - 1.31 |
| v1.4.0 | 1.27 - 1.30 |
`
	matrix, err := ExtractMarkdownMatrix(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matrix == nil {
		t.Fatal("expected non-nil matrix")
	}
	// "1.28 - 1.31" should extract 1.28 and 1.31 as individual versions
	if got := matrix["v1.5.0"]; len(got) < 2 {
		t.Errorf("v1.5.0 supports %d versions, want >= 2: %v", len(got), got)
	}
}

func TestParseMarkdownTables_SeparatorVariants(t *testing.T) {
	content := `| A | B |
|:---:|:---|
| 1 | 2 |
`
	tables := parseMarkdownTables(content)
	if len(tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(tables))
	}
	// The separator row should be skipped, leaving header + 1 data row
	if len(tables[0]) != 2 {
		t.Errorf("expected 2 rows (header + 1 data), got %d", len(tables[0]))
	}
}

func TestNormalizeVersionCell(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{" v1.0.0 ", "v1.0.0"},
		{"`v1.0.0`", "v1.0.0"},
		{"  `1.28`  ", "1.28"},
		{"plain", "plain"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := normalizeVersionCell(tt.input); got != tt.want {
				t.Errorf("normalizeVersionCell(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsMarkdownTableRow_RejectsProse(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"| A | B |", true},
		{"| A | B | C |", true},
		{"A | B | C", false},                   // no leading/trailing pipe
		{"this has a | pipe in prose", false},   // single pipe, no table structure
		{"code: x || y", false},                 // logical OR in code, no table structure
		{"| single pipe only", false},           // only one pipe
		{"", false},                             // empty line
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			if got := isMarkdownTableRow(tt.line); got != tt.want {
				t.Errorf("isMarkdownTableRow(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestParseMarkdownTables_CellCapDiscardsOversizeTable(t *testing.T) {
	// Build a large table that exceeds maxCells, followed by a small table.
	// The large table should be discarded; the small table should still be parsed.
	var large strings.Builder
	large.WriteString("| Col1 | Col2 | Col3 | Col4 | Col5 |\n")
	large.WriteString("| --- | --- | --- | --- | --- |\n")
	for i := 0; i < 250; i++ {
		fmt.Fprintf(&large, "| a%d | b%d | c%d | d%d | e%d |\n", i, i, i, i, i)
	}
	// 250 rows * 5 cols = 1250 cells (exceeds maxCells=1000)

	content := large.String() + "\nSome text between tables\n\n" +
		"| Version | 1.28 | 1.29 |\n| --- | --- | --- |\n| v1.0.0 | Yes | Yes |\n"

	tables := parseMarkdownTables(content)
	if len(tables) != 1 {
		t.Fatalf("expected 1 table (over-limit discarded), got %d", len(tables))
	}
	// The surviving table should be the small compatibility table.
	if len(tables[0]) != 2 { // header + 1 data row
		t.Fatalf("expected 2 rows in surviving table, got %d", len(tables[0]))
	}
}

func TestBuildMatrixFromVersionHeaders_DeterministicOrder(t *testing.T) {
	// Headers have K8s versions in a specific order.
	headers := []string{"Addon Version", "1.30", "1.28", "1.29"}
	rows := [][]string{
		headers,
		{"v1.0.0", "Yes", "Yes", "Yes"},
	}
	k8sCols := map[int]bool{1: true, 2: true, 3: true}

	// Run multiple times to catch nondeterminism.
	for i := 0; i < 20; i++ {
		matrix := buildMatrixFromVersionHeaders(rows, headers, 0, k8sCols)
		got := matrix["v1.0.0"]
		if len(got) != 3 {
			t.Fatalf("iteration %d: expected 3 versions, got %d: %v", i, len(got), got)
		}
		// Versions must follow header order: 1.30, 1.28, 1.29
		if got[0] != "1.30" || got[1] != "1.28" || got[2] != "1.29" {
			t.Fatalf("iteration %d: expected [1.30 1.28 1.29], got %v", i, got)
		}
	}
}

func TestParseHTMLTables_CellCapDiscardsOversizeTable(t *testing.T) {
	// Build a large HTML table that exceeds maxCells, followed by a small table.
	// The large table should be discarded; the small table should still be parsed.
	var large strings.Builder
	large.WriteString("<table><tr><th>A</th><th>B</th><th>C</th><th>D</th><th>E</th></tr>")
	for i := 0; i < 250; i++ {
		fmt.Fprintf(&large, "<tr><td>%d</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td></tr>", i, i, i, i, i)
	}
	large.WriteString("</table>")

	small := `<table><tr><th>Version</th><th>1.28</th></tr><tr><td>v1.0.0</td><td>Yes</td></tr></table>`
	content := large.String() + small

	tables := parseHTMLTables(content)
	if len(tables) != 1 {
		t.Fatalf("expected 1 table (over-limit discarded), got %d", len(tables))
	}
	// The surviving table should be the small one with 2 rows.
	if len(tables[0]) != 2 {
		t.Fatalf("expected 2 rows in surviving table, got %d", len(tables[0]))
	}
}
