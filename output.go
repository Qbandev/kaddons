package main

type AddonCompatibility struct {
	Name                    string `json:"name"`
	Namespace               string `json:"namespace"`
	InstalledVersion        string `json:"installed_version"`
	EKSVersion              string `json:"eks_version"`
	Compatible              *bool  `json:"compatible"`
	LatestCompatibleVersion string `json:"latest_compatible_version,omitempty"`
	CompatibilitySource     string `json:"compatibility_source,omitempty"`
	Note                    string `json:"note,omitempty"`
}
