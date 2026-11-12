// The board's data rules, against the fixtures that define the board-data
// contract (README.md).   node --test test/
process.env.TZ = "Australia/Brisbane";

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  normaliseBoard, parseBoard, group, counts, columnName, approvalBadge, newestFirst, eventText, fmtLocal,
  taskGroups, pollInterval, due, isShown, dirFromStack, resolveWhere, selfPane, POLL_SHOWN_MS, POLL_HIDDEN_MS,
} from "../model.js";

const raw = (name) => JSON.parse(readFileSync(new URL(`../fixtures/${name}`, import.meta.url), "utf8"));
const board = normaliseBoard(raw("board.json"));
const detail = normaliseBoard(raw("detail.json"));

// The fields README.md lists. The fixtures must have each one, so the
// board-data worker can build `tuckerbox board --json` from them.
const DOC_KEYS = ["version", "repo", "path", "machine", "require_approval", "steps", "columns", "cards"];
const COLUMN_KEYS = ["id", "kind", "name"];
const CARD_KEYS = ["change", "title", "column", "tasks", "approval", "state", "reason", "badge", "step", "agent", "round",
  "since", "branch", "worktree", "machine", "edit_folder", "depends_on"];
const DETAIL_KEYS = ["change", "proposal", "specs", "tasks", "rounds", "history"];
const EVENT_KEYS = ["at", "event", "step", "round", "agent", "machine"];

test("fixtures have every field the README lists", () => {
  for (const name of ["board.json", "detail.json"]) {
    const doc = raw(name);
    for (const k of DOC_KEYS) assert.ok(k in doc, `${name}: ${k}`);
    for (const c of doc.columns) for (const k of COLUMN_KEYS) assert.ok(k in c, `${name}: column ${c.id}: ${k}`);
    for (const c of doc.columns.filter((c) => c.kind === "step")) assert.ok(c.step, `${name}: ${c.id} has its step`);
    for (const c of doc.cards) {
      for (const k of CARD_KEYS) assert.ok(k in c, `${name}: ${c.change}: ${k}`);
      for (const k of ["state", "by", "at", "committed"]) assert.ok(k in c.approval, `${name}: ${c.change}: approval.${k}`);
      for (const k of ["done", "total"]) assert.ok(k in c.tasks, `${name}: ${c.change}: tasks.${k}`);
      assert.ok(doc.columns.some((col) => col.id === c.column), `${name}: ${c.change} in a listed column`);
    }
  }
  const d = raw("detail.json").detail;
  for (const k of DETAIL_KEYS) assert.ok(k in d, `detail: ${k}`);
  for (const h of d.history) for (const k of EVENT_KEYS) assert.ok(k in h, `history ${h.event}: ${k}`);
  for (const t of d.tasks) for (const k of ["number", "group", "text", "checked"]) assert.ok(k in t, `task ${t.number}: ${k}`);
  for (const r of d.rounds) for (const k of ["round", "result", "findings"]) assert.ok(k in r, `round ${r.round}: ${k}`);
});

test("columns: Proposed, Approved, Queued, the steps, Ready, Done", () => {
  assert.deepEqual(board.columns.map((c) => c.name), ["Proposed", "Approved", "Queued", "Plan", "Build", "Review", "Ready", "Done"]);
  const cols = group(board);
  assert.equal(cols.length, 8);
  assert.deepEqual(cols.find((c) => c.id === "proposed").cards.map((c) => c.change), ["add-search", "refund-flow"]);
  assert.deepEqual(cols.find((c) => c.id === "step:build").cards.map((c) => c.change), ["add-users", "image-cdn"]);
  assert.equal(cols.reduce((n, c) => n + c.cards.length, 0), board.cards.length);
});

test("a card in a column the document doesn't list is kept, at the end", () => {
  const b = normaliseBoard({ ...raw("board.json"), cards: [{ change: "odd", column: "step:deploy" }] });
  const cols = group(b);
  assert.equal(cols.at(-1).id, "step:deploy");
  assert.deepEqual(cols.at(-1).cards.map((c) => c.change), ["odd"]);
});

test("header counts: running and needing you (blocked or failed)", () => {
  assert.deepEqual(counts(board), { running: 2, needs: 2, total: board.cards.length });
  assert.deepEqual(counts(null), { running: 0, needs: 0, total: 0 });
});

