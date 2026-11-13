// Operating modes on the board (add-operating-modes, modes-board): the mode
// and claims from the board document, Dispatch in the header and Take over
// on expired cards. The real BoardView and actions against the strict
// Herdr stub, with a fake exec that records what it runs.
//   node --test test/
import { test, beforeEach } from "node:test";
import assert from "node:assert/strict";
await import("./stub/register.mjs");
const { state, reset } = await import("./stub/herdr.mjs");
const { find, callsOf, text } = await import("./stub/element.mjs");
const { fakeCx } = await import("./stub/cx.mjs");
const { BoardView } = await import("../board.js");
const { normaliseBoard, mergeBoards, claimText } = await import("../model.js");
const { detailActions, cardActions, dispatchOffered, dispatchInvocation, invocation, commandLine, confirmText, detachCall, actionCard } = await import("../actions.js");

const COLUMNS = [
  { id: "proposed", kind: "proposed", name: "Proposed" }, { id: "approved", kind: "approved", name: "Approved" },
  { id: "in_progress", kind: "in_progress", name: "In progress" }, { id: "ready", kind: "ready", name: "Ready" }, { id: "done", kind: "done", name: "Done" },
];
const approved = { state: "approved", by: "Owner", at: "2026-11-13T08:00:00+10:00", committed: true, published: true };
const card = (change, extra = {}) => ({ change, title: change, column: "approved", tasks: { done: 0, total: 3 }, approval: approved, depends_on: [], settings: [], machines: [], ...extra });
const doc = (cards, extra = {}) => normaliseBoard({
  version: 1, repo: "github.com/acme/shop", path: "/work/shop", machine: "studio", os: "darwin", require_approval: true,
  mode: "fleet", fleet: { max_runs: 3, claims: 1 }, claims_read: { source: "fleet", at: "2026-11-13T09:00:00+10:00" },
  columns: COLUMNS, steps: ["build", "review"], cards, ...extra,
}, { on: null });
const expired = { machine: "mini", state: "expired", epoch: 1, renewed_at: "2026-11-13T08:48:00+10:00", run: "r1" };

let execs;
beforeEach(() => {
  reset();
  execs = [];
  state.exec = async (cmd, args, opts) => {
    execs.push({ cmd, args, cwd: opts?.cwd, on: opts?.on ?? null });
    if (cmd === "tuckerbox" && args[0] === "dispatch") return { code: 0, stdout: "add-login → mini\nadd-search → studio\n", stderr: "" };
    if (cmd === "tuckerbox" && args[0] === "run") return { code: 0, stdout: "add-login: taken over from mini (epoch 2), started build\n", stderr: "" };
    return { code: 1, stdout: "", stderr: "unexpected" };
  };
});

function view(b) {
  state.props = {};
  const v = new BoardView();
  v.init({ board: b }, fakeCx());
  v.at = { found: true, root: "/work/shop", cwd: "/work/shop", on: null };
  return v;
}
const click = (el, id, cx) => {
  const node = find(el, id);
  assert.ok(node, `no ${id}`);
  callsOf(node, "on_click")[0][0]({}, cx);
};

test("the board document's mode, fleet count and claims", () => {
  const b = doc([card("add-login", { claim: expired }), card("add-search")]);
  assert.equal(b.mode, "fleet");
  assert.deepEqual(b.fleet, { max_runs: 3, claims: 1 });
  assert.equal(b.claims_read.source, "fleet");
  assert.deepEqual(b.cards[0].claim, { machine: "mini", state: "expired", epoch: 1, renewed_at: expired.renewed_at, login: "", run: "r1" });
  assert.equal(b.cards[1].claim, null);
  // Older documents have no mode: solo, no claims.
  const solo = normaliseBoard({ version: 1, repo: "r", path: "/r", cards: [card("x", { claim: { state: "bogus" } })] });
  assert.equal(solo.mode, "solo");
  assert.equal(solo.cards[0].claim, null);
  assert.equal(claimText({ state: "open", login: "sam", machine: "studio" }), "sam on studio");
  assert.equal(claimText(expired), "expired on mini");
});

