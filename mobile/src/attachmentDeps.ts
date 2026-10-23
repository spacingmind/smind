// attachmentDeps.ts: the device half of the attachment pipeline -- the ONLY
// file importing the native pickers, the image manipulator and the file
// reader (attachments.ts takes them as injected deps so it stays pure).
// Pickers resolve to Picks or throw; the caller turns a throw into an
// inline notice (never a crash).

import { Platform } from 'react-native';
import * as DocumentPicker from 'expo-document-picker';
import { readAsStringAsync, EncodingType } from 'expo-file-system/legacy';
import { ImageManipulator, SaveFormat } from 'expo-image-manipulator';
import * as ImagePicker from 'expo-image-picker';
import { ImageDeps, Pick } from './attachments';

/** base64 of the file at `uri`. expo-file-system does not run on web, where picker uris are blob/object URLs. */
async function readBase64(uri: string): Promise<string> {
  if (Platform.OS === 'web') {
    const blob = await (await fetch(uri)).blob();
    return await new Promise<string>((resolve, reject) => {
      const reader = new FileReader();
      reader.onerror = () => reject(reader.error ?? new Error('could not read the file'));
      reader.onload = () => {
        const url = String(reader.result);
        resolve(url.slice(url.indexOf(',') + 1));
      };
      reader.readAsDataURL(blob);
    });
  }
  return readAsStringAsync(uri, { encoding: EncodingType.Base64 });
}

/** Device deps for attachments.addPick; `dims` remembers picker-reported sizes so a resize does not need a decode first. */
export function createImageDeps(dims: Map<string, { width: number; height: number }>): ImageDeps {
  return {
    readBase64,
    async manipulate(uri, { maxLongEdge, quality }) {
      let size = dims.get(uri);
      if (!size) {
        const probe = await ImageManipulator.manipulate(uri).renderAsync();
        size = { width: probe.width, height: probe.height };
        probe.release();
      }
      const ctx = ImageManipulator.manipulate(uri);
      try {
        if (Math.max(size.width, size.height) > maxLongEdge) {
          ctx.resize(size.width >= size.height ? { width: maxLongEdge } : { height: maxLongEdge });
        }
        const ref = await ctx.renderAsync();
        try {
          const out = await ref.saveAsync({ format: SaveFormat.JPEG, compress: quality, base64: true });
          return { uri: out.uri, width: out.width, height: out.height, base64: out.base64 ?? '' };
        } finally {
          ref.release();
        }
      } finally {
        ctx.release();
      }
    },
  };
}

/** The photo-library picker; `limit` is how many more attachments fit. Empty when cancelled. */
export async function pickPhotos(limit: number): Promise<Pick[]> {
  const res = await ImagePicker.launchImageLibraryAsync({
    mediaTypes: ['images'],
    allowsMultipleSelection: true,
    selectionLimit: Math.max(1, limit),
    quality: 1,
    exif: false,
  });
  if (res.canceled) return [];
  return res.assets.map((a) => ({
    kind: 'image' as const,
    uri: a.uri,
    name: a.fileName ?? 'image',
    mimeType: a.mimeType,
    width: a.width,
    height: a.height,
  }));
}

/** The document picker. Empty when cancelled. */
export async function pickFiles(): Promise<Pick[]> {
  const res = await DocumentPicker.getDocumentAsync({ multiple: true, copyToCacheDirectory: true });
  if (res.canceled) return [];
  return res.assets.map((a) => ({ kind: 'file' as const, uri: a.uri, name: a.name, mimeType: a.mimeType, size: a.size }));
}
