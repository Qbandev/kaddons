package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed k8s_universal_addons.json
var addonsJSON []byte

type Addon struct {
	Name                   string `json:"name"`
	ProjectURL             string `json:"project_url"`
	Repository             string `json:"repository"`
	CompatibilityMatrixURL string `json:"compatibility_matrix_url"`
	ChangelogLocation      string `json:"changelog_location"`
	UpgradePathType        string `json:"upgrade_path_type"`
}

type AddonsFile struct {
	Addons   []Addon                `json:"addons"`
	Metadata map[string]interface{} `json:"metadata"`
}

func loadAddons() ([]Addon, error) {
	var f AddonsFile
	if err := json.Unmarshal(addonsJSON, &f); err != nil {
		return nil, fmt.Errorf("parsing embedded addons JSON: %w", err)
	}
	return f.Addons, nil
}

func lookupAddon(name string, addons []Addon) []Addon {
	lower := strings.ToLower(name)

	// Exact match (case-insensitive)
	for _, a := range addons {
		if strings.ToLower(a.Name) == lower {
			return []Addon{a}
		}
	}

	// Substring match
	var matches []Addon
	for _, a := range addons {
		if strings.Contains(strings.ToLower(a.Name), lower) {
			matches = append(matches, a)
		}
	}
	return matches
}
