// The strict Herdr stub's member list (stub/herdr-api.json) against the
// installed Herdr's gpui-kit.d.ts and herdr.d.ts, when Herdr is installed (a
// developer's machine). CI has no Herdr and uses the committed list.
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { API } from "./stub/element.mjs";

const home = process.env.HERDR_APP_HOME || join(homedir(), ".config", "herdr-app");
const kit = join(home, "gpui-kit.d.ts");
const herdr = join(home, "herdr.d.ts");
const esc = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

test("every element member the board uses is in the installed gpui-kit.d.ts", { skip: !existsSync(kit) && `no ${kit}` }, () => {
  const d = readFileSync(kit, "utf8");
  const missing = API.element.filter((m) => !new RegExp(`^\\s+${esc(m)}(<Self|\\()`, "m").test(d));
  assert.deepEqual(missing, [], `not in ${kit}`);
  for (const [mod, names] of Object.entries(API.modules)) {
    const file = mod === "herdr" ? herdr : kit;
    if (!existsSync(file)) continue;
    const text = readFileSync(file, "utf8");
    const gone = names.filter((n) => !new RegExp(`export (abstract class|class|function|const) ${esc(n)}\\b`).test(text));
    assert.deepEqual(gone, [], `${mod} exports not in ${file}`);
  }
});
