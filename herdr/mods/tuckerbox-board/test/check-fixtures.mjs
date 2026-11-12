// Renders the board and the card detail from the fixtures with
// `herdr-app check --print-spec`, and looks for what each must show.
// `herdr-app check` renders a mod once, before any async read finishes, so
// this copies the mod to a temporary folder whose main.js hands the fixture
// to the view directly. Skips when herdr-app isn't installed (as in CI).
//
//   node herdr/mods/tuckerbox-board/test/check-fixtures.mjs
import { execFileSync, spawnSync } from "node:child_process";
import { cpSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const mod = join(dirname(fileURLToPath(import.meta.url)), "..");
const which = spawnSync(process.platform === "win32" ? "where" : "which", ["herdr-app"], { encoding: "utf8" });
if (which.status !== 0) {
  console.log("check-fixtures: herdr-app not installed, skipped");
  process.exit(0);
}

const board = readFileSync(join(mod, "fixtures", "board.json"), "utf8");
const detail = readFileSync(join(mod, "fixtures", "detail.json"), "utf8");

const views = {
  board: {
    main: `import { View } from "gpui-kit";
import { BoardView } from "./board.js";
import { normaliseBoard } from "./model.js";
import doc from "./fixture.js";
export default class Main extends View {
  init(p, cx) { this.v = new BoardView({ board: normaliseBoard(doc) }); this.v.init({ board: normaliseBoard(doc) }, cx); }
  render(cx) { return this.v.render(cx); }
}`,
    fixture: board,
    want: ["Tuckerbox", "github.com/example/shop", "Proposed", "Approved", "Queued", "Plan", "Build", "Review", "Ready", "Done",
      "add login page", "review limit reached", "stale approval", "not committed", "2 running", "2 need you",
      "after add-users (Build)", "Approve", "▶ Start", "Resume", "Stop", "Re-run build", "round 3"],
  },
  "no-config": {
    main: `import { View } from "gpui-kit";
import { BoardView } from "./board.js";
const b = { error: { code: "no_config", message: "" } };
export default class Main extends View {
  init(p, cx) { this.v = new BoardView({ board: b }); this.v.init({ board: b }, cx); }
  render(cx) { return this.v.render(cx); }
}`,
    fixture: "null",
    want: ["No Tuckerbox config in this space"],
    absent: ["tuckerbox init", "Dock"],
  },
  drawer: {
    main: `import { View } from "gpui-kit";
import { DrawerView } from "./drawer.js";
const p = { at: { found: true, root: "/work/shop" }, counts: { running: 2, needs: 1, total: 6 }, repo: "shop" };
export default class Main extends View {
  init(_p, cx) { this.v = new DrawerView(p); this.v.init(p, cx); }
  render(cx) { return this.v.render(cx); }
}`,
    fixture: "null",
    want: ["shop", "running", "needs you"],
  },
  "drawer-no-config": {
    main: `import { View } from "gpui-kit";
import { DrawerView } from "./drawer.js";
const p = { at: { found: false, root: "" }, counts: null, repo: "" };
export default class Main extends View {
  init(_p, cx) { this.v = new DrawerView(p); this.v.init(p, cx); }
  render(cx) { return this.v.render(cx); }
}`,
    fixture: "null",
    want: [],
    absent: ["running", "Tuckerbox", "tuckerbox-drawer"],
  },
  card: {
    main: `import { View } from "gpui-kit";
import { CardView } from "./card.js";
import { normaliseBoard } from "./model.js";
import doc from "./fixture.js";
const b = normaliseBoard(doc);
const p = { change: b.detail.change, card: b.cards[0], board: b, detail: b.detail };
export default class Main extends View {
  init(_p, cx) { this.v = new CardView(p); this.v.init(p, cx); }
  render(cx) { return this.v.render(cx); }
}`,
    fixture: detail,
    want: ["add login page", "PROPOSAL", "TASKS", "2.1", "Check the password against the users table", "DEPENDS ON", "add-users",
      "ROUNDS", "Round 1", "rejected", "The form uses GET, so the password ends up in the access log.", "Round 3",
      "HISTORY", "Blocked review: review limit reached", "Handed on build → review", "approved", "by Sam Example",
      "committed", "Resume", "Re-run review", "Follow review log ↗", "Open worktree", "EDIT", "login spec",
      // blocked-answers: a blocked card's question box (this one blocked on
      // the review limit, so its step asked nothing) with both answers.
      "QUESTION", "The run is blocked, but its step left no question.", "Answer and resume", "Answer"],
    // Started is the oldest event: newest first puts Blocked before it.
    order: ["Blocked review: review limit reached", "Run started"],
  },
};

views.compact = {
  ...views.board,
  fixture: detail,
  want: ["Tuckerbox", "Proposed", "Approved", "In progress", "Ready", "Done", "review", "review limit reached"],
};

let failed = 0;
for (const [name, v] of Object.entries(views)) {
  const dir = mkdtempSync(join(tmpdir(), `tuckerbox-board-${name}-`));
  try {
    cpSync(mod, dir, { recursive: true, filter: (src) => !src.includes(`${join(mod, "test")}`) });
    writeFileSync(join(dir, "fixture.js"), `export default ${v.fixture};\n`);
    writeFileSync(join(dir, "main.js"), v.main);
    let out;
    try { out = execFileSync("herdr-app", ["check", dir, "--print-spec"], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }); }
    catch (e) { console.error(`${name}: herdr-app check failed\n${e.stdout ?? ""}${e.stderr ?? ""}`); failed++; continue; }
    const missing = v.want.filter((w) => !out.includes(JSON.stringify(w).slice(1, -1)));
    if (missing.length) { console.error(`${name}: missing ${missing.map((m) => JSON.stringify(m)).join(", ")}`); failed++; }
    const extra = (v.absent ?? []).filter((w) => out.includes(JSON.stringify(w).slice(1, -1)));
    if (extra.length) { console.error(`${name}: should not show ${extra.map((m) => JSON.stringify(m)).join(", ")}`); failed++; }
    if (v.order) {
      const [a, b] = v.order.map((s) => out.indexOf(s));
      if (a < 0 || b < 0 || a > b) { console.error(`${name}: expected ${JSON.stringify(v.order[0])} before ${JSON.stringify(v.order[1])}`); failed++; }
    }
    if (!missing.length && !extra.length) console.log(`${name}: ok`);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}
process.exit(failed ? 1 : 0);