test("approval badge: approved, stale or none, and not committed kept", () => {
  assert.equal(approvalBadge({ state: "approved" }).word, "approved");
  assert.equal(approvalBadge({ state: "stale" }).word, "stale approval");
  assert.equal(approvalBadge({ state: "none" }).word, "not approved");
  const stale = board.cards.find((c) => c.change === "refund-flow");
  assert.equal(stale.column, "proposed");
  assert.equal(stale.approval.state, "stale");
  assert.equal(board.cards.find((c) => c.change === "add-wishlist").approval.committed, false);
});

test("Blocked review: in Review, blocked, review limit reached", () => {
  const c = board.cards.find((x) => x.change === "add-login-page");
  assert.equal(c.column, "step:review");
  assert.equal(c.state, "blocked");
  assert.equal(c.reason, "review limit reached");
});

test("dependencies carry their column's name", () => {
  const c = board.cards.find((x) => x.change === "checkout-v2");
  assert.deepEqual(c.depends_on, [{ change: "add-users", column: "step:build" }]);
  assert.equal(columnName(board, "step:build"), "Build");
  assert.equal(columnName(board, "step:gone"), "step:gone");
});

test("missing fields read as empty, and a title falls back to the id", () => {
  const b = normaliseBoard({ version: 1, columns: [], cards: [{ change: "add-login-page" }] });
  const c = b.cards[0];
  assert.equal(c.title, "add login page");
  assert.equal(c.approval.state, "none");
  assert.equal(c.approval.committed, true);
  assert.deepEqual(c.tasks, { done: 0, total: 0 });
  assert.deepEqual(c.depends_on, []);
  assert.equal(b.require_approval, true);
});

test("Space outside a repository: the error document is kept for the board to show", () => {
  const out = JSON.stringify({ version: 1, error: { code: "not_a_repository", message: "no .tuckerbox/config.toml in /tmp/x" } });
  const b = parseBoard(out, "", 1);
  assert.equal(b.error.code, "not_a_repository");
  assert.match(b.error.message, /config\.toml/);
  const f = parseBoard("", "tuckerbox: unknown command \"board\"\n", 2);
  assert.equal(f.error.code, "failed");
  assert.match(f.error.message, /unknown command/);
  assert.equal(parseBoard("not json", "", 0).error.code, "failed");
  assert.equal(parseBoard(JSON.stringify(raw("board.json")), "", 0, { on: "mini" }).cards[0].on, "mini");
});

test("Card detail: both rejections with findings, the full history, task 2.1 ticked", () => {
  const d = detail.detail;
  assert.equal(d.change, "add-login-page");
  assert.deepEqual(d.rounds.map((r) => r.result), ["rejected", "rejected", "blocked"]);
  assert.equal(d.rounds[0].findings.length, 2);
  const t21 = d.tasks.find((t) => t.number === "2.1");
  assert.equal(t21.checked, true);
  assert.deepEqual(taskGroups(d.tasks).map((g) => [g.group, g.tasks.length]), [["1. Page", 2], ["2. Sign in", 2], ["3. Header", 1]]);
  assert.equal(d.history.filter((h) => h.event === "rejected").length, 2);
});

test("Rejected change: history newest first shows the hand-off back to build", () => {
  const h = newestFirst(detail.detail.history);
  assert.equal(h[0].event, "blocked");
  assert.equal(h.at(-1).event, "started");
  const rejected1 = h.findIndex((x) => x.event === "rejected" && x.round === 1);
  const build2 = h.findIndex((x) => x.event === "step_started" && x.step === "build" && x.round === 2);
  assert.ok(build2 < rejected1, "round 2's build comes after round 1's rejection, so above it");
  // Same timestamp: step_finished then handoff in the file; newest first reverses them.
  const same = h.filter((x) => x.at === "2026-11-11T15:20:00+10:00").map((x) => x.event);
  assert.deepEqual(same, ["handoff", "step_finished"]);
  assert.equal(eventText(h.find((x) => x.event === "handoff")), "Handed on build → review: Unknown email gets the same message.");
  assert.equal(eventText(h.find((x) => x.event === "rejected")), "Rejected review with 1 finding: One finding left.");
  assert.equal(eventText(h[0]), "Blocked review: review limit reached");
});

