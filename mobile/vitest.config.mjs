import { defineConfig } from 'vitest/config';

// Expo SDK 58's `expo` package.json has an `exports` map without `./tsconfig.base`, so Vite's
// (exports-aware) tsconfig loader cannot follow `extends: "expo/tsconfig.base"` the way `tsc` does.
// Vite is pointed at a tsconfig that extends the same file by path; `tsc`/Metro keep using tsconfig.json.
export default defineConfig({ tsconfig: './tsconfig.vitest.json' });
