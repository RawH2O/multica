"use client";

import { useState } from "react";
import { Loader2, Plus, Trash2, Webhook } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Agent } from "@multica/core/types";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  agentHooksOptions,
  hookListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";

export function HooksTab({
  agent,
  canEdit = true,
}: {
  agent: Agent;
  canEdit?: boolean;
}) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const [adding, setAdding] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const assignedQuery = useQuery(agentHooksOptions(agent.id));
  const allQuery = useQuery(hookListOptions(wsId));
  const assigned = assignedQuery.data ?? [];
  const available = (allQuery.data ?? []).filter(
    (hook) => !assigned.some((item) => item.id === hook.id),
  );
  const refresh = () =>
    Promise.all([
      qc.invalidateQueries({ queryKey: ["agents", agent.id, "hooks"] }),
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) }),
    ]);
  const add = async (hookId: string) => {
    setBusy(hookId);
    try {
      await api.addAgentHooks(agent.id, { hook_ids: [hookId] });
      await refresh();
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Failed to add hook",
      );
    } finally {
      setBusy(null);
    }
  };
  const remove = async (hookId: string) => {
    setBusy(hookId);
    try {
      await api.removeAgentHook(agent.id, hookId);
      await refresh();
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Failed to remove hook",
      );
    } finally {
      setBusy(null);
    }
  };
  const toggle = async (hookId: string, enabled: boolean) => {
    setBusy(hookId);
    try {
      await api.setAgentHookEnabled(agent.id, hookId, enabled);
      await refresh();
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Failed to toggle hook",
      );
    } finally {
      setBusy(null);
    }
  };
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">
          Managed Codex hooks are materialized when this agent runs.
        </p>
        {canEdit && (
          <Button
            size="sm"
            variant="outline"
            disabled={available.length === 0}
            onClick={() => setAdding((value) => !value)}
          >
            <Plus className="mr-1.5 h-4 w-4" />
            Add hook
          </Button>
        )}
      </div>
      {adding && (
        <div className="rounded-lg border p-2">
          {available.length === 0 ? (
            <p className="p-2 text-sm text-muted-foreground">
              All workspace hooks are assigned.
            </p>
          ) : (
            available.map((hook) => (
              <button
                key={hook.id}
                type="button"
                className="flex w-full items-center gap-2 rounded px-2 py-2 text-left text-sm hover:bg-muted"
                onClick={() => add(hook.id)}
                disabled={busy === hook.id}
              >
                <Webhook className="h-4 w-4 text-muted-foreground" />
                {hook.name}
                <span className="ml-auto text-xs text-muted-foreground">
                  {hook.events.join(", ")}
                </span>
              </button>
            ))
          )}
        </div>
      )}
      {assigned.length === 0 ? (
        <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">
          No hooks assigned.
        </div>
      ) : (
        <div className="divide-y rounded-lg border">
          {assigned.map((hook) => (
            <div key={hook.id} className="flex items-center gap-3 p-3">
              <Webhook className="h-4 w-4 text-muted-foreground" />
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm font-medium">{hook.name}</div>
                <div className="text-xs text-muted-foreground">
                  {hook.events.join(", ")}
                </div>
              </div>
              {canEdit && (
                <>
                  <Switch
                    checked={hook.enabled !== false}
                    disabled={busy === hook.id}
                    onCheckedChange={(value) => toggle(hook.id, value)}
                    aria-label={`Toggle ${hook.name}`}
                  />
                  {busy === hook.id ? (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  ) : (
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => remove(hook.id)}
                      aria-label={`Remove ${hook.name}`}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  )}
                </>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
