import { existsSync, readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const require = createRequire(import.meta.url);
const appConfig = require('../../app.config.js') as (ctx: { config: any }) => any;
const appJson = JSON.parse(readFileSync(resolve(__dirname, '../../app.json'), 'utf8'));

describe('app.config.js', () => {
  it('keeps the static app.json fields', () => {
    const out = appConfig({ config: appJson.expo });
    expect(out.owner).toBe('spacing-mind');
    expect(out.android.package).toBe('com.spacingmind.smind');
    expect(out.ios.bundleIdentifier).toBe('com.spacingmind.smind');
    expect(out.extra.eas.projectId).toBe('7184c9e3-0a46-44b5-befd-5a29f7f47675');
  });

  it('adds googleServicesFile only when GOOGLE_SERVICES_JSON is set', () => {
    const prev = process.env.GOOGLE_SERVICES_JSON;
    try {
      delete process.env.GOOGLE_SERVICES_JSON;
      expect(appConfig({ config: appJson.expo }).android.googleServicesFile).toBeUndefined();
      process.env.GOOGLE_SERVICES_JSON = '/tmp/x/google-services.json';
      expect(appConfig({ config: appJson.expo }).android.googleServicesFile).toBe('/tmp/x/google-services.json');
    } finally {
      if (prev === undefined) delete process.env.GOOGLE_SERVICES_JSON;
      else process.env.GOOGLE_SERVICES_JSON = prev;
    }
  });

  it('never commits a google-services.json', () => {
    expect(existsSync(resolve(__dirname, '../../google-services.json'))).toBe(false);
  });

  it('app.json lists the expo-notifications plugin', () => {
    expect(appJson.expo.plugins).toContain('expo-notifications');
  });
});
