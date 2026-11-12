// BoardView's drawing code against the strict Herdr stub (test/stub): a call
// gpui-kit.d.ts doesn't declare throws.   node --test test/
import { test, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
await import("./stub/register.mjs");
const { state, reset } = await import("./stub/herdr.mjs");
const { find, all, has, callsOf, text, idOf } = await import("./stub/element.mjs");
const { fakeCx } = await import("./stub/cx.mjs");
const { BoardView } = await import("../board.js");
const { normaliseBoard, group } = await import("../model.js");

const json = (name) => JSON.parse(readFileSync(new URL(`../fixtures/${name}`, import.meta.url), "utf8"));
const detail = json("detail.json");
const board = normaliseBoard(json("board.json"));

beforeEach(() => reset());

function view(b, props = {}) {
  state.props = props;
  const v = new BoardView();
  v.init({ board: b }, fakeCx());
  return v;
}

test("renders the In progress column and the step or queued badge on its cards", () => {
  for (const [st, step, badge] of [["blocked", "review", "review"], ["queued", "build", "queued"]]) {
    const b = normaliseBoard({ ...detail, cards: [{ ...detail.cards[0], state: st, step, badge }] });
    const drawn = view(b).column(group(b).find((col) => col.id === "in_progress"));
    assert.equal(idOf(drawn), "col-in_progress");
    const words = text(drawn);
    assert.ok(words.includes("In progress"));
    assert.ok(words.includes(badge));
    if (badge === "queued") assert.ok(!words.includes("build"));
    // Its position in priority order (board-data: Priority on the board).
    assert.ok(words.includes("#2"));
  }
});

// approval-publish: Card after publishing: "not published" shows only while
// this machine's approval isn't on the remote.
test("shows not published until the approval is published", () => {
  for (const [published, shown] of [[false, true], [true, false], [undefined, false]]) {
    const approval = { ...detail.cards[0].approval, state: "approved", published };
    const b = normaliseBoard({ ...detail, cards: [{ ...detail.cards[0], approval, state: "", step: "", badge: "" }] });
    const card = b.cards[0];
    const drawn = view(b).column(group(b).find((col) => col.cards.some((c) => c.change === card.change)));
    assert.equal(text(drawn).includes("not published"), shown, `published ${published}`);
  }
});

// Columns scroll sideways: the row, its columns and their lists as drawn.
function columnsOf(props) {
  const root = view(board, props).render();
  const row = find(root, "columns");
  assert.ok(row, "no column row");
  const cols = row.kids.filter((k) => String(idOf(k)).startsWith("col-"));
  return { root, row, cols };
}

for (const [name, props] of [["Narrow docked pane", { folder: "/work/shop", machine: null }], ["Wide window", { overlay: "o1" }]]) {
  test(`${name}: one sideways scroll row of fixed 236 px columns, none dropped, header outside it`, () => {
    const { root, row, cols } = columnsOf(props);
    assert.ok(has(row, "overflow_x_scrollbar"), "the row scrolls sideways with Herdr's scrollbar");
    assert.deepEqual(cols.map(idOf), group(board).map((c) => `col-${c.id}`), "every column is drawn");
    for (const c of cols) {
      assert.deepEqual(callsOf(c, "w"), [[236]], `${idOf(c)} is 236 px`);
      assert.ok(has(c, "flex_none"), `${idOf(c)} is never squeezed`);
      assert.ok(!has(c, "flex_1"), `${idOf(c)} is never stretched`);
    }
    assert.ok(!all(row).some((x) => has(x, "flex_wrap")), "columns don't wrap");
    assert.ok(!all(find(root, "tuckerbox-board").kids[0]).some((x) => idOf(x) === "columns"), "the header is outside the row");
  });
}

test("Plain wheel over a column: each list scrolls up and down, and nothing handles the wheel itself", () => {
  const { root, cols } = columnsOf({ overlay: "o1" });
  for (const c of cols) {
    const list = c.kids.find((k) => String(idOf(k)).startsWith("col-list-"));
    assert.ok(list && has(list, "overflow_y_scrollbar"), `${idOf(c)} list scrolls vertically`);
  }
  assert.ok(!all(root).some((x) => has(x, "on_scroll_wheel")), "no wheel handler of its own");
});

test("the strict stub refuses a member gpui-kit.d.ts doesn't declare", async () => {
  const { div } = await import("./stub/gpui-kit.mjs");
  assert.throws(() => div().scroll_to_item(3), /declares no element member scroll_to_item/);
});

// No approval badge on Done cards.
const WORDS = ["not approved", "approved", "stale approval"];
const chips = (el) => text(el).filter((w) => WORDS.includes(w));

test("Archived change: its Done card shows no approval badge", () => {
  const done = board.cards.find((c) => c.column === "done");
  for (const approval of [done.approval, { ...done.approval, state: "approved" }]) {
    const v = view(board);
    assert.deepEqual(chips(v.card({ ...done, approval })), [], `approval ${approval.state}`);
  }
});

test("Active change without approval: a Proposed card still shows not approved", () => {
  const card = board.cards.find((c) => c.column === "proposed" && c.approval.state === "none");
  assert.deepEqual(chips(view(board).card(card)), ["not approved"]);
});

test("Archived change: card detail shows no approval row for a Done card, and does for a Proposed one", async () => {
  const { CardView } = await import("../card.js");
  const detailOf = (card) => {
    state.props = { change: card.change, card, board: { columns: board.columns, require_approval: true } };
    const v = new CardView();
    v.init({}, { ...fakeCx(), focus_handle: () => ({ focus() {}, is_focused: () => false }) });
    v.detail = { ...detail.detail, change: card.change };
    return v.details();
  };
  const done = board.cards.find((c) => c.column === "done");
  assert.deepEqual(chips(detailOf({ ...done, approval: { ...done.approval, state: "approved", by: "ann" } })), []);
  const proposed = board.cards.find((c) => c.column === "proposed" && c.approval.state === "none");
  assert.deepEqual(chips(detailOf(proposed)), ["not approved"]);
});
