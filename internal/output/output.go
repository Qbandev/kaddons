package output

import (
	"encoding/json"
	"fmt"
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

// FormatOutput parses raw JSON from the LLM and writes formatted output to stdout.
func FormatOutput(rawJSON string, k8sVersion string, format string) error {
	var addons []AddonCompatibility
	if err := json.Unmarshal([]byte(rawJSON), &addons); err != nil {
		truncated := rawJSON
		if len(truncated) > 500 {
			truncated = truncated[:500] + "... (truncated)"
		}
		return fmt.Errorf("parsing agent JSON output: %w\nRaw output (first 500 chars):\n%s", err, truncated)
	}

	if format == "json" {
		report := CompatibilityReport{
			K8sVersion: k8sVersion,
			Addons:     addons,
		}
		out, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling report: %w", err)
		}
		fmt.Println(string(out))
		return nil
	}

	printTable(addons, k8sVersion)
	return nil
}

func printTable(addons []AddonCompatibility, k8sVersion string) {
	type row struct {
		name, namespace, version, k8s, compat, latest, note string
	}

	header := row{"NAME", "NAMESPACE", "VERSION", "K8S", "COMPATIBLE", "LATEST", "NOTE"}
	rows := make([]row, len(addons))
	for i, a := range addons {
		var compat string
		switch a.Compatible {
		case StatusTrue:
			compat = "yes"
		case StatusFalse:
			compat = "NO"
		default:
			compat = "unknown"
		}

		note := a.Note
		if len(note) > 60 {
			note = note[:57] + "..."
		}
		rows[i] = row{a.Name, a.Namespace, a.InstalledVersion, k8sVersion, compat, a.LatestCompatibleVersion, note}
	}

	// Calculate column widths
	widths := []int{
		len(header.name), len(header.namespace), len(header.version),
		len(header.k8s), len(header.compat), len(header.latest), len(header.note),
	}
	for _, r := range rows {
		fieldValues := []string{r.name, r.namespace, r.version, r.k8s, r.compat, r.latest, r.note}
		for idx := range fieldValues {
			if len(fieldValues[idx]) > widths[idx] {
				widths[idx] = len(fieldValues[idx])
			}
		}
	}

	hLine := func(left, mid, right string) string {
		var sb strings.Builder
		_, _ = sb.WriteString(left)
		for i, w := range widths {
			_, _ = sb.WriteString(strings.Repeat("─", w+2))
			if i < len(widths)-1 {
				_, _ = sb.WriteString(mid)
			}
		}
		_, _ = sb.WriteString(right)
		return sb.String()
	}

	dataLine := func(r row) string {
		fields := []string{r.name, r.namespace, r.version, r.k8s, r.compat, r.latest, r.note}
		var sb strings.Builder
		_, _ = sb.WriteString("│")
		for i, f := range fields {
			_, _ = fmt.Fprintf(&sb, " %-*s ", widths[i], f)
			if i < len(fields)-1 {
				_, _ = sb.WriteString("│")
			}
		}
		_, _ = sb.WriteString("│")
		return sb.String()
	}

	fmt.Println(hLine("┌", "┬", "┐"))
	fmt.Println(dataLine(header))
	fmt.Println(hLine("├", "┼", "┤"))
	for i, r := range rows {
		fmt.Println(dataLine(r))
		if i < len(rows)-1 {
			fmt.Println(hLine("├", "┼", "┤"))
		}
	}
	fmt.Println(hLine("└", "┴", "┘"))
}

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
