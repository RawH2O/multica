package handler

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const managedHookProviderCodex = "codex"

// HookResponse is the workspace-managed coding-agent hook definition. The
// daemon uses command/events/matcher to materialize the provider config; it
// never evaluates command on the server.
type HookResponse struct {
	ID          string   `json:"id"`
	WorkspaceID string   `json:"workspace_id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Command     string   `json:"command"`
	Providers   []string `json:"providers"`
	Events      []string `json:"events"`
	Matcher     string   `json:"matcher"`
	Config      any      `json:"config"`
	CreatedBy   *string  `json:"created_by"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
	// Enabled is only populated by agent-scoped endpoints.
	Enabled *bool `json:"enabled,omitempty"`
}

type CreateHookRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Command     string   `json:"command"`
	Providers   []string `json:"providers"`
	Events      []string `json:"events"`
	Matcher     string   `json:"matcher"`
	Config      any      `json:"config"`
}

type UpdateHookRequest struct {
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	Command     *string   `json:"command"`
	Providers   *[]string `json:"providers"`
	Events      *[]string `json:"events"`
	Matcher     *string   `json:"matcher"`
	Config      any       `json:"config"`
}

type SetAgentHooksRequest struct {
	HookIDs []string `json:"hook_ids"`
}

type AddAgentHooksRequest struct {
	HookIDs []string `json:"hook_ids"`
}

// AgentHookSummary is the lightweight shape embedded in AgentResponse. The
// full definition remains available from the agent-scoped hooks endpoint.
type AgentHookSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

func hookToResponse(h db.Hook) HookResponse {
	return HookResponse{
		ID:          uuidToString(h.ID),
		WorkspaceID: uuidToString(h.WorkspaceID),
		Name:        h.Name,
		Description: h.Description,
		Command:     h.Command,
		Providers:   normalizedHookValues(h.Providers),
		Events:      normalizedHookValues(h.Events),
		Matcher:     h.Matcher,
		Config:      decodeHookConfig(h.Config),
		CreatedBy:   uuidToPtr(h.CreatedBy),
		CreatedAt:   timestampToString(h.CreatedAt),
		UpdatedAt:   timestampToString(h.UpdatedAt),
	}
}

func hookSummaryToResponse(h db.ListAgentHookSummariesRow) HookResponse {
	resp := HookResponse{
		ID:          uuidToString(h.ID),
		WorkspaceID: uuidToString(h.WorkspaceID),
		Name:        h.Name,
		Description: h.Description,
		Command:     h.Command,
		Providers:   normalizedHookValues(h.Providers),
		Events:      normalizedHookValues(h.Events),
		Matcher:     h.Matcher,
		Config:      decodeHookConfig(h.Config),
		CreatedBy:   uuidToPtr(h.CreatedBy),
		CreatedAt:   timestampToString(h.CreatedAt),
		UpdatedAt:   timestampToString(h.UpdatedAt),
	}
	resp.Enabled = &h.Enabled
	return resp
}

func decodeHookConfig(raw []byte) any {
	var config any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &config)
	}
	if config == nil {
		return map[string]any{}
	}
	return config
}

func normalizedHookValues(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return append([]string(nil), values...)
}

