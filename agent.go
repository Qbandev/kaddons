package main

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/genai"
)

const maxIterations = 20

func buildSystemPrompt(eksVersion string, addonsFilter string) string {
	var sb strings.Builder
	sb.WriteString(`You are a Kubernetes addon compatibility analyzer. Your job is to determine whether addons installed in a cluster are compatible with the cluster's Kubernetes version.

Follow these steps:

`)
	if eksVersion != "" {
		sb.WriteString(fmt.Sprintf("1. The cluster Kubernetes version is: %s (provided by user, skip get_cluster_version)\n", eksVersion))
	} else {
		sb.WriteString("1. Call get_cluster_version to determine the cluster's Kubernetes version\n")
	}

	if addonsFilter != "" {
		sb.WriteString(fmt.Sprintf("2. The user wants to check these specific addons: %s. Still call list_installed_addons to get their installed versions and namespaces.\n", addonsFilter))
	} else {
		sb.WriteString("2. Call list_installed_addons to discover all addons in the cluster\n")
	}

	sb.WriteString(`3. For each addon found:
   a. Call lookup_addon_info with the addon name to find its metadata
   b. If a compatibility_matrix_url is found, call check_compatibility_url to fetch the page
   c. Analyze the fetched page content to determine if the addon version is compatible with the cluster's Kubernetes version
4. Produce your final output as a strict JSON array (no markdown code fences, no extra text) with this schema for each addon:
   {
     "name": "addon-name",
     "namespace": "namespace",
     "installed_version": "v1.2.3",
     "eks_version": "1.29",
     "compatible": true|false|null,
     "latest_compatible_version": "v1.2.5",
     "compatibility_source": "https://...",
     "note": "optional note"
   }
   - "compatible" must be null if you cannot determine compatibility
   - "note" should explain ambiguity or errors
   - Only include "latest_compatible_version" and "compatibility_source" if you found them

`)
	if addonsFilter != "" {
		sb.WriteString("Only include the requested addons in the output. If a requested addon is not found in the cluster, include it with compatible: null and a note explaining it was not found.\n")
	}

	return sb.String()
}

func runAgent(ctx context.Context, client *genai.Client, model string, namespace string, eksVersion string, addonsFilter string) error {
	addons, err := loadAddons()
	if err != nil {
		return fmt.Errorf("loading addon database: %w", err)
	}

	tools := allTools(namespace, addons)
	decls := toolDeclarations(tools)
	byName := toolsByName(tools)

	systemPrompt := buildSystemPrompt(eksVersion, addonsFilter)

	config := &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(systemPrompt, genai.RoleUser),
		Tools: []*genai.Tool{
			{FunctionDeclarations: decls},
		},
	}

	chat, err := client.Chats.Create(ctx, model, config, nil)
	if err != nil {
		return fmt.Errorf("creating chat session: %w", err)
	}

	userMsg := "Analyze the Kubernetes cluster and determine addon compatibility. Produce a JSON compatibility report."
	if addonsFilter != "" {
		userMsg = fmt.Sprintf("Check compatibility for these addons: %s. Produce a JSON compatibility report.", addonsFilter)
	}

	resp, err := chat.SendMessage(ctx, genai.Part{Text: userMsg})
	if err != nil {
		return fmt.Errorf("sending initial message: %w", err)
	}

	for i := 0; i < maxIterations; i++ {
		functionCalls := extractFunctionCalls(resp)
		if len(functionCalls) == 0 {
			printTextResponse(resp)
			return nil
		}

		var responseParts []genai.Part
		for _, fc := range functionCalls {
			tool, ok := byName[fc.Name]
			if !ok {
				responseParts = append(responseParts, genai.Part{
					FunctionResponse: &genai.FunctionResponse{
						Name:     fc.Name,
						Response: map[string]any{"error": fmt.Sprintf("unknown tool: %s", fc.Name)},
					},
				})
				continue
			}

			result, err := tool.Run(ctx, fc.Args)
			if err != nil {
				result = map[string]any{"error": err.Error()}
			}

			responseParts = append(responseParts, genai.Part{
				FunctionResponse: &genai.FunctionResponse{
					Name:     fc.Name,
					Response: result,
				},
			})
		}

		resp, err = chat.SendMessage(ctx, responseParts...)
		if err != nil {
			return fmt.Errorf("sending function responses (iteration %d): %w", i, err)
		}
	}

	return fmt.Errorf("agent loop exceeded maximum iterations (%d)", maxIterations)
}

func extractFunctionCalls(resp *genai.GenerateContentResponse) []*genai.FunctionCall {
	var calls []*genai.FunctionCall
	if resp == nil {
		return calls
	}
	for _, cand := range resp.Candidates {
		if cand.Content == nil {
			continue
		}
		for _, part := range cand.Content.Parts {
			if part.FunctionCall != nil {
				calls = append(calls, part.FunctionCall)
			}
		}
	}
	return calls
}

func printTextResponse(resp *genai.GenerateContentResponse) {
	if resp == nil {
		return
	}
	for _, cand := range resp.Candidates {
		if cand.Content == nil {
			continue
		}
		for _, part := range cand.Content.Parts {
			if part.Text != "" {
				fmt.Print(part.Text)
			}
		}
	}
	fmt.Println()
}
