package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/vcs"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ── Response mappers ────────────────────────────────────────────────────────

// vcsPullRequestToResponse maps a stored VCS PR onto the shared PR response
// shape for single-PR webhook broadcasts (no aggregated check counts; the
// frontend re-queries the issue's PR list for fresh counts).
func vcsPullRequestToResponse(p db.VcsPullRequest) GitHubPullRequestResponse {
	return GitHubPullRequestResponse{
		ID:               uuidToString(p.ID),
		Provider:         p.Provider,
		WorkspaceID:      uuidToString(p.WorkspaceID),
		RepoOwner:        p.RepoOwner,
		RepoName:         p.RepoName,
		Number:           p.PrNumber,
		Title:            p.Title,
		State:            p.State,
		HtmlURL:          p.HtmlUrl,
		Branch:           textToPtr(p.Branch),
		AuthorLogin:      textToPtr(p.AuthorLogin),
		AuthorAvatarURL:  textToPtr(p.AuthorAvatarUrl),
		MergedAt:         timestampToPtr(p.MergedAt),
		ClosedAt:         timestampToPtr(p.ClosedAt),
		PRCreatedAt:      timestampToString(p.PrCreatedAt),
		PRUpdatedAt:      timestampToString(p.PrUpdatedAt),
		MergeableState:   nil,
		ChecksConclusion: nil,
		Additions:        p.Additions,
		Deletions:        p.Deletions,
		ChangedFiles:     p.ChangedFiles,
	}
}

// vcsPullRequestRowToResponse maps an issue's PR-list row, which carries the
// aggregated commit-status counts, onto the shared response shape.
func vcsPullRequestRowToResponse(p db.ListVCSPullRequestsByIssueRow) GitHubPullRequestResponse {
	return GitHubPullRequestResponse{
		ID:               uuidToString(p.ID),
		Provider:         p.Provider,
		WorkspaceID:      uuidToString(p.WorkspaceID),
		RepoOwner:        p.RepoOwner,
		RepoName:         p.RepoName,
		Number:           p.PrNumber,
		Title:            p.Title,
		State:            p.State,
		HtmlURL:          p.HtmlUrl,
		Branch:           textToPtr(p.Branch),
		AuthorLogin:      textToPtr(p.AuthorLogin),
		AuthorAvatarURL:  textToPtr(p.AuthorAvatarUrl),
		MergedAt:         timestampToPtr(p.MergedAt),
		ClosedAt:         timestampToPtr(p.ClosedAt),
		PRCreatedAt:      timestampToString(p.PrCreatedAt),
		PRUpdatedAt:      timestampToString(p.PrUpdatedAt),
		MergeableState:   nil,
		ChecksConclusion: aggregateChecksConclusion(p.ChecksFailed, p.ChecksPassed, p.ChecksPending, p.ChecksTotal),
		ChecksTotal:      p.ChecksTotal,
		ChecksPassed:     p.ChecksPassed,
		ChecksFailed:     p.ChecksFailed,
		ChecksPending:    p.ChecksPending,
		ChecksRunning:    p.ChecksPending,
		FailedCheckNames: []string{},
		Additions:        p.Additions,
		Deletions:        p.Deletions,
		ChangedFiles:     p.ChangedFiles,
	}
}

// ── Webhook ─────────────────────────────────────────────────────────────────

