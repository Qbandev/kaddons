package output

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractJSON_PlainJSON(t *testing.T) {
	input := `[{"name":"test"}]`
	got := ExtractJSON(input)
	if got != input {
		t.Errorf("ExtractJSON plain = %q, want %q", got, input)
	}
}

func TestExtractJSON_WithCodeFences(t *testing.T) {
	input := "```json\n[{\"name\":\"test\"}]\n```"
	want := `[{"name":"test"}]`
	got := ExtractJSON(input)
	if got != want {
		t.Errorf("ExtractJSON fenced = %q, want %q", got, want)
	}
}

func TestExtractJSON_WithWhitespace(t *testing.T) {
	input := "  \n [{}] \n  "
	want := "[{}]"
	got := ExtractJSON(input)
	if got != want {
		t.Errorf("ExtractJSON whitespace = %q, want %q", got, want)
	}
}

func TestExtractJSON_EmptyString(t *testing.T) {
	got := ExtractJSON("")
	if got != "" {
		t.Errorf("ExtractJSON empty = %q, want empty", got)
	}
}

func TestStatusUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  Status
	}{
		{"bool true", `true`, StatusTrue},
		{"bool false", `false`, StatusFalse},
		{"string true", `"true"`, StatusTrue},
		{"string false", `"false"`, StatusFalse},
		{"null", `null`, StatusUnknown},
		{"string unknown", `"unknown"`, StatusUnknown},
		{"string garbage", `"maybe"`, StatusUnknown},
		{"empty string", `""`, StatusUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s Status
			if err := json.Unmarshal([]byte(tt.input), &s); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if s != tt.want {
				t.Errorf("Unmarshal(%s) = %q, want %q", tt.input, s, tt.want)
			}
		})
	}
}

