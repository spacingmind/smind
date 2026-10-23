// attachmentTray.ts: the pure view helpers behind the composer's attach
// button and pending-attachment tray (ADR-0038 mobile). No react-native.

import { AttachmentsInfo } from './api';
import { capabilities, humanSize, PendingAttachment } from './attachments';

export interface TrayPill {
  id: string;
  label: string;
  sizeLabel: string;
  kind: 'image' | 'file';
  thumbUri?: string;
}

export function trayPills(pending: PendingAttachment[]): TrayPill[] {
  return pending.map((a) => ({
    id: a.id,
    label: a.name,
    sizeLabel: humanSize(a.size),
    kind: a.kind,
    ...(a.kind === 'image' && a.uri ? { thumbUri: a.uri } : {}),
  }));
}

export interface AttachMenuItem {
  key: 'photo' | 'file';
  label: string;
}

/** The attach menu from daemon.info's attachments block; an empty list hides the button. */
export function attachMenuItems(info?: AttachmentsInfo): AttachMenuItem[] {
  const caps = capabilities(info);
  const items: AttachMenuItem[] = [];
  if (caps.image) items.push({ key: 'photo', label: 'Photo library' });
  if (caps.file) items.push({ key: 'file', label: 'Files' });
  return items;
}
