package execenv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	codexHooksFile        = "hooks.json"
	managedCodexHooksFile = ".multica-managed-hooks.json"
)

// prepareCodexHooks reconciles only the groups previously written by Multica
// and leaves any provider/user-owned groups in hooks.json intact. The sidecar
// makes changes to Agent bindings converge on reused task environments: a
// removed Hook is not left active just because the task directory survived.
func prepareCodexHooks(codexHome string, hooks []HookContextForEnv, logger *slog.Logger) error {
	path := filepath.Join(codexHome, codexHooksFile)
	sidecarPath := filepath.Join(codexHome, managedCodexHooksFile)

	document, err := readJSONMap(path)
	if err != nil {
		return err
	}
	previous, err := readJSONGroups(sidecarPath)
	if err != nil {
		return err
	}
	removeManagedHookGroups(document, previous)
	current := codexHookGroups(hooks)
	if len(current) > 0 {
		root, ok := document["hooks"].(map[string]any)
		if !ok {
			root = map[string]any{}
			document["hooks"] = root
		}
		for event, groups := range current {
			raw, _ := root[event].([]any)
			for _, group := range groups {
				if !containsJSONValue(raw, group) {
					raw = append(raw, group)
				}
			}
			root[event] = raw
		}
	}
	if root, ok := document["hooks"].(map[string]any); ok && len(root) == 0 {
		delete(document, "hooks")
	}

	if len(document) == 0 {
		if err := removeIfExists(path); err != nil {
			return err
		}
	} else if err := writeJSONMap(path, document); err != nil {
		return err
	}

	if len(current) == 0 {
		if err := removeIfExists(sidecarPath); err != nil {
			return err
		}
	} else if err := writeJSONGroups(sidecarPath, current); err != nil {
		return err
	}
	if logger != nil && len(current) > 0 {
		logger.Debug("execenv: reconciled managed Codex hooks", "count", countHookGroups(current), "path", path)
	}
	return nil
}

func codexHookGroups(hooks []HookContextForEnv) map[string][]any {
	groups := map[string][]any{}
	for _, hook := range hooks {
		if !codexHookProvider(hook.Providers) || strings.TrimSpace(hook.Command) == "" {
			continue
		}
		config := map[string]any{}
		if len(bytes.TrimSpace(hook.Config)) > 0 {
			_ = json.Unmarshal(hook.Config, &config)
		}
		handler := map[string]any{"type": "command", "command": strings.TrimSpace(hook.Command)}
		if timeout, ok := config["timeout"].(float64); ok && timeout > 0 {
			handler["timeout"] = int(timeout)
		}
		if message, ok := config["statusMessage"].(string); ok && strings.TrimSpace(message) != "" {
			handler["statusMessage"] = strings.TrimSpace(message)
		}
		for _, event := range uniqueSortedStrings(hook.Events) {
			if event == "" || event == "*" {
				continue
			}
			group := map[string]any{"hooks": []any{handler}}
			if matcher := strings.TrimSpace(hook.Matcher); matcher != "" {
				group["matcher"] = matcher
			}
			groups[event] = append(groups[event], group)
		}
	}
	return groups
}

func codexHookProvider(providers []string) bool {
	if len(providers) == 0 {
		return true
	}
	for _, provider := range providers {
		if strings.EqualFold(strings.TrimSpace(provider), "codex") {
			return true
		}
	}
	return false
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; !exists {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func removeManagedHookGroups(document map[string]any, previous map[string][]any) {
	root, ok := document["hooks"].(map[string]any)
	if !ok {
		return
	}
	for event, managed := range previous {
		raw, ok := root[event].([]any)
		if !ok {
			continue
		}
		kept := make([]any, 0, len(raw))
		for _, group := range raw {
			if !containsJSONValue(managed, group) {
				kept = append(kept, group)
			}
		}
		if len(kept) == 0 {
			delete(root, event)
		} else {
			root[event] = kept
		}
	}
}

func containsJSONValue(values []any, candidate any) bool {
	want, err := json.Marshal(candidate)
	if err != nil {
		return false
	}
	for _, value := range values {
		got, err := json.Marshal(value)
		if err == nil && bytes.Equal(got, want) {
			return true
		}
	}
	return false
}

func readJSONMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if document == nil {
		return map[string]any{}, nil
	}
	return document, nil
}

func readJSONGroups(path string) (map[string][]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string][]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var groups map[string][]any
	if err := json.Unmarshal(data, &groups); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if groups == nil {
		return map[string][]any{}, nil
	}
	return groups, nil
}

func writeJSONMap(path string, document map[string]any) error {
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func writeJSONGroups(path string, groups map[string][]any) error {
	data, err := json.MarshalIndent(groups, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

func countHookGroups(groups map[string][]any) int {
	count := 0
	for _, values := range groups {
		count += len(values)
	}
	return count
}