test("a claim from any machine's copy stays on the merged card", () => {
  const own = doc([card("add-login")]);
  const other = doc([card("add-login", { claim: expired })]);
  assert.equal(mergeBoards(own, [{ machine: "mini", board: other }]).cards[0].claim.machine, "mini");
});

test("Dispatch is offered only in fleet mode with an unclaimed Approved card", () => {
  assert.equal(dispatchOffered(doc([card("add-login"), card("add-search")])), true);
  assert.equal(dispatchOffered(doc([card("add-login", { claim: expired })])), false);
  assert.equal(dispatchOffered(doc([card("add-login", { column: "proposed", approval: { state: "none" } })])), false);
  assert.equal(dispatchOffered(doc([card("add-login", { column: "in_progress", state: "running" })])), false);
  assert.equal(dispatchOffered(doc([card("add-login")], { mode: "solo" })), false);
  assert.equal(dispatchOffered(doc([card("add-login")], { mode: "team" })), false);
  const v = view(doc([card("add-login", { claim: expired })]));
  assert.equal(find(v.header(), "dispatch"), null);
});

// modes-board: Dispatch from the board.
test("Dispatch from the board", async () => {
  const v = view(doc([card("add-login"), card("add-search")]));
  const cx = fakeCx();
  click(v.header(), "dispatch", cx);
  await cx.settle();
  assert.deepEqual(execs, [{ cmd: "tuckerbox", args: ["dispatch"], cwd: "/work/shop", on: null }]);
  // Where each change went, or why not, as dispatch printed it.
  assert.equal(v.notice.kind, "info");
  assert.match(v.notice.text, /add-login → mini/);
  assert.match(v.notice.text, /add-search → studio/);
  assert.equal(v.last, 0, "reads the board again at once");
  // On a board read from another machine it runs there, in a herdr-core
  // terminal, since dispatch starts runs.
  const remote = dispatchInvocation({ cwd: "/home/me/shop", on: "mini" });
  assert.equal(remote.detach, true);
  const args = detachCall(remote);
  assert.deepEqual(args.slice(args.indexOf("--on"), args.indexOf("--on") + 4), ["--on", "mini", "--cwd", "/home/me/shop"]);
  assert.deepEqual(args.slice(args.indexOf("--") + 1), ["tuckerbox", "dispatch"]);
});

test("Take over is offered only on expired fleet claims, and Start not on claimed cards", () => {
  const b = doc([card("add-login", { claim: expired })]);
  const c = b.cards[0];
  assert.ok(detailActions(c, b).includes("takeover"));
  assert.ok(cardActions(c, b).includes("takeover"));
  assert.ok(!detailActions(c, b).includes("start"));
  for (const state of ["active", "parked", "ready"]) {
    const x = doc([card("add-login", { claim: { ...expired, state } })]).cards[0];
    assert.ok(!detailActions(x, b).includes("takeover"), state);
    assert.ok(!detailActions(x, b).includes("start"), state);
  }
  const team = doc([card("add-login", { claim: { machine: "studio", state: "open", login: "sam" } })], { mode: "team" });
  assert.ok(!detailActions(team.cards[0], team).includes("takeover"));
  assert.deepEqual(commandLine("takeover", "add-login"), ["tuckerbox", "run", "--take-over", "add-login"]);
  assert.equal(invocation("takeover", { ...c, on: "mini" }).detach, true);
  assert.throws(() => commandLine("takeover", "--force"));
});

