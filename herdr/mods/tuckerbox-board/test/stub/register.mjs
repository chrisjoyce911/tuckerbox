// Import first (await import) so the mod's "herdr", "gpui-kit" and "gpui-base"
// imports resolve to the strict stand-ins in this folder.
import * as mod from "node:module";
import { resolveStub } from "./hooks.mjs";

if (typeof mod.registerHooks === "function") {
  mod.registerHooks({
    resolve(spec, ctx, next) {
      const url = resolveStub(spec);
      return url ? { url, shortCircuit: true } : next(spec, ctx);
    },
  });
} else {
  mod.register("./hooks.mjs", import.meta.url);
}