test("times show in local time (here: Brisbane)", () => {
  const now = new Date("2026-11-12T12:00:00+10:00");
  assert.equal(fmtLocal("2026-11-12T08:15:30+10:00", now), "08:15");
  assert.equal(fmtLocal("2026-11-11T22:15:30Z", now), "08:15", "UTC is converted");
  assert.equal(fmtLocal("2026-11-11T16:02:00+10:00", now), "11 Nov 16:02");
  assert.equal(fmtLocal("2025-01-02T09:00:00+10:00", now), "2 Jan 2025 09:00");
  assert.equal(fmtLocal("", now), "");
  assert.equal(fmtLocal("garbage", now), "");
});

test("polling: every 5 s while shown, 30 s while hidden", () => {
  assert.equal(pollInterval(true), 5000);
  assert.equal(pollInterval(false), 30000);
  assert.equal(POLL_SHOWN_MS, 5000);
  assert.equal(POLL_HIDDEN_MS, 30000);
  assert.equal(due(0, 1000, false), true, "never read: read now");
  assert.equal(due(10_000, 14_999, true), false);
  assert.equal(due(10_000, 15_000, true), true);
  assert.equal(due(10_000, 15_000, false), false);
  assert.equal(due(10_000, 40_000, false), true);
});

// Two spaces on two machines: A (here) with a board pane and a shell, B on
// "mini" with a shell. Both tabs of A can hold boards.
const MOD = "tuckerbox-board";
const world = (o = {}) => ({
  spaces: [
    { id: "A", active: o.active !== "B", machine: null, folder: "/repo/a", tabs: [
      { id: "a1", active: o.tab !== "a2", zoomed: Boolean(o.zoomed) },
      { id: "a2", active: o.tab === "a2", zoomed: false },
    ] },
    { id: "B", active: o.active === "B", machine: "mini", folder: "/repo/b", tabs: [{ id: "b1", active: true, zoomed: false }] },
  ],
  panes: [
    { pane: "pa-shell", tab: "a1", cwd: "/repo/a/sub", zoomed: Boolean(o.zoomed) },
    { pane: "pa-board", tab: "a1", mod: MOD, cwd: null, zoomed: false },
    { pane: "pa-board2", tab: "a2", mod: MOD, cwd: null, zoomed: false },
    { pane: "pb-shell", tab: "b1", cwd: "/repo/b", zoomed: false },
  ],
});

test("where: focus in another space doesn't move the board's repository or machine", () => {
  const w = world();
  // Docked with its space: focus moves to B's terminal (on mini).
  const docked = resolveWhere({ props: { space: "A" }, ...w, focused: { pane: "pb-shell" } });
  assert.deepEqual(docked, { cwd: "/repo/a/sub", on: null, space: "A" });
  // B shown and focused: still A, still this computer.
  const wb = world({ active: "B" });
  assert.deepEqual(resolveWhere({ props: { space: "A" }, ...wb, focused: { pane: "pb-shell" } }), { cwd: "/repo/a/sub", on: null, space: "A" });
  // A pane copy without a space prop finds its space from its own pane.
  assert.deepEqual(resolveWhere({ props: {}, ...wb, focused: { pane: "pb-shell" }, self: "pa-board" }), { cwd: "/repo/a/sub", on: null, space: "A" });
  // Focus inside the board's own space is followed.
  assert.equal(resolveWhere({ props: { space: "A" }, ...w, focused: { pane: "pa-shell" } }).cwd, "/repo/a/sub");
});

test("where: a window follows the shown space, folder and machine together", () => {
  const wb = world({ active: "B" });
  assert.deepEqual(resolveWhere({ props: { overlay: "o1" }, ...wb, focused: { pane: "pb-shell" } }), { cwd: "/repo/b", on: "mini", space: "B" });
  // Focus left in A while B is shown: B's own terminal, never A's folder on mini.
  assert.deepEqual(resolveWhere({ props: { overlay: "o1" }, ...wb, focused: { pane: "pa-shell" } }), { cwd: "/repo/b", on: "mini", space: "B" });
});

