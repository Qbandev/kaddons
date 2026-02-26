package output

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
)

// Status represents a tri-state value: "true", "false", or "unknown".
// It serializes as a JSON string and deserializes from booleans, strings, or null.
type Status string

const (
	StatusTrue    Status = "true"
	StatusFalse   Status = "false"
	StatusUnknown Status = "unknown"
)

// UnmarshalJSON normalizes JSON booleans, strings, and null into a Status value.
func (s *Status) UnmarshalJSON(data []byte) error {
	str := strings.TrimSpace(string(data))
	switch str {
	case "true", `"true"`:
		*s = StatusTrue
	case "false", `"false"`:
		*s = StatusFalse
	default:
		*s = StatusUnknown
	}
	return nil
}

// AddonCompatibility represents the compatibility verdict for a single addon.
type AddonCompatibility struct {
	Name                    string `json:"name"`
	Namespace               string `json:"namespace"`
	InstalledVersion        string `json:"installed_version"`
	Compatible              Status `json:"compatible"`
	LatestCompatibleVersion string `json:"latest_compatible_version,omitempty"`
	Note                    string `json:"note,omitempty"`
}

// CompatibilityReport is the top-level output structure.
type CompatibilityReport struct {
	K8sVersion string               `json:"k8s_version"`
	Addons     []AddonCompatibility `json:"addons"`
}

// FormatOutput parses raw JSON from the LLM and writes the selected output format.
func FormatOutput(rawJSON string, k8sVersion string, format string, outputPath string) ([]AddonCompatibility, error) {
	var addons []AddonCompatibility
	if err := json.Unmarshal([]byte(rawJSON), &addons); err != nil {
		truncated := rawJSON
		if len(truncated) > 500 {
			truncated = truncated[:500] + "... (truncated)"
		}
		return nil, fmt.Errorf("parsing agent JSON output: %w\nRaw output (first 500 chars):\n%s", err, truncated)
	}

	switch format {
	case "json":
		report := CompatibilityReport{
			K8sVersion: k8sVersion,
			Addons:     addons,
		}
		out, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshaling report: %w", err)
		}
		fmt.Println(string(out))
		return addons, nil
	case "html":
		if err := writeHTMLReport(addons, k8sVersion, outputPath); err != nil {
			return nil, err
		}
		fmt.Fprintf(os.Stderr, "HTML report written to %s\n", outputPath)
		return addons, nil
	default:
		return nil, fmt.Errorf("unsupported output format %q (supported: json, html)", format)
	}
}

type htmlReportRow struct {
	Name                    string
	Namespace               string
	InstalledVersion        string
	CompatibleClass         string
	CompatibleLabel         string
	LatestCompatibleVersion string
	Note                    string
}

type htmlReportData struct {
	K8sVersion   string
	Addons       []htmlReportRow
	Compatible   int
	Incompatible int
	Unknown      int
}

