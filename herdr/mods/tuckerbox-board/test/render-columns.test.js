// Exercise BoardView's drawing code with a small gpui fluent-view stand-in.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { normaliseBoard, group } from "../model.js";

const source = readFileSync(new URL("../board.js", import.meta.url), "utf8");
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
const div = node, h_flex = node, v_flex = node;
const theme = () => ({}), needsYou = () => false, cardActions = () => [], defaultStep = () => "";
const stateChip = node, approvalChip = node, progressBar = node, editOf = () => null, editNote = () => null;
const ago = () => "recently", columnName = () => "In progress", activityLine = () => null;
const sortedColumn = () => true, dropPosition = () => null, eventBox = () => null, liveDrop = () => null, priorityCard = (c) => c;
`;
const { BoardView } = await import(`data:text/javascript;base64,${Buffer.from(stubs + source.replace(/^import .*;\r?\n/gm, "")).toString("base64")}`);
const fixture = JSON.parse(readFileSync(new URL("../fixtures/detail.json", import.meta.url), "utf8"));

test("renders the In progress column and the step or queued badge on its cards", () => {
  for (const [state, step, badge] of [["blocked", "review", "review"], ["queued", "build", "queued"]]) {
    const board = normaliseBoard({ ...fixture, cards: [{ ...fixture.cards[0], state, step, badge }] });
    const view = new BoardView();
    view.board = board;
    const drawn = view.column(group(board).find((col) => col.id === "in_progress"));
    assert.ok(drawn.ids.includes("col-in_progress"));
    assert.ok(drawn.text.includes("In progress"));
    assert.ok(drawn.text.includes(badge));
    if (badge === "queued") assert.ok(!drawn.text.includes("build"));
    // Its position in priority order (board-data: Priority on the board).
    assert.ok(drawn.text.includes("#2"));
  }
});

// approval-publish: Card after publishing: "not published" shows only while
// this machine's approval isn't on the remote.
test("shows not published until the approval is published", () => {
  for (const [published, shown] of [[false, true], [true, false], [undefined, false]]) {
    const approval = { ...fixture.cards[0].approval, state: "approved", published };
    const board = normaliseBoard({ ...fixture, cards: [{ ...fixture.cards[0], approval, state: "", step: "", badge: "" }] });
    const view = new BoardView();
    view.board = board;
    const card = board.cards[0];
    const drawn = view.column(group(board).find((col) => col.cards.some((c) => c.change === card.change)));
    assert.equal(drawn.text.includes("not published"), shown, `published ${published}`);
  }
});
