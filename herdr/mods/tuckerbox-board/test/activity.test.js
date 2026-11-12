// A run's activity on the board and in card detail, and Watch
// (agent-activity: Activity on the board, Waiting for a person;
// agent-terminals: Watch from the board).   node --test test/
// Activity times are Brisbane time whatever the computer's time zone, so
// these run in New York's.
process.env.TZ = "America/New_York";

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { normaliseBoard, normaliseCard, normaliseDetail, counts, drawerRows, needsYou, activityLine, activityRows, duration, eventText, fmtBrisbane, fmtLocal } from "../model.js";
import { detailActions, cardActions, invocation, herdrCall, commandLine, actionLabel } from "../actions.js";

const raw = (name) => JSON.parse(readFileSync(new URL(`../fixtures/${name}`, import.meta.url), "utf8"));
const NOW = Date.parse("2026-11-12T09:15:00+10:00");
const minutesAgo = (m) => new Date(NOW - m * 60000).toISOString();

// add-login's builder on task 2.3, in a terminal on mini, after ticking 2.2.
const running = (over = {}) => normaliseCard({
  change: "add-login", column: "step:build", state: "running", step: "build", agent: "claude", round: 2,
  approval: { state: "approved" },
  activity: {
    step: "build", agent: "claude", model: "opus", effort: "high", round: 2,
    task: { number: "2.3", text: "Remember me on this device" }, task_label: "on 2.3",
    last_tick: { number: "2.2", at: minutesAgo(4), ticked: true, step: "build", round: 2 },
    step_started: minutesAgo(25), run_started: minutesAgo(70), last_activity: minutesAgo(1),
    agent_state: "working", backend: "terminal", terminal: { address: "t7", name: "tb-0123456789abcdef", machine: "mini" },
    ...over,
  },
}, { on: "mini", path: "/work/shop" });

const detail = normaliseDetail({
  change: "add-login",
  ticks: [
    { number: "2.2", at: minutesAgo(4), ticked: true, step: "build", round: 2 },
    { number: "2.1", at: minutesAgo(18), ticked: true, step: "build", round: 2 },
  ],
  history: [{ at: minutesAgo(4), event: "task_ticked", step: "build", round: 2, agent: "claude", task: "2.2" }],
});

// Card detail through a small gpui fluent-view stand-in, with the real
// model and actions.
const source = readFileSync(new URL("../card.js", import.meta.url), "utf8");
const stubs = `
class View {}
const node = () => new Proxy({ text: [], ids: [] }, { get(target, key, receiver) {
  if (key in target) return target[key];
  if (key === "when") return (yes, fn) => yes ? fn(receiver) : receiver;
  if (key === "child") return (child) => {
    if (typeof child === "string") target.text.push(child);
    else if (child?.text) { target.text.push(...child.text); target.ids.push(...child.ids); }
    return receiver;
  };
  if (key === "children") return (children) => { for (const child of children) receiver.child(child); return receiver; };
  if (key === "id") return (id) => { target.ids.push(id); return receiver; };
  return () => receiver;
} });
const div = node, h_flex = node, v_flex = node, TextView = { markdown: node };
const theme = () => ({}), props = () => null, closeOverlay = () => {}, loadDetail = async () => null;
const perform = async () => ({}), validate = async () => {}, watchEdits = () => [], changed = () => 0;
const editOf = () => null, editNote = () => null;
const dot = node, stateChip = node, approvalChip = node, progressBar = node, note = node, tone = () => "";
const section = (title) => { const n = node(); n.child(title); return n; };
const button = (id, text) => { const n = node(); n.id(id); n.child(text); return n; };
const tiny = button, closeButton = node;
`;
const model = new URL("../model.js", import.meta.url).href;
const actions = new URL("../actions.js", import.meta.url).href;
const body = source
  .replace(/^import .* from "\.\/model\.js";\n/m, (l) => l.replace("./model.js", model))
  .replace(/^import .* from "\.\/actions\.js";\n/m, (l) => l.replace("./actions.js", actions))
  .replace(/^import .* from "(?!file:).*";\n/gm, "");
const { CardView } = await import(`data:text/javascript;base64,${Buffer.from(stubs + body).toString("base64")}`);

