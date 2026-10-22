package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var hookCmd = &cobra.Command{Use: "hook", Short: "Work with managed coding-agent hooks"}

var (
	hookListCmd   = &cobra.Command{Use: "list", Short: "List hooks in the workspace", RunE: runHookList}
	hookGetCmd    = &cobra.Command{Use: "get <id>", Short: "Get hook details", Args: exactArgs(1), RunE: runHookGet}
	hookCreateCmd = &cobra.Command{Use: "create", Short: "Create a hook", RunE: runHookCreate}
	hookUpdateCmd = &cobra.Command{Use: "update <id>", Short: "Update a hook", Args: exactArgs(1), RunE: runHookUpdate}
	hookDeleteCmd = &cobra.Command{Use: "delete <id>", Short: "Delete a hook", Args: exactArgs(1), RunE: runHookDelete}
	hookAgentCmd  = &cobra.Command{Use: "agent", Short: "Manage an agent's hook bindings"}
)

func init() {
	hookCmd.AddCommand(hookListCmd, hookGetCmd, hookCreateCmd, hookUpdateCmd, hookDeleteCmd, hookAgentCmd)
	hookListCmd.Flags().String("output", "table", "Output format: table or json")
	hookGetCmd.Flags().String("output", "json", "Output format: table or json")
	hookCreateCmd.Flags().String("name", "", "Hook name (required)")
	hookCreateCmd.Flags().String("description", "", "Hook description")
	hookCreateCmd.Flags().String("command", "", "Command executed by Codex (required)")
	hookCreateCmd.Flags().String("providers", "codex", "Comma-separated providers")
	hookCreateCmd.Flags().String("events", "PreToolUse", "Comma-separated Codex events")
	hookCreateCmd.Flags().String("matcher", "", "Optional Codex tool matcher")
	hookCreateCmd.Flags().String("config", "", "Hook runtime config as JSON (e.g. timeout/statusMessage)")
	hookCreateCmd.Flags().String("output", "json", "Output format: json or table")
	hookUpdateCmd.Flags().String("name", "", "New hook name")
	hookUpdateCmd.Flags().String("description", "", "New hook description")
	hookUpdateCmd.Flags().String("command", "", "New command")
	hookUpdateCmd.Flags().String("providers", "", "Comma-separated providers")
	hookUpdateCmd.Flags().String("events", "", "Comma-separated Codex events")
	hookUpdateCmd.Flags().String("matcher", "", "New Codex tool matcher")
	hookUpdateCmd.Flags().String("config", "", "New hook runtime config as JSON")
	hookUpdateCmd.Flags().String("output", "json", "Output format: json or table")
	hookDeleteCmd.Flags().Bool("yes", false, "Skip confirmation prompt")

	hookAgentSetCmd := &cobra.Command{Use: "set <agent-id>", Short: "Replace an agent's hooks", Args: exactArgs(1), RunE: runHookAgentSet}
	hookAgentListCmd := &cobra.Command{Use: "list <agent-id>", Short: "List an agent's hooks", Args: exactArgs(1), RunE: runHookAgentList}
	hookAgentAddCmd := &cobra.Command{Use: "add <agent-id>", Short: "Add hooks to an agent", Args: exactArgs(1), RunE: runHookAgentAdd}
	hookAgentEnableCmd := &cobra.Command{Use: "enable <agent-id> <hook-id>", Short: "Enable or disable an agent hook", Args: exactArgs(2), RunE: runHookAgentEnable}
	hookAgentRemoveCmd := &cobra.Command{Use: "remove <agent-id> <hook-id>", Short: "Remove a hook from an agent", Args: exactArgs(2), RunE: runHookAgentRemove}
	hookAgentSetCmd.Flags().String("hook-ids", "", "Comma-separated hook IDs; omit to clear all bindings")
	hookAgentAddCmd.Flags().String("hook-ids", "", "Comma-separated hook IDs (required)")
	hookAgentSetCmd.Flags().String("output", "json", "Output format: json")
	hookAgentAddCmd.Flags().String("output", "json", "Output format: json")
	hookAgentListCmd.Flags().String("output", "json", "Output format: json")
	hookAgentEnableCmd.Flags().Bool("enabled", true, "Whether the binding is enabled")
	hookAgentEnableCmd.Flags().String("output", "json", "Output format: json")
	hookAgentCmd.AddCommand(hookAgentListCmd, hookAgentSetCmd, hookAgentAddCmd, hookAgentEnableCmd, hookAgentRemoveCmd)
}

func hookIDsFlag(cmd *cobra.Command) ([]string, error) {
	raw, _ := cmd.Flags().GetString("hook-ids")
	ids := splitHookFlag(raw)
	if len(ids) == 0 {
		return nil, fmt.Errorf("--hook-ids is required")
	}
	return ids, nil
}

func splitHookFlag(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			if _, exists := seen[part]; !exists {
				seen[part] = struct{}{}
				result = append(result, part)
			}
		}
	}
	return result
}

func hookJSONConfig(cmd *cobra.Command) (any, bool, error) {
	if !cmd.Flags().Changed("config") {
		return nil, false, nil
	}
	raw, _ := cmd.Flags().GetString("config")
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, false, fmt.Errorf("--config must be valid JSON: %w", err)
	}
	return value, true, nil
}