// HandleVCSWebhook (POST /api/webhooks/vcs/{connectionId}) authenticates and
// mirrors webhooks from any token-based Git provider. The connection id in the path
// selects the workspace, the provider, and the decryption secret; the provider
// adapter handles the provider-specific signature scheme, event header, and
// payload shape, returning normalized events to the shared mirror logic below.
func (h *Handler) HandleVCSWebhook(w http.ResponseWriter, r *http.Request) {
	// Where the integration is off (the managed cloud) the endpoint behaves as
	// if it does not exist — a bare 404 that reveals nothing about config, the
	// same response a genuinely unknown connection id gets below.
	if !h.isVCSAvailable() {
		writeError(w, http.StatusNotFound, "unknown connection")
		return
	}
	if !h.isVCSConfigured() {
		writeError(w, http.StatusServiceUnavailable, "vcs webhooks not configured")
		return
	}
	connUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "connectionId"), "connection id")
	if !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20)) // 10 MiB cap
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body failed")
		return
	}

	conn, err := h.Queries.GetVCSConnectionByID(r.Context(), connUUID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("vcs: lookup connection failed", "err", err)
		}
		writeError(w, http.StatusNotFound, "unknown connection")
		return
	}
	provider, ok := vcs.For(conn.Provider)
	if !ok {
		slog.Error("vcs: connection has unknown provider", "provider", conn.Provider)
		writeError(w, http.StatusInternalServerError, "unknown provider")
		return
	}

	secret, err := h.openVCSSecret(conn.WebhookSecretEncrypted)
	if err != nil {
		slog.Error("vcs: decrypt webhook secret failed", "err", err)
		writeError(w, http.StatusInternalServerError, "secret error")
		return
	}
	if !provider.VerifySignature(secret, r.Header, body) {
		writeError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	switch provider.EventKind(r.Header) {
	case vcs.EventPullRequest:
		if pr, err := provider.ParsePullRequest(body); err != nil {
			slog.Warn("vcs: bad pull_request payload", "provider", conn.Provider, "err", err)
		} else {
			h.mirrorVCSPullRequest(r.Context(), conn, pr)
		}
	case vcs.EventCIStatus:
		if st, err := provider.ParseCIStatus(body); err != nil {
			slog.Warn("vcs: bad status payload", "provider", conn.Provider, "err", err)
		} else {
			h.mirrorVCSCIStatus(r.Context(), conn, st)
		}
	default:
		// Acknowledge unmodelled events so the provider doesn't flag the hook.
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) mirrorVCSPullRequest(ctx context.Context, conn db.VcsConnection, ev vcs.PullRequestEvent) {
	if ev.RepoOwner == "" || ev.RepoName == "" || ev.Number == 0 {
		slog.Warn("vcs: pull_request missing repo identity", "provider", conn.Provider)
		return
	}

	pr, err := h.Queries.UpsertVCSPullRequest(ctx, db.UpsertVCSPullRequestParams{
		WorkspaceID:     conn.WorkspaceID,
		ConnectionID:    conn.ID,
		Provider:        conn.Provider,
		RepoOwner:       ev.RepoOwner,
		RepoName:        ev.RepoName,
		PrNumber:        ev.Number,
		Title:           ev.Title,
		State:           ev.State,
		HtmlUrl:         ev.HTMLURL,
		Branch:          ptrToText(strPtrOrNil(ev.Branch)),
		AuthorLogin:     ptrToText(strPtrOrNil(ev.AuthorLogin)),
		AuthorAvatarUrl: ptrToText(strPtrOrNil(ev.AuthorAvatarURL)),
		MergedAt:        parseGHTime(ev.MergedAt),
		ClosedAt:        parseGHTime(ev.ClosedAt),
		PrCreatedAt:     parseGHTimeRequired(ev.CreatedAt),
		PrUpdatedAt:     parseGHTimeRequired(ev.UpdatedAt),
		Additions:       ev.Additions,
		Deletions:       ev.Deletions,
		ChangedFiles:    ev.ChangedFiles,
		HeadSha:         ev.HeadSHA,
	})
	if err != nil {
		slog.Warn("vcs: upsert pr failed", "err", err)
		return
	}

	// Out-of-order guard for the link metadata. UpsertVCSPullRequest keeps the
	// newer persisted row on a stale redelivery, so `pr` may reflect a newer
	// event than this `ev`. Everything the link write derives below —
	// close_intent, reference_only, preserveCloseIntent — comes from `ev`, so
	// rewriting the link from a stale event would corrupt what the newer event
	// already set (e.g. a redelivered older "opened" event flipping a merged
	// PR's link back to reference_only, blocking auto-advance). If the persisted
	// row is strictly newer than this event, the newer event already linked and
	// published — stop here. (An event with no usable timestamp falls back to
	// now(), which is never strictly after the stored value, so it proceeds.)
	evUpdatedAt := parseGHTimeRequired(ev.UpdatedAt)
	if pr.PrUpdatedAt.Valid && evUpdatedAt.Valid && pr.PrUpdatedAt.Time.After(evUpdatedAt.Time) {
		return
	}

	workspaceID := uuidToString(conn.WorkspaceID)
	resp := vcsPullRequestToResponse(pr)

	// Auto-link to issues by identifiers in title/body/branch. Connecting a
	// a provider is the opt-in, so there is no separate per-workspace flag. The
	// issue-side machinery is shared with GitHub.
	linkedIssueIDs := make([]string, 0)
	idents := extractIdentifiers(ev.Title, ev.Body, ev.Branch)
	closingIdents := map[string]struct{}{}
	for _, c := range extractClosingIdentifiers(ev.Title, ev.Body) {
		closingIdents[c] = struct{}{}
	}
	// qualifyingIdents genuinely tie this PR to an issue: a title prefix, a
	// branch-name reference, or a body closing keyword. An identifier matched
	// ONLY by a bare body mention is reference_only — it links (so the PR shows
	// in history) but is hidden from the issue PR list and excluded from the
	// close aggregate, so a drive-by "Related MUL-1" neither looks like a
	// working PR nor blocks a genuine Closes sibling from advancing the issue.
	// Mirrors the GitHub path (MUL-3739); branch is deliberately excluded from
	// the closing-keyword scan there and here.
	qualifyingIdents := map[string]struct{}{}
	for _, id := range extractIdentifiers(ev.Title, ev.Branch) {
		qualifyingIdents[id] = struct{}{}
	}
	for c := range closingIdents {
		qualifyingIdents[c] = struct{}{}
	}
	// Freeze close_intent once the terminal merge/close event has arrived.
	preserveCloseIntent := !ev.Terminal() && (ev.State == "merged" || ev.State == "closed")
	prefix := h.getIssuePrefix(ctx, conn.WorkspaceID)
	reevalIssues := make([]db.Issue, 0, len(idents))
	for _, id := range idents {
		issue, ok := h.lookupIssueByIdentifier(ctx, conn.WorkspaceID, prefix, id)
		if !ok {
			continue
		}
		_, declared := closingIdents[id]
		closeIntent := declared && !preserveCloseIntent
		_, qualifies := qualifyingIdents[id]
		referenceOnly := !qualifies
		if err := h.Queries.LinkIssueToVCSPullRequest(ctx, db.LinkIssueToVCSPullRequestParams{
			IssueID:             issue.ID,
			PullRequestID:       pr.ID,
			CloseIntent:         closeIntent,
			ReferenceOnly:       referenceOnly,
			PreserveCloseIntent: preserveCloseIntent,
			LinkedByType:        strToText("system"),
			LinkedByID:          pgtype.UUID{},
		}); err != nil {
			slog.Warn("vcs: link failed", "err", err)
			continue
		}
		linkedIssueIDs = append(linkedIssueIDs, uuidToString(issue.ID))
		reevalIssues = append(reevalIssues, issue)
	}

	if ev.State == "merged" || ev.State == "closed" {
		for _, issue := range reevalIssues {
			// A custom terminal status counts as terminal here. (MUL-6243)
			if s := issuestatus.Effective(ctx, h.Queries, issue.WorkspaceID, issue.Status); s == "done" || s == "cancelled" {
				continue
			}
			counts, err := h.Queries.GetIssueCombinedPullRequestCloseAggregate(ctx, issue.ID)
			if err != nil {
				slog.Warn("vcs: count linked pr states failed", "err", err, "issue_id", uuidToString(issue.ID))
				continue
			}
			if counts.OpenCount == 0 && counts.MergedWithCloseIntentCount > 0 {
				h.advanceIssueToDone(ctx, issue, workspaceID)
			}
		}
	}

	// A GitLab pipeline webhook can arrive before the MR webhook that moves
	// this row to the new head SHA. Replay a failure already stored for the
	// current head after the MR link is in place, so webhook ordering cannot
	// lose the agent reminder.
	if conn.Provider == string(vcs.KindGitLab) && pr.HeadSha != "" {
		h.replayVCSCIFailureForHead(ctx, conn, pr.HeadSha)
	}

	h.publish(protocol.EventPullRequestUpdated, workspaceID, "system", "", map[string]any{
		"pull_request":     resp,
		"linked_issue_ids": linkedIssueIDs,
	})
}

func (h *Handler) mirrorVCSCIStatus(ctx context.Context, conn db.VcsConnection, ev vcs.CIStatusEvent) {
	if ev.SHA == "" || ev.State == "" {
		return
	}
	// Use the provider's own event timestamp so UpsertVCSCommitStatus's
	// monotonic guard has something real to compare — writing time.Now() here
	// made the guard always true, so an out-of-order redelivery could regress a
	// status. Falls back to now() only when the payload carried no timestamp.
	statusParams := db.UpsertVCSCommitStatusParams{
		ConnectionID: conn.ID,
		Sha:          ev.SHA,
		Context:      ev.Context,
		State:        ev.State,
		TargetUrl:    ptrToText(strPtrOrNil(ev.TargetURL)),
		Description:  ptrToText(strPtrOrNil(ev.Description)),
		UpdatedAt:    parseGHTimeRequired(ev.UpdatedAt),
	}
	if ev.State == "failed" && conn.Provider == string(vcs.KindGitLab) {
		rows, err := h.Queries.UpsertVCSCommitStatusOnFailure(ctx, db.UpsertVCSCommitStatusOnFailureParams{
			ConnectionID: conn.ID,
			Sha:          ev.SHA,
			Context:      ev.Context,
			State:        ev.State,
			UpdatedAt:    statusParams.UpdatedAt,
			TargetUrl:    statusParams.TargetUrl,
			Description:  statusParams.Description,
		})
		if err != nil {
			slog.Warn("vcs: upsert failed commit status failed", "err", err)
			return
		}
		if rows > 0 {
			h.replayVCSCIFailureForHead(ctx, conn, ev.SHA)
		}
	} else if err := h.Queries.UpsertVCSCommitStatus(ctx, statusParams); err != nil {
		slog.Warn("vcs: upsert commit status failed", "err", err)
		return
	}

	issueIDs, err := h.Queries.ListIssueIDsForVCSPRHead(ctx, db.ListIssueIDsForVCSPRHeadParams{
		ConnectionID: conn.ID,
		HeadSha:      ev.SHA,
	})
	if err != nil {
		slog.Warn("vcs: lookup issues for status failed", "err", err)
		return
	}
	workspaceID := uuidToString(conn.WorkspaceID)
	for _, issueID := range issueIDs {
		h.publish(protocol.EventPullRequestUpdated, workspaceID, "system", "", map[string]any{
			"issue_id": uuidToString(issueID),
		})
	}
}

// replayVCSCIFailureForHead is the shared notification path for both webhook
// orderings. The CI path calls it after accepting a new failed state; the MR
// path calls it after the current head SHA and issue link are persisted.
func (h *Handler) replayVCSCIFailureForHead(ctx context.Context, conn db.VcsConnection, headSHA string) {
	if conn.Provider != string(vcs.KindGitLab) || headSHA == "" {
		return
	}
	status, err := h.Queries.GetFailedVCSCommitStatus(ctx, db.GetFailedVCSCommitStatusParams{
		ConnectionID: conn.ID,
		Sha:          headSHA,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("vcs: lookup failed status for agent reminder failed", "err", err)
		}
		return
	}
	issueIDs, err := h.Queries.ListActionableIssueIDsForVCSPRHead(ctx, db.ListActionableIssueIDsForVCSPRHeadParams{
		ConnectionID: conn.ID,
		HeadSha:      headSHA,
	})
	if err != nil {
		slog.Warn("vcs: lookup issues for failed status reminder failed", "err", err)
		return
	}
	for _, issueID := range issueIDs {
		h.notifyVCSCIFailureIssue(ctx, conn, issueID, status)
	}
}

// notifyVCSCIFailureIssue writes one durable system comment and explicitly
// wakes the issue's agent/squad assignee. It locks the issue while checking a
// deterministic marker, because the CI and MR replay paths can race with one
// another. The generic comment listeners intentionally ignore system comments;
// publishSystemIssueComment is therefore required after the transaction.
func (h *Handler) notifyVCSCIFailureIssue(ctx context.Context, conn db.VcsConnection, issueID pgtype.UUID, status db.VcsCommitStatus) {
	if h.TxStarter == nil {
		slog.Warn("vcs: cannot notify agent about failed CI without transaction starter", "issue_id", uuidToString(issueID))
		return
	}

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		slog.Warn("vcs: begin failed CI reminder transaction failed", "err", err, "issue_id", uuidToString(issueID))
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	qtx := h.Queries.WithTx(tx)
	issue, err := qtx.LockIssueForVCSFailureNotification(ctx, db.LockIssueForVCSFailureNotificationParams{
		ID:          issueID,
		WorkspaceID: conn.WorkspaceID,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("vcs: lock issue for failed CI reminder failed", "err", err, "issue_id", uuidToString(issueID))
		}
		return
	}
	if !eligibleForVCSFailureReminder(ctx, qtx, issue) {
		return
	}

	marker := vcsCIFailureMarker(conn.ID, status)
	seen, err := qtx.HasVCSFailureNotificationComment(ctx, db.HasVCSFailureNotificationCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		Marker:      marker,
	})
	if err != nil {
		slog.Warn("vcs: check duplicate failed CI reminder failed", "err", err, "issue_id", uuidToString(issue.ID))
		return
	}
	if seen {
		return
	}

	content := vcsCIFailureCommentContent(h.buildParentAssigneeMention(ctx, issue), marker, status)
	created, err := qtx.CreateComment(ctx, db.CreateCommentParams{
		ID:          dbid.NewV7(),
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{Valid: true},
		Content:     content,
		Type:        "system",
		ParentID:    pgtype.UUID{Valid: false},
	})
	if err != nil {
		slog.Warn("vcs: create failed CI reminder comment failed", "err", err, "issue_id", uuidToString(issue.ID))
		return
	}
	if err := tx.Commit(ctx); err != nil {
		slog.Warn("vcs: commit failed CI reminder failed", "err", err, "issue_id", uuidToString(issue.ID))
		return
	}
	committed = true
	h.publishSystemIssueComment(ctx, issue, created.Comment(), created.IssueRevision)
}

