// Resolve hooks for module.register (nodes without module.registerHooks).
const STUBS = { herdr: "./herdr.mjs", "gpui-kit": "./gpui-kit.mjs", "gpui-base": "./gpui-base.mjs" };
export function resolveStub(spec) {
  return Object.hasOwn(STUBS, spec) ? new URL(STUBS[spec], import.meta.url).href : null;
}
export async function resolve(spec, ctx, next) {
  const url = resolveStub(spec);
  return url ? { url, shortCircuit: true } : next(spec, ctx);
}