// The board's cards through the same stand-in, with the real model and
// actions.
const boardStubs = stubs.replace("const div = node, h_flex = node, v_flex = node, TextView = { markdown: node };",
  "const div = node, h_flex = node, v_flex = node, svg = node;") + `
const openOverlay = () => {}, spaces = () => [], panes = () => [], focused = () => null, run = async () => ({}), notify = () => {};
const MOD = "tuckerbox-board", loadAll = async () => null, locate = () => null, candidates = () => [], fixturesFolder = () => "";
const SPINNER = "";
`;
const boardBody = readFileSync(new URL("../board.js", import.meta.url), "utf8")
  .replace(/^import .* from "\.\/model\.js";\n/m, (l) => l.replace("./model.js", model))
  .replace(/^import .* from "\.\/actions\.js";\n/m, (l) => l.replace("./actions.js", actions))
  .replace(/^import .* from "(?!file:).*";\n/gm, "");
const { BoardView } = await import(`data:text/javascript;base64,${Buffer.from(boardStubs + boardBody).toString("base64")}`);
const boardView = () => { const v = new BoardView(); v.board = { columns: [], steps: ["plan", "build", "review"], require_approval: true }; return v; };

test("Owner opens a running card: step, agent, model and effort, round, task, last tick, times and last activity", () => {
  const c = running();
  const view = new CardView();
  view.card = c;
  view.board = { columns: [{ id: "step:build", kind: "step", step: "build", name: "Build" }], steps: ["plan", "build", "review"], require_approval: true };
  view.detail = { ...detail, proposal: "", specs: [], tasks: [], rounds: [], groups: [] };
  const drawn = view.details();
  const text = drawn.text.join("\n");
  assert.ok(drawn.text.includes("Activity"));
  for (const want of ["build · round 2", "claude · opus · effort high", "on 2.3: Remember me on this device",
    "working", "terminal t7 on mini"]) {
    assert.ok(text.includes(want), `detail lacks ${JSON.stringify(want)}:\n${text}`);
  }
  // The rows with their times, as card detail shows them at NOW.
  const rows = Object.fromEntries(activityRows(c, NOW));
  assert.equal(rows["Last tick"], "2.2 ticked 09:11 (4m ago)");
  assert.equal(rows["Step running"], "25m, since 08:50");
  assert.equal(rows["Run running"], "1h 10m, since 08:05");
  assert.equal(rows["Last activity"], "09:14 (1m ago)");
  // The ticks of the current run as a timeline, newest first, at their
  // Brisbane times (09:11 and 08:57 on 12 Nov).
  const t2 = text.indexOf("ticked 2.2 in build · round 2"), t1 = text.indexOf("ticked 2.1 in build · round 2");
  assert.ok(t2 >= 0 && t1 > t2, `tick timeline:\n${text}`);
  assert.ok(/(^|\s)09:11$/m.test(text) && /(^|\s)08:57$/m.test(text), `tick times in Brisbane time:\n${text}`);
  assert.equal(eventText(detail.history[0]), "Ticked 2.2 build");
});

test("a running card shows step, agent and model, on <n> with its text, time since the last tick and running time", () => {
  const a = activityLine(running(), NOW);
  assert.deepEqual(a, {
    step: "build", agent: "claude · opus", task: "on 2.3", task_text: "Remember me on this device",
    tick: "2.2 ticked 4m ago", running: "running 25m", last: "last activity 1m ago", waiting: false,
  });
  const done = activityLine(running({ task: null, task_label: "all tasks in scope ticked", last_tick: null }), NOW);
  assert.equal(done.task, "all tasks in scope ticked");
  assert.equal(done.tick, "");
  assert.equal(duration(minutesAgo(25), NOW), "25m");
  assert.equal(duration(minutesAgo(26 * 60), NOW), "1d 2h");
});

test("Quiet agent: running for 25 minutes, last activity 12 minutes ago", () => {
  const c = running({ last_activity: minutesAgo(12), backend: "headless", terminal: null });
  const a = activityLine(c, NOW);
  assert.equal(a.running, "running 25m");
  assert.equal(a.last, "last activity 12m ago");
  // The card on the board shows both.
  const now = Date.now(), before = (m) => new Date(now - m * 60000).toISOString();
  const shown = running({ step_started: before(25), last_activity: before(12), last_tick: null, backend: "headless", terminal: null });
  const text = boardView().card(shown).text.join("\n");
  for (const want of ["build", "claude · opus", "on 2.3", "running 25m", "last activity 12m ago"]) {
    assert.ok(text.includes(want), `card lacks ${JSON.stringify(want)}:\n${text}`);
  }
  assert.ok(!text.includes("waiting for you"));
  assert.ok(boardView().card(running({ ...shown.activity, agent_state: "waiting for you" })).text.includes("waiting for you"));
});

