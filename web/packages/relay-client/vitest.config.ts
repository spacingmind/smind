import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: { setupFiles: ['./src/__tests__/setupRandom.ts'] },
});
