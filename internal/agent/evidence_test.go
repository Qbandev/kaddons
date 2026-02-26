package agent

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPruneEvidenceText_IsDeterministicAndBounded(t *testing.T) {
	input := `
intro
this line has kubernetes compatibility matrix for k8s 1.29 1.30
another noise line
supported versions include 1.31 and 1.32
deprecated release note
`

	first := pruneEvidenceText(input, 120, 3)
	second := pruneEvidenceText(input, 120, 3)

	if first != second {
		t.Fatalf("pruneEvidenceText() is not deterministic:\nfirst=%q\nsecond=%q", first, second)
	}
	if len(first) > 120 {
		t.Fatalf("pruneEvidenceText() length = %d, want <= 120", len(first))
	}
}

func TestPruneEvidenceText_FallsBackWhenNoKeywordMatch(t *testing.T) {
	input := "line one\nline two\nline three"
	got := pruneEvidenceText(input, 40, 2)
	want := "line one\nline two"
	if got != want {
		t.Fatalf("pruneEvidenceText() = %q, want %q", got, want)
	}
}

func TestPruneEvidenceText_KeepsContextAroundKeywordLines(t *testing.T) {
	input := strings.Join([]string{
		"header line",
		"compatibility matrix",
		"1.30 -> supported",
		"1.31 -> supported",
		"1.32 -> unsupported",
		"tail line",
	}, "\n")

	got := pruneEvidenceText(input, 500, 20)
	if !strings.Contains(got, "compatibility matrix") {
		t.Fatalf("expected keyword line to be preserved, got %q", got)
	}
	if !strings.Contains(got, "1.31 -> supported") {
		t.Fatalf("expected nearby version row to be preserved, got %q", got)
	}
}

func TestPruneEvidenceText_PrioritizesStrongLateCompatibilitySignals(t *testing.T) {
	inputLines := []string{
		"intro line 1",
		"intro line 2",
		"intro line 3",
		"intro line 4",
		"intro line 5",
		"intro line 6",
		"intro line 7",
		"intro line 8",
		"intro line 9",
		"intro line 10",
		"intro line 11",
		"intro line 12",
		"random setup text",
		"another non-matrix line",
		"Tested versions",
		"Argo CD version | Kubernetes versions",
		"3.3 | v1.34, v1.33, v1.32, v1.31",
	}

	got := pruneEvidenceText(strings.Join(inputLines, "\n"), 4000, 16)
	if !strings.Contains(got, "v1.31") {
		t.Fatalf("expected late strong compatibility line to be included, got %q", got)
	}
}

func TestIsTransientLLMError_ClassifiesRetryable(t *testing.T) {
	if !isTransientLLMError(errors.New("unexpected EOF")) {
		t.Fatalf("expected EOF-like error to be retryable")
	}
	if !isTransientLLMError(errors.New("HTTP 429 resource_exhausted")) {
		t.Fatalf("expected 429 error to be retryable")
	}
	if isTransientLLMError(errors.New("invalid request body")) {
		t.Fatalf("expected terminal error to be non-retryable")
	}
}

func TestTruncateToValidUTF8Prefix_RespectsRuneBoundary(t *testing.T) {
	text := "hello 😀 world"
	got := truncateToValidUTF8Prefix(text, 8) // truncates inside emoji byte sequence if naive
	if !utf8.ValidString(got) {
		t.Fatalf("truncateToValidUTF8Prefix() returned invalid UTF-8: %q", got)
	}
	if got != "hello " {
		t.Fatalf("truncateToValidUTF8Prefix() = %q, want %q", got, "hello ")
	}
}
