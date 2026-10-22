package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// publishSystemIssueComment publishes a persisted platform comment and fires
// the explicit assignee trigger. System comments intentionally bypass the
// generic mention listeners, so callers that want an agent/squad wake must use
// this helper after the comment has committed.
func (h *Handler) publishSystemIssueComment(ctx context.Context, issue db.Issue, comment db.Comment, issueRevision int64) {
	h.publish(protocol.EventCommentCreated, uuidToString(issue.WorkspaceID), "system", "", map[string]any{
		"comment":             commentToResponse(comment, nil, nil),
		"issue_title":         issue.Title,
		"issue_assignee_type": textToPtr(issue.AssigneeType),
		"issue_assignee_id":   uuidToPtr(issue.AssigneeID),
		"issue_status":        issue.Status,
		"issue_revision":      issueRevision,
	})
	h.dispatchSystemIssueAssigneeTrigger(ctx, issue, comment)
}

// buildSystemIssueAssigneeMention gives a durable system comment the same
// readable target marker as a normal mention. Routing is still explicit below;
// system comments are deliberately excluded from the generic mention listener.
func (h *Handler) buildSystemIssueAssigneeMention(ctx context.Context, issue db.Issue) string {
	if !issue.AssigneeType.Valid || !issue.AssigneeID.Valid {
		return ""
	}
	label, ok := h.resolveSystemAssigneeMentionLabel(ctx, issue.WorkspaceID, issue.AssigneeType.String, issue.AssigneeID)
	if !ok {
		return ""
	}
	return fmt.Sprintf("[@%s](mention://%s/%s) ", label, issue.AssigneeType.String, uuidToString(issue.AssigneeID))
}

func (h *Handler) resolveSystemAssigneeMentionLabel(ctx context.Context, workspaceID pgtype.UUID, assigneeType string, assigneeID pgtype.UUID) (string, bool) {
	switch assigneeType {
	case "agent":
		agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: assigneeID, WorkspaceID: workspaceID})
		if err != nil {
			return "", false
		}
		return sanitizeSystemMentionLabel(agent.Name), true
	case "squad":
		squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: assigneeID, WorkspaceID: workspaceID})
		if err != nil {
			return "", false
		}
		return sanitizeSystemMentionLabel(squad.Name), true
	default:
		return "", false
	}
}

func sanitizeSystemMentionLabel(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "]", ""))
	if name == "" {
		return "assignee"
	}
	return name
}

// dispatchSystemIssueAssigneeTrigger is the explicit routing edge paired with
// a system comment. It follows the current TaskService APIs and applies the
// same pending-task guard used by ordinary mention dispatches.
func (h *Handler) dispatchSystemIssueAssigneeTrigger(ctx context.Context, issue db.Issue, systemComment db.Comment) {
	if h.TaskService == nil || !issue.AssigneeType.Valid || !issue.AssigneeID.Valid {
		return
	}

	var agentID pgtype.UUID
	var squadID pgtype.UUID
	switch issue.AssigneeType.String {
	case "agent":
		agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: issue.AssigneeID, WorkspaceID: issue.WorkspaceID})
		if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
			return
		}
		agentID = issue.AssigneeID
	case "squad":
		squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: issue.AssigneeID, WorkspaceID: issue.WorkspaceID})
		if err != nil || squad.ArchivedAt.Valid {
			return
		}
		agent, err := h.Queries.GetAgent(ctx, squad.LeaderID)
		if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
			return
		}
		agentID = squad.LeaderID
		squadID = squad.ID
	default:
		return
	}

	hasPending, err := h.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: issue.ID,
		AgentID: agentID,
		HeadSha: h.TaskService.ResolveIssueReviewSHAParam(ctx, issue.ID),
	})
	if err != nil || hasPending {
		return
	}

	var enqueueErr error
	if squadID.Valid {
		_, enqueueErr = h.TaskService.EnqueueTaskForSquadLeader(ctx, issue, agentID, squadID, systemComment.ID, service.OriginDerived)
	} else {
		_, enqueueErr = h.TaskService.EnqueueTaskForMention(ctx, issue, agentID, systemComment.ID, service.OriginDerived)
	}
	if enqueueErr != nil && !errors.Is(enqueueErr, service.ErrDuplicatePendingTask) {
		slog.Warn("system comment: enqueue assignee task failed", "error", enqueueErr, "issue_id", uuidToString(issue.ID), "agent_id", uuidToString(agentID))
	}
}
