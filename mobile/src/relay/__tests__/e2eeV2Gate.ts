// Temporary gate (ADR-0035): the Go daemon speaks E2EE v2 and this client does
// not yet, so every suite that runs a real Go harness is skipped until the
// mobile E2EE v2 change lands and deletes this file with its usages. Vector and
// unit tests are never gated.
export const E2EE_V2_PENDING = 'E2EE v2 pending in this client (ADR-0035)';
