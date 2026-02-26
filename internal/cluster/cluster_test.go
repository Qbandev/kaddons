package cluster

import (
	"errors"
	"testing"
)

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

func TestIsRetryableKubectlError_FromWrappedStderrMessage(t *testing.T) {
	err := errors.New("exit status 1: Unable to connect to the server: connection refused")
	if !isRetryableKubectlError(err) {
		t.Fatalf("expected wrapped kubectl stderr network error to be retryable")
	}
}