func hookBody(cmd *cobra.Command, update bool) (map[string]any, error) {
	body := map[string]any{}
	if !update || cmd.Flags().Changed("name") {
		if value, _ := cmd.Flags().GetString("name"); value != "" || update {
			body["name"] = value
		}
	}
	if !update || cmd.Flags().Changed("description") {
		value, _ := cmd.Flags().GetString("description")
		body["description"] = value
	}
	if !update || cmd.Flags().Changed("command") {
		value, _ := cmd.Flags().GetString("command")
		if value != "" || update {
			body["command"] = value
		}
	}
	if !update || cmd.Flags().Changed("providers") {
		value, _ := cmd.Flags().GetString("providers")
		body["providers"] = splitHookFlag(value)
	}
	if !update || cmd.Flags().Changed("events") {
		value, _ := cmd.Flags().GetString("events")
		body["events"] = splitHookFlag(value)
	}
	if !update || cmd.Flags().Changed("matcher") {
		value, _ := cmd.Flags().GetString("matcher")
		body["matcher"] = value
	}
	if config, set, err := hookJSONConfig(cmd); err != nil {
		return nil, err
	} else if set {
		body["config"] = config
	}
	if update && len(body) == 0 {
		return nil, fmt.Errorf("no fields to update")
	}
	if !update && body["name"] == "" {
		return nil, fmt.Errorf("--name is required")
	}
	if !update && body["command"] == "" {
		return nil, fmt.Errorf("--command is required")
	}
	return body, nil
}

func runHookList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var hooks []map[string]any
	if err := client.GetJSON(ctx, "/api/hooks", &hooks); err != nil {
		return fmt.Errorf("list hooks: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, hooks)
	}
	rows := make([][]string, 0, len(hooks))
	for _, hook := range hooks {
		rows = append(rows, []string{strVal(hook, "id"), strVal(hook, "name"), strVal(hook, "command"), strVal(hook, "updated_at")})
	}
	cli.PrintTable(os.Stdout, []string{"ID", "NAME", "COMMAND", "UPDATED_AT"}, rows)
	return nil
}

func runHookGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var hook map[string]any
	if err := client.GetJSON(ctx, "/api/hooks/"+args[0], &hook); err != nil {
		return fmt.Errorf("get hook: %w", err)
	}
	return cli.PrintJSON(os.Stdout, hook)
}

func runHookCreate(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	body, err := hookBody(cmd, false)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/hooks", body, &result); err != nil {
		return fmt.Errorf("create hook: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Hook created: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

func runHookUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	body, err := hookBody(cmd, true)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var result map[string]any
	if err := client.PutJSON(ctx, "/api/hooks/"+args[0], body, &result); err != nil {
		return fmt.Errorf("update hook: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Hook updated: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

func runHookDelete(cmd *cobra.Command, args []string) error {
	yes, _ := cmd.Flags().GetBool("yes")
	if !yes {
		fmt.Printf("Are you sure you want to delete hook %s? This cannot be undone. [y/N] ", args[0])
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	if err := client.DeleteJSON(ctx, "/api/hooks/"+args[0]); err != nil {
		return fmt.Errorf("delete hook: %w", err)
	}
	fmt.Printf("Hook deleted: %s\n", args[0])
	return nil
}

func runHookAgentSet(cmd *cobra.Command, args []string) error {
	// An empty set is meaningful for GitOps: it removes all bindings. The
	// incremental add command still requires at least one id.
	raw, _ := cmd.Flags().GetString("hook-ids")
	return runHookAgentMutationWithIDs(cmd, args[0], "PUT", "/hooks", splitHookFlag(raw))
}
func runHookAgentAdd(cmd *cobra.Command, args []string) error {
	return runHookAgentMutation(cmd, args[0], "POST", "/hooks/add")
}

func runHookAgentList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var result any
	if err := client.GetJSON(ctx, "/api/agents/"+args[0]+"/hooks", &result); err != nil {
		return fmt.Errorf("list agent hooks: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runHookAgentMutation(cmd *cobra.Command, agentID, method, suffix string) error {
	ids, err := hookIDsFlag(cmd)
	if err != nil {
		return err
	}
	return runHookAgentMutationWithIDs(cmd, agentID, method, suffix, ids)
}

func runHookAgentMutationWithIDs(cmd *cobra.Command, agentID, method, suffix string, ids []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	body := map[string]any{"hook_ids": ids}
	var result any
	path := "/api/agents/" + agentID + suffix
	if method == "PUT" {
		err = client.PutJSON(ctx, path, body, &result)
	} else {
		err = client.PostJSON(ctx, path, body, &result)
	}
	if err != nil {
		return fmt.Errorf("update agent hooks: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runHookAgentEnable(cmd *cobra.Command, args []string) error {
	enabled, _ := cmd.Flags().GetBool("enabled")
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var result any
	if err := client.PutJSON(ctx, "/api/agents/"+args[0]+"/hooks/"+args[1]+"/enabled", map[string]any{"enabled": enabled}, &result); err != nil {
		return fmt.Errorf("toggle agent hook: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runHookAgentRemove(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	if err := client.DeleteJSON(ctx, "/api/agents/"+args[0]+"/hooks/"+args[1]); err != nil {
		return fmt.Errorf("remove agent hook: %w", err)
	}
	fmt.Printf("Hook %s removed from agent %s\n", args[1], args[0])
	return nil
}
