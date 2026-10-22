"use client";

import { useEffect, useState, type ChangeEvent } from "react";
import { Pencil, Plus, Trash2, Webhook } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import type { Hook, HookSummary } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  hookListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { CollectionPageHeader } from "../layout/collection-page";

type FormState = {
  name: string;
  description: string;
  command: string;
  events: string;
  matcher: string;
};

const EMPTY_FORM: FormState = {
  name: "",
  description: "",
  command: "",
  events: "PreToolUse",
  matcher: "",
};

function formFromHook(hook: Hook | null): FormState {
  if (!hook) return EMPTY_FORM;
  return {
    name: hook.name,
    description: hook.description,
    command: hook.command,
    events: hook.events.join(", "),
    matcher: hook.matcher,
  };
}

function HookForm({
  initial,
  saving,
  onCancel,
  onSave,
}: {
  initial: FormState;
  saving: boolean;
  onCancel: () => void;
  onSave: (value: FormState) => void;
}) {
  const [form, setForm] = useState(initial);
  useEffect(() => setForm(initial), [initial]);
  const set =
    (key: keyof FormState) =>
    (event: ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) =>
      setForm((current) => ({ ...current, [key]: event.target.value }));
  return (
    <div className="space-y-4 p-1">
      <div className="space-y-1.5">
        <Label htmlFor="hook-name">Name</Label>
        <Input
          id="hook-name"
          value={form.name}
          onChange={set("name")}
          placeholder="mention-required"
        />
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="hook-command">Command</Label>
        <Input
          id="hook-command"
          value={form.command}
          onChange={set("command")}
          placeholder="my-hook --provider codex"
        />
        <p className="text-caption text-muted-foreground">
          The command must be available on the daemon host PATH.
        </p>
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="hook-events">Events</Label>
        <Input
          id="hook-events"
          value={form.events}
          onChange={set("events")}
          placeholder="PreToolUse, PostToolUse"
        />
        <p className="text-caption text-muted-foreground">
          Comma-separated Codex events. V1 supports Codex only.
        </p>
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="hook-matcher">Matcher (optional)</Label>
        <Input
          id="hook-matcher"
          value={form.matcher}
          onChange={set("matcher")}
          placeholder="Bash"
        />
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="hook-description">Description</Label>
        <Textarea
          id="hook-description"
          value={form.description}
          onChange={set("description")}
          rows={3}
        />
      </div>
      <div className="flex justify-end gap-2">
        <Button variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button disabled={saving} onClick={() => onSave(form)}>
          {saving ? "Saving…" : "Save"}
        </Button>
      </div>
    </div>
  );
}

export function HooksPage() {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { data: hooks = [], isLoading } = useQuery(hookListOptions(wsId));
  const [editing, setEditing] = useState<Hook | null | undefined>(undefined);
  const [saving, setSaving] = useState(false);

  const save = async (form: FormState) => {
    if (!form.name.trim() || !form.command.trim() || !form.events.trim())
      return;
    setSaving(true);
    try {
      const body = {
        name: form.name.trim(),
        description: form.description.trim(),
        command: form.command.trim(),
        events: form.events
          .split(",")
          .map((value) => value.trim())
          .filter(Boolean),
        providers: ["codex"],
        matcher: form.matcher.trim(),
      };
      if (editing) await api.updateHook(editing.id, body);
      else await api.createHook(body);
      await qc.invalidateQueries({ queryKey: workspaceKeys.hooks(wsId) });
      setEditing(undefined);
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Failed to save hook",
      );
    } finally {
      setSaving(false);
    }
  };

  const remove = async (hook: HookSummary) => {
    if (!window.confirm(`Delete hook ${hook.name}?`)) return;
    try {
      await api.deleteHook(hook.id);
      await qc.invalidateQueries({ queryKey: workspaceKeys.hooks(wsId) });
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Failed to delete hook",
      );
    }
  };

  return (
    <div className="flex h-full min-h-0 flex-col">
      <CollectionPageHeader
        icon={Webhook}
        title="Hooks"
        count={hooks.length}
        description="Manage Codex hooks and bind them to agents."
        actions={
          <Button size="sm" onClick={() => setEditing(null)}>
            <Plus className="mr-1.5 h-4 w-4" />
            New hook
          </Button>
        }
      />
      <div className="min-h-0 flex-1 overflow-y-auto p-4 sm:p-6">
        {isLoading ? (
          <p className="text-sm text-muted-foreground">Loading hooks…</p>
        ) : hooks.length === 0 ? (
          <div className="rounded-lg border border-dashed p-10 text-center text-sm text-muted-foreground">
            No managed hooks yet.
          </div>
        ) : (
          <div className="divide-y rounded-lg border">
            {hooks.map((hook) => (
              <div key={hook.id} className="flex items-start gap-3 p-4">
                <Webhook className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
                <div className="min-w-0 flex-1">
                  <div className="font-medium">{hook.name}</div>
                  <div className="mt-1 break-all font-mono text-xs text-muted-foreground">
                    {hook.command}
                  </div>
                  <div className="mt-2 flex flex-wrap gap-1.5 text-xs text-muted-foreground">
                    <span>Codex</span>
                    <span>·</span>
                    <span>{hook.events.join(", ")}</span>
                    {hook.matcher && (
                      <>
                        <span>·</span>
                        <span>matcher: {hook.matcher}</span>
                      </>
                    )}
                  </div>
                  {hook.description && (
                    <p className="mt-1 text-sm text-muted-foreground">
                      {hook.description}
                    </p>
                  )}
                </div>
                <div className="flex shrink-0 gap-1">
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={`Edit ${hook.name}`}
                    onClick={async () => setEditing(await api.getHook(hook.id))}
                  >
                    <Pencil className="h-4 w-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={`Delete ${hook.name}`}
                    onClick={() => remove(hook)}
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
      <Dialog
        open={editing !== undefined}
        onOpenChange={(open) => !open && setEditing(undefined)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editing ? "Edit hook" : "Create hook"}</DialogTitle>
          </DialogHeader>
          <HookForm
            initial={formFromHook(editing ?? null)}
            saving={saving}
            onCancel={() => setEditing(undefined)}
            onSave={save}
          />
        </DialogContent>
      </Dialog>
    </div>
  );
}
