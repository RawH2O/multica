package handler

import (
	"net/http/httptest"
	"testing"
)

func TestValidateManagedHookDefinitionDefaultsAndNormalizes(t *testing.T) {
	response := httptest.NewRecorder()
	providers, events, ok := validateManagedHookDefinition(
		response,
		"  mention-required ",
		"  mention-required --provider codex ",
		nil,
		[]string{"PostToolUse", " PreToolUse ", "PostToolUse"},
	)
	if !ok {
		t.Fatalf("validation failed: %s", response.Body.String())
	}
	if got, want := providers, []string{"codex"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("providers = %#v, want %#v", got, want)
	}
	if got, want := events, []string{"PostToolUse", "PreToolUse"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("events = %#v, want %#v", got, want)
	}
	if response.Code != 200 {
		t.Fatalf("status = %d, want no error response", response.Code)
	}
}

func TestValidateManagedHookDefinitionRejectsNonCodexProvider(t *testing.T) {
	response := httptest.NewRecorder()
	if _, _, ok := validateManagedHookDefinition(response, "hook", "hook-command", []string{"claude"}, []string{"Stop"}); ok {
		t.Fatal("validation accepted unsupported provider")
	}
	if response.Code != 400 {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}
