package execenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareCodexHooksMaterializesAndReconciles(t *testing.T) {
	home := t.TempDir()
	hooks := []HookContextForEnv{{
		Name: "mention-required", Command: "mention-required --provider codex",
		Providers: []string{"codex"}, Events: []string{"Stop", "PreToolUse"}, Matcher: "Bash",
	}}
	if err := prepareCodexHooks(home, hooks, nil); err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	data, err := os.ReadFile(filepath.Join(home, codexHooksFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	root := document["hooks"].(map[string]any)
	for _, event := range []string{"Stop", "PreToolUse"} {
		groups := root[event].([]any)
		if len(groups) != 1 {
			t.Fatalf("event %s groups = %d, want 1", event, len(groups))
		}
		group := groups[0].(map[string]any)
		if group["matcher"] != "Bash" {
			t.Fatalf("matcher = %v", group["matcher"])
		}
	}

	if err := prepareCodexHooks(home, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, codexHooksFile)); !os.IsNotExist(err) {
		t.Fatalf("managed hooks file still exists, err=%v", err)
	}
}

func TestPrepareCodexHooksPreservesUserGroups(t *testing.T) {
	home := t.TempDir()
	initial := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"user-hook"}]}]}}`
	if err := os.WriteFile(filepath.Join(home, codexHooksFile), []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	managed := HookContextForEnv{Name: "managed", Command: "managed-hook", Providers: []string{"codex"}, Events: []string{"Stop"}}
	if err := prepareCodexHooks(home, []HookContextForEnv{managed}, nil); err != nil {
		t.Fatal(err)
	}
	if err := prepareCodexHooks(home, nil, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, codexHooksFile))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	groups := document["hooks"].(map[string]any)["Stop"].([]any)
	if len(groups) != 1 || groups[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"] != "user-hook" {
		t.Fatalf("user hook was not preserved: %s", data)
	}
}