// modes-board: Take over an expired card.
test("Take over an expired card", async () => {
  const v = view(doc([card("add-login", { claim: expired })]));
  const cx = fakeCx();
  const drawn = v.card(v.board.cards[0]);
  assert.match(text(drawn).join(" "), /claim: expired on mini/);
  click(drawn, "takeover-add-login", cx);
  await cx.settle();
  // Nothing runs until the person confirms that mini's work is left behind.
  assert.deepEqual(execs, []);
  assert.equal(v.pending.text, confirmText("takeover", v.board.cards[0]));
  assert.match(v.pending.text, /left behind/);
  assert.match(v.pending.text, /mini/);
  const shown = v.render();
  click(shown, "confirm-no", cx);
  assert.equal(v.pending, null);
  click(v.card(v.board.cards[0]), "takeover-add-login", cx);
  click(v.render(), "confirm-yes", cx);
  await cx.settle();
  assert.deepEqual(execs, [{ cmd: "tuckerbox", args: ["run", "--take-over", "add-login"], cwd: "/work/shop", on: null }]);
  assert.equal(v.pending, null);
  assert.equal(v.last, 0, "reads the board again at once");
  // The next read: this machine holds the claim.
  v.board = doc([card("add-login", { column: "in_progress", state: "running", step: "build", claim: { machine: "studio", state: "active", epoch: 2, renewed_at: "2026-11-13T09:01:00+10:00" } })]);
  const after = text(v.card(v.board.cards[0])).join(" ");
  assert.match(after, /claim: active on studio/);
  assert.equal(find(v.card(v.board.cards[0]), "takeover-add-login"), null);
});

test("the header shows the mode, the fleet count and stale claims", () => {
  const words = (b) => text(view(b).header()).join(" ");
  assert.match(words(doc([])), /fleet 1\/3 claimed/);
  assert.match(words(doc([], { mode: "team", fleet: null, claims_read: { source: "forge", at: "2026-11-13T08:55:00+10:00", stale: true, error: "forge down" } })), /team.*claims from/);
  assert.doesNotMatch(words(doc([], { mode: "solo", fleet: null, claims_read: null })), /fleet|team/);
});

// Take over runs on the board's own machine (studio, this computer), not on
// the expired holder's: the merged board takes mini's copy of the card, with
// its run, its machine and its checkout there.
test("Take over of a card merged from the expired holder runs on the board's machine", async () => {
  const own = doc([card("add-login", { claim: expired })]);
  const mini = normaliseBoard({ ...own, path: "/srv/shop", machine: "mini", os: "linux",
    cards: [card("add-login", { column: "in_progress", state: "running", step: "build", claim: expired })] }, { on: "mini" });
  const merged = mergeBoards(own, [{ machine: "mini", board: mini }]);
  const c = merged.cards[0];
  assert.equal(c.on, "mini");
  assert.equal(c.path, "/srv/shop");
  assert.ok(detailActions(c, merged).includes("takeover"));

  // The board.
  const v = view(merged);
  const cx = fakeCx();
  click(v.card(c), "takeover-add-login", cx);
  click(v.render(), "confirm-yes", cx);
  await cx.settle();
  assert.deepEqual(execs, [{ cmd: "tuckerbox", args: ["run", "--take-over", "add-login"], cwd: "/work/shop", on: null }]);

  // Card detail, opened from that board: its detail read is on mini and
  // replaces the board path with mini's; Take over still runs here.
  execs = [];
  const { CardView } = await import("../card.js");
  state.props = { change: c.change, card: c, board: v.boardProps() };
  const d = new CardView();
  d.init({}, { ...fakeCx(), focus_handle: () => ({ focus() {}, is_focused: () => false }) });
  d.board = { ...d.board, path: "/srv/shop" };
  const dcx = fakeCx();
  d.act("takeover", {}, dcx);
  assert.deepEqual(execs, [], "asks first");
  assert.match(d.pending.confirm, /left behind/);
  d.act(d.pending.action, d.pending.extra, dcx);
  await dcx.settle();
  // (then it reads the detail again, on mini)
  assert.deepEqual(execs.filter((e) => e.args[0] === "run"), [{ cmd: "tuckerbox", args: ["run", "--take-over", "add-login"], cwd: "/work/shop", on: null }]);

  // A board shown from mini's space takes over on mini, in a herdr-core terminal there.
  const there = invocation("takeover", actionCard("takeover", c, view({ ...merged, on: "mini", path: "/srv/shop" }).home()));
  assert.equal(there.on, "mini");
  assert.equal(there.detach, true);
  // Other actions keep the card's machine.
  assert.equal(invocation("stop", actionCard("stop", c, v.home())).on, "mini");
});
