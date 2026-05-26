// Side-effect import: sets up global polyfills
if (typeof globalThis.structuredClone === 'undefined') {
  globalThis.structuredClone = (obj: unknown) => JSON.parse(JSON.stringify(obj));
}
