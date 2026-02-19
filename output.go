package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

type AddonCompatibility struct {
	Name                    string `json:"name"`
	Namespace               string `json:"namespace"`
	InstalledVersion        string `json:"installed_version"`
	K8sVersion              string `json:"k8s_version"`
	Compatible              *bool  `json:"compatible"`
	LatestCompatibleVersion string `json:"latest_compatible_version,omitempty"`
	CompatibilitySource     string `json:"compatibility_source,omitempty"`
	Note                    string `json:"note,omitempty"`
}

func formatOutput(rawJSON string, format string) error {
	if format == "json" {
		fmt.Println(rawJSON)
		return nil
	}

	var addons []AddonCompatibility
	if err := json.Unmarshal([]byte(rawJSON), &addons); err != nil {
		return fmt.Errorf("parsing agent JSON output: %w\nRaw output:\n%s", err, rawJSON)
	}

	printTable(addons)
	return nil
}

func printTable(addons []AddonCompatibility) {
	type row struct {
		name, namespace, version, k8s, compat, latest, note string
	}

	header := row{"NAME", "NAMESPACE", "VERSION", "K8S", "COMPATIBLE", "LATEST", "NOTE"}
	rows := make([]row, len(addons))
	for i, a := range addons {
		compat := "unknown"
		if a.Compatible != nil {
			if *a.Compatible {
				compat = "yes"
			} else {
				compat = "NO"
			}
		}
		note := a.Note
		if len(note) > 60 {
			note = note[:57] + "..."
		}
		rows[i] = row{a.Name, a.Namespace, a.InstalledVersion, a.K8sVersion, compat, a.LatestCompatibleVersion, note}
	}

	// Calculate column widths
	widths := [7]int{
		len(header.name), len(header.namespace), len(header.version),
		len(header.k8s), len(header.compat), len(header.latest), len(header.note),
	}
	for _, r := range rows {
		fields := [7]string{r.name, r.namespace, r.version, r.k8s, r.compat, r.latest, r.note}
		for j, f := range fields {
			if len(f) > widths[j] {
				widths[j] = len(f)
			}
		}
	}

	// Box-drawing helpers
	hLine := func(left, mid, right string) string {
		var sb strings.Builder
		sb.WriteString(left)
		for i, w := range widths {
			sb.WriteString(strings.Repeat("─", w+2))
			if i < len(widths)-1 {
				sb.WriteString(mid)
			}
		}
		sb.WriteString(right)
		return sb.String()
	}

	dataLine := func(r row) string {
		fields := [7]string{r.name, r.namespace, r.version, r.k8s, r.compat, r.latest, r.note}
		var sb strings.Builder
		sb.WriteString("│")
		for i, f := range fields {
			sb.WriteString(fmt.Sprintf(" %-*s ", widths[i], f))
			if i < len(fields)-1 {
				sb.WriteString("│")
			}
		}
		sb.WriteString("│")
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

func extractJSON(text string) string {
	text = strings.TrimSpace(text)
	// Strip markdown code fences if present
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		start, end := 1, len(lines)-1
		if end > start && strings.HasPrefix(lines[end], "```") {
			text = strings.Join(lines[start:end], "\n")
		}
	}
	return strings.TrimSpace(text)
}
