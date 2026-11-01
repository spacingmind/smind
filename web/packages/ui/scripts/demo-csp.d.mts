export function inlineScriptHashes(html: string): string[];
export function cspDirectives(scriptHashes: string[], opts: { frameAncestors: boolean }): [string, string][];
export function cspString(scriptHashes: string[], opts: { frameAncestors: boolean }): string;
export function cspMetaTag(scriptHashes: string[]): string;
export function headersFile(scriptHashes: string[]): string;
export function readMetaCsp(html: string): string | null;
