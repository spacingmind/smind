// scan.ts is the AST scanner behind guards.test.ts: it parses a source file
// with the TypeScript compiler and reports forbidden constructs by rule, so a
// comment trick, an alias or a computed property name cannot slip past it the
// way they slipped past regexes.

import ts from 'typescript';

export interface Violation {
  file: string;
  line: number;
  rule: string;
  detail: string;
}

const FORBIDDEN_NAMES = new Set([
  'console',
  'localStorage',
  'sessionStorage',
  'eval',
  'XMLHttpRequest',
  'sendBeacon',
  'postMessage',
  'BroadcastChannel',
]);
const CRYPTO_METHODS = new Set(['generateKey', 'importKey', 'exportKey', 'deriveKey', 'deriveBits', 'wrapKey', 'unwrapKey']);
/** Strings that must not appear at all: they are how a forbidden name gets built or looked up by computed access. */
const FORBIDDEN_STRINGS = new Set([...FORBIDDEN_NAMES, 'cookie', ...CRYPTO_METHODS]);
const FORBIDDEN_FORMATS = new Set(['pkcs8', 'jwk']);
const GLOBAL_OBJECTS = new Set(['globalThis', 'window', 'self', 'crypto', 'subtle']);

/** The last name of an expression: `a.b.subtle` -> 'subtle', `subtle` -> 'subtle'. */
function lastName(e: ts.Expression): string | null {
  if (ts.isIdentifier(e)) return e.text;
  if (ts.isPropertyAccessExpression(e)) return e.name.text;
  if (ts.isParenthesizedExpression(e) || ts.isNonNullExpression(e) || ts.isAsExpression(e)) return lastName(e.expression);
  return null;
}

function isGlobalish(name: string | null): boolean {
  return name !== null && (GLOBAL_OBJECTS.has(name) || /subtle$/i.test(name));
}

export function scanSource(file: string, text: string): Violation[] {
  const sf = ts.createSourceFile(file, text, ts.ScriptTarget.ES2022, true);
  const out: Violation[] = [];
  const report = (node: ts.Node, rule: string, detail: string) =>
    out.push({ file, line: sf.getLineAndCharacterOfPosition(node.getStart(sf)).line + 1, rule, detail });

  const visit = (node: ts.Node): void => {
    // (1) forbidden names, wherever they appear as an identifier or property name
    if (ts.isIdentifier(node)) {
      if (FORBIDDEN_NAMES.has(node.text)) report(node, 'forbidden-name', node.text);
      if (node.text === 'cookie') report(node, 'forbidden-name', 'cookie');
      if (node.text === 'Function' && node.parent && (ts.isCallExpression(node.parent) || ts.isNewExpression(node.parent)) && node.parent.expression === node) {
        report(node, 'forbidden-name', 'Function(');
      }
      // (3) crypto methods only as a direct `<expr>.method(...)` call
      if (CRYPTO_METHODS.has(node.text)) {
        const p = node.parent;
        const direct =
          p !== undefined &&
          ts.isPropertyAccessExpression(p) &&
          p.name === node &&
          p.parent !== undefined &&
          ts.isCallExpression(p.parent) &&
          p.parent.expression === p;
        if (!direct) {
          report(node, 'crypto-reference', `${node.text} must only be called directly`);
        } else {
          const call = (p as ts.PropertyAccessExpression).parent as ts.CallExpression;
          if (node.text === 'generateKey') {
            if (call.arguments[1]?.kind !== ts.SyntaxKind.FalseKeyword) report(node, 'generate-key-extractable', 'the second argument of generateKey must be the literal false');
          } else if (node.text === 'exportKey') {
            const a = call.arguments[0];
            if (!a || !ts.isStringLiteral(a) || a.text !== 'raw') report(node, 'export-key-format', "the first argument of exportKey must be 'raw'");
          } else {
            report(node, 'crypto-forbidden', `${node.text} is not allowed`);
          }
        }
      }
    }
    if (ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) {
      const m = node.moduleSpecifier;
      if (m && ts.isStringLiteral(m) && m.text.startsWith('node:')) report(node, 'node-module', m.text);
    }
    if (ts.isCallExpression(node) && (node.expression.kind === ts.SyntaxKind.ImportKeyword || lastName(node.expression) === 'require')) {
      const a = node.arguments[0];
      if (a && ts.isStringLiteral(a) && a.text.startsWith('node:')) report(node, 'node-module', a.text);
    }
    // (2) computed access on a global or a SubtleCrypto, and strings that name forbidden things
    if (ts.isElementAccessExpression(node) && isGlobalish(lastName(node.expression))) {
      report(node, 'computed-access', `${node.expression.getText(sf)}[...]`);
    }
    if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
      if (FORBIDDEN_STRINGS.has(node.text)) report(node, 'forbidden-string', JSON.stringify(node.text));
      if (FORBIDDEN_FORMATS.has(node.text)) report(node, 'key-format', JSON.stringify(node.text));
    }
    // a key that can be exported
    if (ts.isPropertyAssignment(node) && ts.isIdentifier(node.name) && node.name.text === 'extractable' && node.initializer.kind === ts.SyntaxKind.TrueKeyword) {
      report(node, 'extractable-true', 'extractable: true');
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return out;
}