test("activity times are Brisbane time on any computer, with Brisbane's day boundaries", () => {
  assert.equal(new Date(NOW).getTimezoneOffset() > 0, true, "the test runs west of UTC");
  const now = new Date("2026-11-13T00:10:00+10:00");
  // 23:30 yesterday in Brisbane, though the same day (08:30) in New York.
  assert.equal(fmtBrisbane("2026-11-12T23:30:00+10:00", now), "12 Nov 23:30");
  assert.equal(fmtLocal("2026-11-12T23:30:00+10:00", now), "08:30");
  assert.equal(fmtBrisbane("2026-11-12T14:05:00Z", now), "00:05");
  assert.equal(fmtBrisbane("2025-12-31T14:30:00Z", now), "1 Jan 00:30");
  assert.equal(fmtBrisbane("2025-12-31T13:30:00Z", now), "31 Dec 2025 23:30");
  assert.equal(fmtBrisbane("", now), "");
  const rows = Object.fromEntries(activityRows(running({ step_started: "2026-11-12T23:30:00+10:00", last_activity: "2026-11-13T00:05:00+10:00" }), now.getTime()));
  assert.equal(rows["Step running"], "40m, since 12 Nov 23:30");
  assert.equal(rows["Last activity"], "00:05 (5m ago)");
});

test("Permission prompt: a waiting card shows waiting for you and counts in the board's and drawer's needs-you count", () => {
  const b = normaliseBoard({ ...raw("board.json") });
  const before = counts(b).needs;
  const waiting = running({ agent_state: "waiting for you" });
  assert.ok(needsYou(waiting));
  assert.ok(!needsYou(running()));
  assert.equal(activityLine(waiting, NOW).waiting, true);
  const n = counts({ ...b, cards: [...b.cards, waiting] });
  assert.equal(n.needs, before + 1);
  assert.ok(drawerRows(n, "shop").some((r) => r.kind === "needs" && r.count === before + 1));
  // Only a running run's agent waits.
  assert.ok(!needsYou({ ...waiting, state: "stopped" }));
});

test("Watch a builder: Herdr focuses the agent's terminal on the card's machine", () => {
  const c = running();
  const board = { steps: ["plan", "build", "review"], require_approval: true };
  assert.ok(detailActions(c, board).includes("watch"));
  assert.ok(!detailActions(c, board).includes("follow"), "a terminal card offers Watch, not Follow log");
  assert.deepEqual(cardActions(c, board), ["watch", "stop"]);
  assert.equal(actionLabel("watch"), "Watch ↗");
  assert.equal(commandLine("watch", "add-login"), null);
  const inv = invocation("watch", c);
  assert.equal(inv.place, "view");
  assert.equal(inv.on, "mini");
  assert.deepEqual(herdrCall(inv, { shown: { id: "s1", machine: null } }), { command: "agent.focus", args: { terminal: "mini/t7" } });
  // On this computer the address alone.
  assert.deepEqual(herdrCall(invocation("watch", { ...c, on: null })), { command: "agent.focus", args: { terminal: "t7" } });
  // No address, no Watch; an address that isn't one is refused.
  const headless = running({ backend: "headless", terminal: null });
  assert.ok(!detailActions(headless, board).includes("watch"));
  assert.ok(detailActions(headless, board).includes("follow"));
  assert.throws(() => invocation("watch", { ...c, activity: { ...c.activity, terminal: { address: "t7; rm" } } }));
});

test("the board fixture's activity normalises", () => {
  const b = normaliseBoard(raw("board.json"));
  const users = b.cards.find((c) => c.change === "add-users");
  assert.equal(users.activity.backend, "terminal");
  assert.equal(users.activity.terminal.address, "t7");
  assert.equal(users.activity.task.number, "2.3");
  assert.equal(b.cards.find((c) => c.change === "order-emails").activity.task, null);
  assert.equal(b.cards.find((c) => c.change === "add-search").activity, null);
  const d = normaliseBoard(raw("detail.json")).detail;
  assert.deepEqual(d.ticks.map((t) => t.number), ["2.2", "2.1"]);
});
