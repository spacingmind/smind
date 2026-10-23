# Rebuild the dev client

Prompt attachments (ADR-0038) add native modules to the mobile app:

- `expo-image-picker` (photo library)
- `expo-document-picker` (files)
- `expo-image-manipulator` (on-device JPEG downscale)
- `expo-file-system` (reading picked files as base64)

A dev client built before they were added does not contain them, so the Attach menu fails there. **Expo Go cannot run them
either**; use a dev client.

Rebuild after pulling this change:

```sh
cd mobile
eas build --profile development --platform ios      # or android
# or, local native build:
npx expo run:ios                                    # or run:android
```

Permission strings live in `app.json`: the `expo-image-picker` plugin entry sets the iOS photo-library usage text
(`photosPermission`; the camera is not requested). Changing a plugin entry also needs a rebuild, not just a JS reload.

The web build (`npm run web`, the smoke target) needs no rebuild: the pickers use `<input type="file">` there.
