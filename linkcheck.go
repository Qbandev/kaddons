package main

import (
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

type brokenLink struct {
	AddonName string
	Field     string
	URL       string
	Status    string
}

func newLinkcheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "linkcheck",
		Short: "Check all addon URLs for broken links",
		Long:  "HTTP HEAD-checks every URL in the addon database and reports broken links as a Markdown table.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLinkcheck()
		},
	}
}

func runLinkcheck() error {
	addons, err := loadAddons()
	if err != nil {
		return err
	}

	type checkItem struct {
		addonName string
		field     string
		url       string
	}

	var items []checkItem
	for _, a := range addons {
		for _, pair := range []struct {
			field string
			url   string
		}{
			{"project_url", a.ProjectURL},
			{"repository", a.Repository},
			{"compatibility_matrix_url", a.CompatibilityMatrixURL},
			{"changelog_location", a.ChangelogLocation},
		} {
			if pair.url == "" {
				continue
			}
			items = append(items, checkItem{
				addonName: a.Name,
				field:     pair.field,
				url:       pair.url,
			})
		}
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	// Deduplicate URLs before checking
	uniqueURLs := make(map[string]struct{})
	for _, it := range items {
		uniqueURLs[it.url] = struct{}{}
	}

	results := make(map[string]string, len(uniqueURLs))
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, 10)
	)

	fmt.Fprintf(os.Stderr, "Checking %d unique URLs across %d addons...\n", len(uniqueURLs), len(addons))

	for url := range uniqueURLs {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			status := checkURL(client, u)

			mu.Lock()
			results[u] = status
			mu.Unlock()
		}(url)
	}
	wg.Wait()

	// Collect broken links
	var broken []brokenLink
	for _, it := range items {
		if status := results[it.url]; status != "ok" {
			broken = append(broken, brokenLink{
				AddonName: it.addonName,
				Field:     it.field,
				URL:       it.url,
				Status:    status,
			})
		}
	}

	if len(broken) == 0 {
		fmt.Println("All links are healthy.")
		return nil
	}

	addonSet := make(map[string]struct{})
	for _, b := range broken {
		addonSet[b.AddonName] = struct{}{}
	}

	fmt.Printf("Found **%d** broken links across **%d** addons.\n\n", len(broken), len(addonSet))
	fmt.Println("| Addon Name | Field | URL | Status |")
	fmt.Println("|------------|-------|-----|--------|")
	for _, b := range broken {
		fmt.Printf("| %s | `%s` | %s | %s |\n", b.AddonName, b.Field, b.URL, b.Status)
	}

	os.Exit(1)
	return nil
}

func checkURL(client *http.Client, url string) string {
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return fmt.Sprintf("invalid URL: %v", err)
	}
	req.Header.Set("User-Agent", "kaddons-linkcheck/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	defer resp.Body.Close()

	// Some servers reject HEAD; retry with GET
	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusForbidden {
		getReq, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return fmt.Sprintf("invalid URL: %v", err)
		}
		getReq.Header.Set("User-Agent", "kaddons-linkcheck/1.0")

		resp2, err := client.Do(getReq)
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		defer resp2.Body.Close()

		if resp2.StatusCode >= 400 {
			return fmt.Sprintf("HTTP %d", resp2.StatusCode)
		}
		return "ok"
	}

	if resp.StatusCode >= 400 {
		return fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return "ok"
}