func TestStatusMarshalJSON(t *testing.T) {
	tests := []struct {
		name  string
		input Status
		want  string
	}{
		{"true", StatusTrue, `"true"`},
		{"false", StatusFalse, `"false"`},
		{"unknown", StatusUnknown, `"unknown"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.input)
			if err != nil {
				t.Fatalf("marshal error: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("Marshal(%q) = %s, want %s", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatOutput_JSONWrapper(t *testing.T) {
	raw := `[{"name":"a","namespace":"ns","installed_version":"v1","compatible":"true","note":"ok"}]`
	// FormatOutput writes to stdout — we test the parse path only
	var addons []AddonCompatibility
	if err := json.Unmarshal([]byte(raw), &addons); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(addons) != 1 {
		t.Fatalf("got %d addons, want 1", len(addons))
	}
	if addons[0].Compatible != StatusTrue {
		t.Errorf("compatible = %q, want %q", addons[0].Compatible, StatusTrue)
	}
}

func TestFormatOutput_EmptyAddons(t *testing.T) {
	var addons []AddonCompatibility
	if err := json.Unmarshal([]byte("[]"), &addons); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(addons) != 0 {
		t.Fatalf("got %d addons, want 0", len(addons))
	}
}

func TestFormatOutput_JSONRoundTrip(t *testing.T) {
	original := []AddonCompatibility{
		{
			Name:                    "test-addon",
			Namespace:               "default",
			InstalledVersion:        "v1.2.3",
			Compatible:              StatusTrue,
			LatestCompatibleVersion: "v1.3.0",
			Note:                    "Compatible per docs",
		},
		{
			Name:             "another",
			Namespace:        "kube-system",
			InstalledVersion: "v0.1.0",
			Compatible:       StatusFalse,
			Note:             "Needs upgrade",
		},
		{
			Name:             "unknown-one",
			Namespace:        "monitoring",
			InstalledVersion: "v2.0.0",
			Compatible:       StatusUnknown,
		},
	}

	report := CompatibilityReport{
		K8sVersion: "1.30",
		Addons:     original,
	}

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded CompatibilityReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.K8sVersion != "1.30" {
		t.Errorf("k8s_version = %q, want %q", decoded.K8sVersion, "1.30")
	}
	if len(decoded.Addons) != 3 {
		t.Fatalf("got %d addons, want 3", len(decoded.Addons))
	}

	cases := []struct {
		idx    int
		compat Status
	}{
		{0, StatusTrue},
		{1, StatusFalse},
		{2, StatusUnknown},
	}
	for _, c := range cases {
		if decoded.Addons[c.idx].Compatible != c.compat {
			t.Errorf("addon[%d].compatible = %q, want %q", c.idx, decoded.Addons[c.idx].Compatible, c.compat)
		}
	}
}

func TestFormatOutput_JSONUnknownFields(t *testing.T) {
	raw := `[{"name":"a","namespace":"ns","installed_version":"v1","compatible":"true","extra_field":"ignored"}]`
	var addons []AddonCompatibility
	if err := json.Unmarshal([]byte(raw), &addons); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if addons[0].Name != "a" {
		t.Errorf("name = %q, want %q", addons[0].Name, "a")
	}
}

func TestFormatOutput_JSONBoolBackcompat(t *testing.T) {
	raw := `[{"name":"a","namespace":"ns","installed_version":"v1","compatible":true}]`
	var addons []AddonCompatibility
	if err := json.Unmarshal([]byte(raw), &addons); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if addons[0].Compatible != StatusTrue {
		t.Errorf("compatible = %q, want %q (bool true back-compat)", addons[0].Compatible, StatusTrue)
	}
}

func TestFormatOutput_JSONNullBackcompat(t *testing.T) {
	raw := `[{"name":"a","namespace":"ns","installed_version":"v1","compatible":null}]`
	var addons []AddonCompatibility
	if err := json.Unmarshal([]byte(raw), &addons); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if addons[0].Compatible != StatusUnknown {
		t.Errorf("compatible = %q, want %q (null back-compat)", addons[0].Compatible, StatusUnknown)
	}
}

func TestCompatibilityReport_NoRemovedFields(t *testing.T) {
	report := CompatibilityReport{
		K8sVersion: "1.30",
		Addons: []AddonCompatibility{
			{
				Name:                    "test",
				Namespace:               "default",
				InstalledVersion:        "v1.0.0",
				Compatible:              StatusTrue,
				LatestCompatibleVersion: "v1.1.0",
				Note:                    "Compatible",
			},
		},
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	required := []string{
		"k8s_version",
		"name",
		"namespace",
		"installed_version",
		"compatible",
		"latest_compatible_version",
		"note",
	}
	jsonStr := string(data)
	for _, field := range required {
		if !strings.Contains(jsonStr, `"`+field+`"`) {
			t.Errorf("missing required field %q in JSON output", field)
		}
	}
}

func TestMarshaledJSON_NoRemovedKeys(t *testing.T) {
	addon := AddonCompatibility{
		Name:                    "test",
		Namespace:               "ns",
		InstalledVersion:        "v1",
		Compatible:              StatusTrue,
		LatestCompatibleVersion: "v2",
		Note:                    "note",
	}
	data, err := json.Marshal(addon)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	jsonStr := string(data)

	removed := []string{"supported", "supported_until"}
	for _, key := range removed {
		if strings.Contains(jsonStr, `"`+key+`"`) {
			t.Errorf("removed field %q still present in JSON output", key)
		}
	}
}

func TestFormatOutput_JSONReturnsParsedAddons(t *testing.T) {
	raw := `[{"name":"a","namespace":"ns","installed_version":"v1","compatible":"true","note":"ok"}]`
	addons, err := FormatOutput(raw, "1.30", "json", "")
	if err != nil {
		t.Fatalf("FormatOutput(json) error = %v", err)
	}
	if len(addons) != 1 {
		t.Fatalf("FormatOutput(json) returned %d addons, want 1", len(addons))
	}
	if addons[0].Name != "a" {
		t.Fatalf("FormatOutput(json) addon name = %q, want %q", addons[0].Name, "a")
	}
}

func TestFormatOutput_HTMLWritesReportFile(t *testing.T) {
	tempDir := t.TempDir()
	reportPath := filepath.Join(tempDir, "compatibility-report.html")
	raw := `[{"name":"cert-manager","namespace":"cert-manager","installed_version":"v1.15.0","compatible":"false","latest_compatible_version":"v1.18.0","note":"Upgrade required"}]`

	addons, err := FormatOutput(raw, "1.31", "html", reportPath)
	if err != nil {
		t.Fatalf("FormatOutput(html) error = %v", err)
	}
	if len(addons) != 1 {
		t.Fatalf("FormatOutput(html) returned %d addons, want 1", len(addons))
	}

	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("reading generated report file: %v", err)
	}
	content := string(data)
	requiredSnippets := []string{
		"kaddons Compatibility Report",
		"cert-manager",
		"Upgrade required",
		"1.31",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(content, snippet) {
			t.Fatalf("generated HTML missing %q", snippet)
		}
	}
}

func TestFormatOutput_InvalidFormatReturnsError(t *testing.T) {
	raw := `[{"name":"a","namespace":"ns","installed_version":"v1","compatible":"true","note":"ok"}]`
	_, err := FormatOutput(raw, "1.30", "table", "")
	if err == nil {
		t.Fatalf("FormatOutput(table) expected error")
	}
	if !strings.Contains(err.Error(), "unsupported output format") {
		t.Fatalf("FormatOutput(table) error = %v, want unsupported output format", err)
	}
}