func normalizeHookValues(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(sanitizeNullBytes(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func validateManagedHookDefinition(w http.ResponseWriter, name, command string, providers, events []string) ([]string, []string, bool) {
	name = strings.TrimSpace(name)
	command = strings.TrimSpace(command)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return nil, nil, false
	}
	if command == "" {
		writeError(w, http.StatusBadRequest, "command is required")
		return nil, nil, false
	}
	providers = normalizeHookValues(providers)
	if len(providers) == 0 {
		providers = []string{managedHookProviderCodex}
	}
	for _, provider := range providers {
		if provider != managedHookProviderCodex {
			writeError(w, http.StatusBadRequest, "only the codex provider is supported in v1")
			return nil, nil, false
		}
	}
	events = normalizeHookValues(events)
	if len(events) == 0 {
		writeError(w, http.StatusBadRequest, "at least one event is required")
		return nil, nil, false
	}
	return providers, events, true
}

func (h *Handler) loadHookForUser(w http.ResponseWriter, r *http.Request, id string) (db.Hook, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	hookID, ok := parseUUIDOrBadRequest(w, id, "hook id")
	if !ok {
		return db.Hook{}, false
	}
	hook, err := h.Queries.GetHookInWorkspace(r.Context(), db.GetHookInWorkspaceParams{
		ID: hookID, WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "hook not found")
		return db.Hook{}, false
	}
	return hook, true
}

// canManageHook follows the Skill policy: the creator or workspace
// owner/admin can change a managed resource.
func (h *Handler) canManageHook(w http.ResponseWriter, r *http.Request, hook db.Hook) bool {
	wsID := uuidToString(hook.WorkspaceID)
	member, ok := h.requireWorkspaceRole(w, r, wsID, "hook not found", "owner", "admin", "member")
	if !ok {
		return false
	}
	if roleAllowed(member.Role, "owner", "admin") {
		return true
	}
	if hook.CreatedBy.Valid && uuidToString(hook.CreatedBy) == requestUserID(r) {
		return true
	}
	writeError(w, http.StatusForbidden, "only the hook creator can manage this hook")
	return false
}

func (h *Handler) ListHooks(w http.ResponseWriter, r *http.Request) {
	hooks, err := h.Queries.ListHooksByWorkspace(r.Context(), parseUUID(h.resolveWorkspaceID(r)))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list hooks")
		return
	}
	resp := make([]HookResponse, len(hooks))
	for i, hook := range hooks {
		resp[i] = hookToResponse(hook)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetHook(w http.ResponseWriter, r *http.Request) {
	hook, ok := h.loadHookForUser(w, r, chi.URLParam(r, "id"))
	if ok {
		writeJSON(w, http.StatusOK, hookToResponse(hook))
	}
}

func (h *Handler) CreateHook(w http.ResponseWriter, r *http.Request) {
	creatorID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var req CreateHookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	providers, events, ok := validateManagedHookDefinition(w, req.Name, req.Command, req.Providers, req.Events)
	if !ok {
		return
	}
	config, err := marshalHookConfig(req.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, "config must be valid JSON")
		return
	}
	hook, err := h.Queries.CreateHook(r.Context(), db.CreateHookParams{
		WorkspaceID: workspaceUUID,
		Name:        sanitizeNullBytes(strings.TrimSpace(req.Name)),
		Description: sanitizeNullBytes(req.Description),
		Command:     sanitizeNullBytes(strings.TrimSpace(req.Command)),
		Providers:   providers,
		Events:      events,
		Matcher:     sanitizeNullBytes(strings.TrimSpace(req.Matcher)),
		Config:      config,
		CreatedBy:   parseUUID(creatorID),
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a hook with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create hook")
		return
	}
	resp := hookToResponse(hook)
	actorType, actorID := h.resolveActor(r, creatorID, workspaceID)
	h.publish(protocol.EventHookCreated, workspaceID, actorType, actorID, map[string]any{"hook": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func marshalHookConfig(config any) ([]byte, error) {
	if config == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(config)
}

func (h *Handler) UpdateHook(w http.ResponseWriter, r *http.Request) {
	hook, ok := h.loadHookForUser(w, r, chi.URLParam(r, "id"))
	if !ok || !h.canManageHook(w, r, hook) {
		return
	}
	var req UpdateHookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name, command := hook.Name, hook.Command
	providers, events := hook.Providers, hook.Events
	if req.Name != nil {
		name = *req.Name
	}
	if req.Command != nil {
		command = *req.Command
	}
	if req.Providers != nil {
		providers = *req.Providers
	}
	if req.Events != nil {
		events = *req.Events
	}
	providers, events, ok = validateManagedHookDefinition(w, name, command, providers, events)
	if !ok {
		return
	}
	params := db.UpdateHookParams{ID: hook.ID}
	params.Name = pgtype.Text{String: sanitizeNullBytes(strings.TrimSpace(name)), Valid: true}
	params.Command = pgtype.Text{String: sanitizeNullBytes(strings.TrimSpace(command)), Valid: true}
	params.Providers = providers
	params.Events = events
	if req.Description != nil {
		params.Description = pgtype.Text{String: sanitizeNullBytes(*req.Description), Valid: true}
	}
	if req.Matcher != nil {
		params.Matcher = pgtype.Text{String: sanitizeNullBytes(strings.TrimSpace(*req.Matcher)), Valid: true}
	}
	if req.Config != nil {
		config, err := marshalHookConfig(req.Config)
		if err != nil {
			writeError(w, http.StatusBadRequest, "config must be valid JSON")
			return
		}
		params.Config = config
	}
	updated, err := h.Queries.UpdateHook(r.Context(), params)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a hook with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update hook")
		return
	}
	resp := hookToResponse(updated)
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(updated.WorkspaceID))
	h.publish(protocol.EventHookUpdated, uuidToString(updated.WorkspaceID), actorType, actorID, map[string]any{"hook": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteHook(w http.ResponseWriter, r *http.Request) {
	hook, ok := h.loadHookForUser(w, r, chi.URLParam(r, "id"))
	if !ok || !h.canManageHook(w, r, hook) {
		return
	}
	if err := h.Queries.DeleteHook(r.Context(), db.DeleteHookParams{ID: hook.ID, WorkspaceID: hook.WorkspaceID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete hook")
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(hook.WorkspaceID))
	h.publish(protocol.EventHookDeleted, uuidToString(hook.WorkspaceID), actorType, actorID, map[string]any{"hook_id": uuidToString(hook.ID)})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListAgentHooks(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	hooks, err := h.Queries.ListAgentHookSummaries(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent hooks")
		return
	}
	resp := make([]HookResponse, len(hooks))
	for i, hook := range hooks {
		resp[i] = hookSummaryToResponse(hook)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) SetAgentHooks(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok || !h.canManageAgent(w, r, agent) {
		return
	}
	var req SetAgentHooksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ids, ok := parseUUIDSliceOrBadRequest(w, req.HookIDs, "hook_ids")
	if !ok || !h.validateAgentHookIDsInWorkspace(w, r, agent, ids) {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if err := qtx.RemoveAllAgentHooks(r.Context(), agent.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear agent hooks")
		return
	}
	for _, id := range ids {
		if err := qtx.AddAgentHook(r.Context(), db.AddAgentHookParams{AgentID: agent.ID, HookID: id}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add agent hook")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}
	h.writeUpdatedAgentHooks(w, r, agent)
}

func (h *Handler) AddAgentHooks(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok || !h.canManageAgent(w, r, agent) {
		return
	}
	var req AddAgentHooksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ids, ok := parseUUIDSliceOrBadRequest(w, req.HookIDs, "hook_ids")
	if !ok || !h.validateAgentHookIDsInWorkspace(w, r, agent, ids) {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	for _, id := range ids {
		if err := qtx.AddAgentHook(r.Context(), db.AddAgentHookParams{AgentID: agent.ID, HookID: id}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add agent hook")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}
	h.writeUpdatedAgentHooks(w, r, agent)
}

func (h *Handler) SetAgentHookEnabled(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok || !h.canManageAgent(w, r, agent) {
		return
	}
	hookID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "hookId"), "hook_id")
	if !ok {
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled is required")
		return
	}
	rows, err := h.Queries.SetAgentHookEnabled(r.Context(), db.SetAgentHookEnabledParams{AgentID: agent.ID, HookID: hookID, Enabled: *req.Enabled})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update agent hook")
		return
	}
	if rows == 0 {
		writeError(w, http.StatusNotFound, "agent hook not found")
		return
	}
	h.writeUpdatedAgentHooks(w, r, agent)
}

func (h *Handler) RemoveAgentHook(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok || !h.canManageAgent(w, r, agent) {
		return
	}
	hookID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "hookId"), "hook_id")
	if !ok {
		return
	}
	if err := h.Queries.RemoveAgentHook(r.Context(), db.RemoveAgentHookParams{AgentID: agent.ID, HookID: hookID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove agent hook")
		return
	}
	h.writeUpdatedAgentHooks(w, r, agent)
}

func (h *Handler) validateAgentHookIDsInWorkspace(w http.ResponseWriter, r *http.Request, agent db.Agent, ids []pgtype.UUID) bool {
	seen := map[string]struct{}{}
	for _, id := range ids {
		key := uuidToString(id)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if _, err := h.Queries.GetHookInWorkspace(r.Context(), db.GetHookInWorkspaceParams{ID: id, WorkspaceID: agent.WorkspaceID}); err != nil {
			writeError(w, http.StatusNotFound, "hook not found")
			return false
		}
	}
	return true
}

func (h *Handler) writeUpdatedAgentHooks(w http.ResponseWriter, r *http.Request, agent db.Agent) {
	hooks, err := h.Queries.ListAgentHookSummaries(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent hooks")
		return
	}
	resp := make([]HookResponse, len(hooks))
	for i, hook := range hooks {
		resp[i] = hookSummaryToResponse(hook)
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(agent.WorkspaceID))
	h.publish(protocol.EventAgentStatus, uuidToString(agent.WorkspaceID), actorType, actorID, map[string]any{"agent_id": uuidToString(agent.ID), "hooks": resp})
	writeJSON(w, http.StatusOK, resp)
}
