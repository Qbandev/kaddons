package main

import (
	"testing"
)

func TestExtractJSON_PlainJSON(t *testing.T) {
	input := `[{"name": "test"}]`
	got := extractJSON(input)
	if got != input {
		t.Errorf("extractJSON() = %q, want %q", got, input)
	}
}

func TestExtractJSON_WithCodeFences(t *testing.T) {
	input := "```json\n[{\"name\": \"test\"}]\n```"
	want := `[{"name": "test"}]`
	got := extractJSON(input)
	if got != want {
		t.Errorf("extractJSON() = %q, want %q", got, want)
	}
}

func TestExtractJSON_WithWhitespace(t *testing.T) {
	input := "  \n  [{\"name\": \"test\"}]  \n  "
	want := `[{"name": "test"}]`
	got := extractJSON(input)
	if got != want {
		t.Errorf("extractJSON() = %q, want %q", got, want)
	}
}

func TestExtractJSON_EmptyString(t *testing.T) {
	got := extractJSON("")
	if got != "" {
		t.Errorf("extractJSON(\"\") = %q, want empty", got)
	}
}