test("where: a space prop that is gone gives no folder, not another space's", () => {
  const w = world();
  assert.deepEqual(resolveWhere({ props: { space: "gone" }, ...w, focused: { pane: "pa-shell" } }), { cwd: "", on: null, space: null });
  assert.equal(resolveWhere({ props: { space: "A", folder: "/pinned" }, ...w, focused: null }).cwd, "/pinned");
});

test("shown: each copy by its own pane, two copies in two tabs", () => {
  const w = world({ tab: "a1" });
  assert.equal(isShown({ self: "pa-board", ...w, mod: MOD }), true);
  assert.equal(isShown({ self: "pa-board2", ...w, mod: MOD }), false, "the copy in the other tab is hidden");
  const w2 = world({ tab: "a2" });
  assert.equal(isShown({ self: "pa-board", ...w2, mod: MOD }), false);
  assert.equal(isShown({ self: "pa-board2", ...w2, mod: MOD }), true);
  // Another space shown: both hidden.
  const wb = world({ active: "B" });
  assert.equal(isShown({ self: "pa-board", ...wb, mod: MOD }), false);
  assert.equal(isShown({ self: "pa-board2", ...wb, mod: MOD }), false);
});

test("shown: a board behind a zoomed sibling is hidden", () => {
  const w = world({ zoomed: true });
  assert.equal(isShown({ self: "pa-board", ...w, mod: MOD }), false);
  w.panes.find((p) => p.pane === "pa-board").zoomed = true;
  w.panes.find((p) => p.pane === "pa-shell").zoomed = false;
  assert.equal(isShown({ self: "pa-board", ...w, mod: MOD }), true, "the board itself zoomed");
});

test("shown: windows, the only board pane, and copies that can't tell", () => {
  const w = world({ tab: "a1" });
  assert.equal(isShown({ overlay: "o1", self: null, ...world({ active: "B" }), mod: MOD }), true);
  const one = { ...w, panes: w.panes.filter((p) => p.pane !== "pa-board2") };
  assert.equal(selfPane({ self: null, panes: one.panes, mod: MOD }), "pa-board");
  assert.equal(isShown({ self: null, ...world({ active: "B" }), panes: one.panes, mod: MOD }), false, "the only board pane is ours");
  assert.equal(selfPane({ self: null, panes: w.panes, mod: MOD }), null, "two board panes: unknown");
  assert.equal(isShown({ self: null, ...w, mod: MOD }), true, "unknown: shown while any board pane is");
  assert.equal(isShown({ self: null, ...world({ active: "B" }), mod: MOD }), false, "unknown: hidden when none is");
  assert.equal(isShown({ self: null, ...w, panes: w.panes.filter((p) => !p.mod), mod: MOD }), true, "not a pane: a layout block");
});

test("the mod's folder from a stack line", () => {
  assert.equal(dirFromStack("    at <anonymous> (/Users/me/mods/tuckerbox-board/data.js?v=1:3:33)", "data.js"), "/Users/me/mods/tuckerbox-board");
  assert.equal(dirFromStack("at f (/a b/c/data.js:1:2)", "data.js"), "/a b/c");
  assert.equal(dirFromStack("at f (C:\\Users\\me\\mods\\tuckerbox-board\\data.js:1:2)", "data.js"), "C:\\Users\\me\\mods\\tuckerbox-board");
  assert.equal(dirFromStack("at f (/x/mydata.js:1:2)", "data.js"), null);
  assert.equal(dirFromStack("nothing here", "data.js"), null);
});

test("compact fixture: In progress keeps step badges and all rerun choices", async () => {
  const { rerunSteps } = await import("../actions.js");
  assert.deepEqual(detail.columns.map((c) => c.id), ["proposed", "approved", "in_progress", "ready", "done"]);
  const col = group(detail).find((c) => c.id === "in_progress");
  assert.equal(col.cards[0].badge, "review");
  assert.equal(col.cards[0].state, "blocked");
  assert.deepEqual(rerunSteps(col.cards[0], detail), ["plan", "build", "review"]);
  const queued = normaliseBoard({ ...raw("detail.json"), cards: [{ change: "waiting", column: "in_progress", state: "queued", step: "build", badge: "queued" }] });
  assert.equal(group(queued).find((c) => c.id === "in_progress").cards[0].badge, "queued");
});