func eligibleForVCSFailureReminder(ctx context.Context, q issuestatus.Querier, issue db.Issue) bool {
	status := issuestatus.Effective(ctx, q, issue.WorkspaceID, issue.Status)
	if status == "done" || status == "cancelled" || status == "backlog" {
		return false
	}
	if !issue.AssigneeType.Valid || !issue.AssigneeID.Valid {
		return false
	}
	return issue.AssigneeType.String == "agent" || issue.AssigneeType.String == "squad"
}

func vcsCIFailureMarker(connectionID pgtype.UUID, status db.VcsCommitStatus) string {
	updatedAt := ""
	if status.UpdatedAt.Valid {
		updatedAt = status.UpdatedAt.Time.UTC().Format(time.RFC3339Nano)
	}
	return fmt.Sprintf("<!-- multica:vcs-ci-failure:%s:%s:%s:%s -->",
		uuidToString(connectionID), status.Sha, status.Context, updatedAt)
}

func vcsCIFailureCommentContent(mention, marker string, status db.VcsCommitStatus) string {
	sha := sanitizeVCSFailureCommentText(status.Sha)
	contextName := sanitizeVCSFailureCommentText(status.Context)
	content := fmt.Sprintf("%s%s\nCI failed for commit `%s` (%s).", marker, mention, sha, contextName)
	if status.TargetUrl.Valid && strings.TrimSpace(status.TargetUrl.String) != "" {
		content += fmt.Sprintf(" Pipeline: %s.", sanitizeVCSFailureCommentText(status.TargetUrl.String))
	}
	if status.Description.Valid && strings.TrimSpace(status.Description.String) != "" {
		content += fmt.Sprintf(" %s", sanitizeVCSFailureCommentText(status.Description.String))
	}
	return content + " Please inspect the failure, fix the MR, and push a new commit."
}

func sanitizeVCSFailureCommentText(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "`", "'")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}
