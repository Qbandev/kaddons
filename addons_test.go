package main

import (
	"testing"
)

func TestLoadAddons(t *testing.T) {
	addons, err := loadAddons()
	if err != nil {
		t.Fatalf("loadAddons() error: %v", err)
	}
	if len(addons) == 0 {
		t.Fatal("loadAddons() returned 0 addons")
	}

	// Verify first addon has required fields populated
	first := addons[0]
	if first.Name == "" {
		t.Error("first addon has empty Name")
	}
}

func TestLookupAddon_ExactMatch(t *testing.T) {
	addons := []Addon{
		{Name: "Cert Manager"},
		{Name: "Istio"},
		{Name: "Prometheus"},
	}

	matches := lookupAddon("Istio", addons)
	if len(matches) != 1 {
		t.Fatalf("lookupAddon(Istio) returned %d matches, want 1", len(matches))
	}
	if matches[0].Name != "Istio" {
		t.Errorf("lookupAddon(Istio) = %q, want %q", matches[0].Name, "Istio")
	}
}

func TestLookupAddon_CaseInsensitive(t *testing.T) {
	addons := []Addon{
		{Name: "Cert Manager"},
		{Name: "Istio"},
	}

	matches := lookupAddon("istio", addons)
	if len(matches) != 1 {
		t.Fatalf("lookupAddon(istio) returned %d matches, want 1", len(matches))
	}
	if matches[0].Name != "Istio" {
		t.Errorf("lookupAddon(istio) = %q, want %q", matches[0].Name, "Istio")
	}
}

func TestLookupAddon_PrefixMatch(t *testing.T) {
	addons := []Addon{
		{Name: "cert-manager"},
		{Name: "cert-manager-csi-driver"},
	}

	matches := lookupAddon("cert", addons)
	if len(matches) != 2 {
		t.Fatalf("lookupAddon(cert) returned %d matches, want 2", len(matches))
	}
}

func TestLookupAddon_ShortNameSkipped(t *testing.T) {
	addons := []Addon{
		{Name: "aws-alb-controller"},
	}

	// Names shorter than 4 chars skip substring matching
	matches := lookupAddon("aws", addons)
	if len(matches) != 0 {
		t.Errorf("lookupAddon(aws) returned %d matches, want 0 (short name skip)", len(matches))
	}
}

func TestLookupAddon_NoMatch(t *testing.T) {
	addons := []Addon{
		{Name: "Istio"},
		{Name: "Prometheus"},
	}

	matches := lookupAddon("nonexistent", addons)
	if len(matches) != 0 {
		t.Errorf("lookupAddon(nonexistent) returned %d matches, want 0", len(matches))
	}
}

func TestStripChartVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"cert-manager-1.14.0", "cert-manager"},
		{"prometheus-25.8.0", "prometheus"},
		{"istio-base", "istio-base"},
		{"kube-state-metrics", "kube-state-metrics"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripChartVersion(tt.input)
			if got != tt.want {
				t.Errorf("stripChartVersion(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractChartVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"cert-manager-1.14.0", "1.14.0"},
		{"prometheus-25.8.0", "25.8.0"},
		{"istio-base", ""},
		{"kube-state-metrics", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractChartVersion(tt.input)
			if got != tt.want {
				t.Errorf("extractChartVersion(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractImageTag(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"registry.k8s.io/coredns:v1.11.1", "v1.11.1"},
		{"quay.io/prometheus/node-exporter:v1.8.0", "v1.8.0"},
		{"nginx", ""},
		{"gcr.io/project/image:latest", "latest"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractImageTag(tt.input)
			if got != tt.want {
				t.Errorf("extractImageTag(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