func writeHTMLReport(addons []AddonCompatibility, k8sVersion string, outputPath string) error {
	rows := make([]htmlReportRow, 0, len(addons))
	data := htmlReportData{K8sVersion: k8sVersion}
	for _, addon := range addons {
		row := htmlReportRow{
			Name:                    addon.Name,
			Namespace:               addon.Namespace,
			InstalledVersion:        addon.InstalledVersion,
			LatestCompatibleVersion: addon.LatestCompatibleVersion,
			Note:                    addon.Note,
		}
		switch addon.Compatible {
		case StatusTrue:
			row.CompatibleClass = "status-true"
			row.CompatibleLabel = "compatible"
			data.Compatible++
		case StatusFalse:
			row.CompatibleClass = "status-false"
			row.CompatibleLabel = "incompatible"
			data.Incompatible++
		default:
			row.CompatibleClass = "status-unknown"
			row.CompatibleLabel = "unknown"
			data.Unknown++
		}
		rows = append(rows, row)
	}
	data.Addons = rows

	if outputPath == "" {
		outputPath = "./kaddons-report.html"
	}
	outputPath = filepath.Clean(outputPath)
	outputDirectoryPath := filepath.Dir(outputPath)
	outputFileName := filepath.Base(outputPath)
	if outputFileName == "." || outputFileName == string(filepath.Separator) {
		return fmt.Errorf("invalid output path: %s", outputPath)
	}

	reportTemplate, err := template.New("kaddons-report").Parse(htmlTemplate)
	if err != nil {
		return fmt.Errorf("parsing HTML template: %w", err)
	}

	if err := os.MkdirAll(outputDirectoryPath, 0o750); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	outputRoot, err := os.OpenRoot(outputDirectoryPath)
	if err != nil {
		return fmt.Errorf("opening output directory root: %w", err)
	}
	defer func() { _ = outputRoot.Close() }()

	file, err := outputRoot.Create(outputFileName)
	if err != nil {
		return fmt.Errorf("creating HTML report file: %w", err)
	}
	defer func() { _ = file.Close() }()

	if err := reportTemplate.Execute(file, data); err != nil {
		return fmt.Errorf("writing HTML report: %w", err)
	}

	return nil
}

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>kaddons Compatibility Report</title>
  <style>
    body { background:#0b0f14; color:#e6edf3; font-family:Inter,system-ui,-apple-system,sans-serif; margin:0; padding:24px; }
    h1 { margin:0 0 8px 0; font-size:24px; }
    .meta { color:#9fb0c3; margin-bottom:16px; }
    .summary { display:flex; gap:12px; margin:0 0 16px 0; }
    .pill { border-radius:999px; padding:6px 10px; font-size:13px; border:1px solid #2b3541; }
    .pill-true { background:#112b1a; color:#7ee787; border-color:#2b6a3f; }
    .pill-false { background:#2d1419; color:#ff7b72; border-color:#8b2c35; }
    .pill-unknown { background:#252218; color:#d29922; border-color:#6f5a1a; }
    table { width:100%; border-collapse:collapse; background:#11161d; border:1px solid #2b3541; }
    th, td { text-align:left; padding:10px; border-bottom:1px solid #2b3541; vertical-align:top; }
    th { color:#9fb0c3; font-weight:600; font-size:12px; letter-spacing:0.03em; text-transform:uppercase; }
    tr:last-child td { border-bottom:none; }
    .status-chip { border-radius:999px; padding:4px 8px; font-size:12px; display:inline-block; border:1px solid; }
    .status-true { color:#7ee787; border-color:#2b6a3f; background:#112b1a; }
    .status-false { color:#ff7b72; border-color:#8b2c35; background:#2d1419; }
    .status-unknown { color:#d29922; border-color:#6f5a1a; background:#252218; }
    .muted { color:#9fb0c3; }
  </style>
</head>
<body>
  <h1>kaddons Compatibility Report</h1>
  <div class="meta">Kubernetes version: {{ .K8sVersion }}</div>
  <div class="summary">
    <span class="pill pill-true">Compatible: {{ .Compatible }}</span>
    <span class="pill pill-false">Incompatible: {{ .Incompatible }}</span>
    <span class="pill pill-unknown">Unknown: {{ .Unknown }}</span>
  </div>
  <table>
    <thead>
      <tr>
        <th>Name</th>
        <th>Namespace</th>
        <th>Installed</th>
        <th>K8s</th>
        <th>Compatibility</th>
        <th>Latest Compatible</th>
        <th>Notes</th>
      </tr>
    </thead>
    <tbody>
      {{ range .Addons }}
      <tr>
        <td>{{ .Name }}</td>
        <td>{{ .Namespace }}</td>
        <td>{{ .InstalledVersion }}</td>
        <td>{{ $.K8sVersion }}</td>
        <td><span class="status-chip {{ .CompatibleClass }}">{{ .CompatibleLabel }}</span></td>
        <td>{{ if .LatestCompatibleVersion }}{{ .LatestCompatibleVersion }}{{ else }}<span class="muted">N/A</span>{{ end }}</td>
        <td>{{ if .Note }}{{ .Note }}{{ else }}<span class="muted">No details</span>{{ end }}</td>
      </tr>
      {{ end }}
    </tbody>
  </table>
</body>
</html>
`

// ExtractJSON strips markdown code fences and whitespace from LLM output.
func ExtractJSON(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		start, end := 1, len(lines)-1
		if end > start && strings.HasPrefix(lines[end], "```") {
			text = strings.Join(lines[start:end], "\n")
		}
	}
	return strings.TrimSpace(text)
}
