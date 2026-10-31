import { useState, type JSX } from "react";

import { SettingsGroup, SettingsRow } from "@/components/settings/settings-layout";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { FormError } from "@/components/ui/form-error";
import { Input } from "@/components/ui/input";
import { useProviderSecret } from "@/hooks/use-provider-recipes";
import type { ProviderInfo, ProviderSecretInfo } from "@/lib/types";

/**
 * Settings -> Providers: the stored keys of a custom instance. Only the daemon's fingerprint is ever shown;
 * a new key is typed into a password field that lives only while the dialog is open.
 */
export function ProviderSecrets({ info, hasClient }: { info: ProviderInfo; hasClient: boolean }): JSX.Element | null {
  if (!info.secrets?.length) return null;
  return (
    <SettingsGroup
      testId={`provider-secrets-${info.id}`}
      title="Keys"
      description="Stored by the daemon. A key is never shown again."
    >
      {info.secrets.map((s) => (
        <SecretRow key={s.name} instanceId={info.id} secret={s} hasClient={hasClient} />
      ))}
    </SettingsGroup>
  );
}

function SecretRow({ instanceId, secret, hasClient }: { instanceId: string; secret: ProviderSecretInfo; hasClient: boolean }): JSX.Element {
  const [dialog, setDialog] = useState<"replace" | "clear" | null>(null);
  const { set, clear, pending, error, clearError } = useProviderSecret();
  const close = (): void => {
    setDialog(null);
    clearError();
  };
  const open = (which: "replace" | "clear"): void => {
    clearError();
    setDialog(which);
  };
  return (
    <SettingsRow
      testId={`provider-secret-${instanceId}-${secret.name}`}
      label={<span className="font-mono">{secret.env}</span>}
      description={secret.set ? (secret.fingerprint ? `Set · ••••${secret.fingerprint}` : "Set") : "Not set"}
      control={
        <div className="flex items-center gap-2">
          <Button
            type="button"
            variant="outline"
            size="xs"
            disabled={!hasClient}
            data-testid={`provider-secret-replace-${secret.name}`}
            onClick={() => open("replace")}
          >
            {secret.set ? "Replace key" : "Set key"}
          </Button>
          {secret.set ? (
            <Button
              type="button"
              variant="outline"
              size="xs"
              disabled={!hasClient}
              data-testid={`provider-secret-clear-${secret.name}`}
              onClick={() => open("clear")}
            >
              Clear key
            </Button>
          ) : null}
          {dialog === "replace" ? (
            <ReplaceKeyDialog
              env={secret.env}
              isSet={secret.set}
              pending={pending}
              error={error}
              onClose={close}
              onSave={(value) => set({ instanceId, name: secret.name, value }, close)}
            />
          ) : null}
          <ConfirmDialog
            open={dialog === "clear"}
            onOpenChange={(next) => {
              if (!next) close();
            }}
            title="Clear key"
            description="Runs on this provider will fail until a new key is set."
            confirmLabel="Clear key"
            pending={pending}
            error={error}
            testId={`provider-secret-clear-dialog-${secret.name}`}
            onConfirm={() => clear({ instanceId, name: secret.name }, close)}
          />
        </div>
      }
    />
  );
}

/** Mounted only while open, so the typed value is dropped on any close (Cancel, Esc, overlay, success). */
function ReplaceKeyDialog({
  env,
  isSet,
  pending,
  error,
  onClose,
  onSave,
}: {
  env: string;
  isSet: boolean;
  pending: boolean;
  error: string | null;
  onClose: () => void;
  onSave: (value: string) => void;
}): JSX.Element {
  const [value, setValue] = useState("");
  const submit = (): void => {
    const typed = value;
    if (!typed) return;
    setValue("");
    onSave(typed);
  };
  return (
    <Dialog
      open
      onOpenChange={(next) => {
        if (!next && !pending) onClose();
      }}
    >
      <DialogContent data-testid="provider-secret-dialog">
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <DialogHeader className="text-left">
            <DialogTitle>{isSet ? "Replace key" : "Set key"}</DialogTitle>
            <DialogDescription>{`The new value of ${env}. The daemon stores it and never shows it again.`}</DialogDescription>
          </DialogHeader>
          <Input
            type="password"
            autoComplete="off"
            spellCheck={false}
            aria-label="New key"
            data-testid="provider-secret-input"
            value={value}
            onChange={(e) => setValue(e.target.value)}
          />
          <FormError message={error} testId="provider-secret-error" />
          <DialogFooter>
            <Button type="button" variant="outline" disabled={pending} onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={!value || pending} data-testid="provider-secret-save">
              {pending ? "Saving…" : "Save"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
