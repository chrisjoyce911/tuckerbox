// Answering a blocked or waiting card from its detail (blocked-answers:
// Answering from the board).   node --test test/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, writeFileSync, mkdtempSync, chmodSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFileSync } from "node:child_process";
import { normaliseCard, answerable } from "../model.js";
import { invocation, commandLine, answerActions, answerText, stdinCall, detailActions, MAX_ANSWER } from "../actions.js";

const QUESTION = "Live Herdr check needed; keep or drop install rollback?";
const blocked = (over = {}) => normaliseCard({
  change: "fix-board-safety", column: "step:build", state: "blocked", step: "build", agent: "claude", round: 2,
  reason: `step build is blocked: ${QUESTION}`, approval: { state: "approved" },
  question: {
    kind: "blocked", step: "build", round: 2, summary: QUESTION,
    findings: [{ severity: "info", file: "install.go", detail: "Rollback restores the previous mod." }],
    answers: [{ text: "Drawer works.", by: "Chris", at: "2026-11-12T23:30:00Z", step: "build", round: 2 }],
  },
  ...over,
}, { on: "mini", path: "/work/shop" });

// Card detail through a gpui fluent-view stand-in, with the real model and
// actions; perform records what it is asked to run, and the next read
// finds the run moved on.
const source = readFileSync(new URL("../card.js", import.meta.url), "utf8");
const stubs = `
class View {}
const node = () => new Proxy({ text: [], ids: [], clicks: {} }, { get(target, key, receiver) {
  if (key in target) return target[key];
  if (key === "when") return (yes, fn) => yes ? fn(receiver) : receiver;
  if (key === "child") return (child) => {
    if (typeof child === "string") target.text.push(child);
    else if (child?.text) { target.text.push(...child.text); target.ids.push(...child.ids); Object.assign(target.clicks, child.clicks); }
    return receiver;
  };
  if (key === "children") return (children) => { for (const child of children) receiver.child(child); return receiver; };
  if (key === "id") return (id) => { target.ids.push(id); return receiver; };
  return () => receiver;
} });
const div = node, h_flex = node, v_flex = node, TextView = { markdown: node };
const Textarea = { new: () => { const n = node(); n.id("answer-box"); return n; } };
const TextareaState = { new: () => ({ v: "", value() { return this.v; }, set_value(v) { this.v = v; } }) };
const theme = () => ({}), props = () => null, closeOverlay = () => {};
globalThis.__runs = [];
globalThis.__next = null;
const loadDetail = async () => globalThis.__next;
const perform = async (inv) => { globalThis.__runs.push(inv); return { ok: true, message: "fix-board-safety: started" }; };
const validate = async () => {}, watchEdits = () => [], changed = () => 0;
const editOf = () => null, editNote = () => null;
const dot = node, stateChip = node, approvalChip = node, progressBar = node, note = node, tone = () => "";
const section = (title) => { const n = node(); n.child(title); return n; };
const button = (id, text, click) => { const n = node(); n.id(id); n.child(text); n.clicks[id] = click; return n; };
const tiny = button, closeButton = node;
`;
const model = new URL("../model.js", import.meta.url).href;
const actions = new URL("../actions.js", import.meta.url).href;
const body = source
  .replace(/^import .* from "\.\/model\.js";\n/m, (l) => l.replace("./model.js", model))
  .replace(/^import .* from "\.\/actions\.js";\n/m, (l) => l.replace("./actions.js", actions))
  .replace(/^import .* from "(?!file:).*";\n/gm, "");
const { CardView } = await import(`data:text/javascript;base64,${Buffer.from(stubs + body).toString("base64")}`);

// A cx whose spawned tasks the test can wait for.
function context() {
  const tasks = [];
  return { tasks, notify() {}, spawn(fn) { tasks.push(fn(this)); }, open_url() {} };
}
async function settle(cx) {
  for (let i = 0; i < cx.tasks.length; i++) await cx.tasks[i];
}

function openCard(card) {
  const view = new CardView();
  view.change = card.change;
  view.card = card;
  view.board = { columns: [{ id: "step:build", kind: "step", step: "build", name: "Build" }], steps: ["plan", "build", "review"], require_approval: true };
  view.detail = { change: card.change, proposal: "", specs: [], tasks: [], rounds: [], groups: [], history: [], ticks: [] };
  view.answerBox = { v: "", value() { return this.v; }, set_value(v) { this.v = v; } };
  view.busy = null;
  return view;
}

test("Answer and resume from the card: tuckerbox answer fix-board-safety --resume - with the text, and the card moves on", async () => {
  const card = blocked();
  const view = openCard(card);
  const drawn = view.details();
  const text = drawn.text.join("\n");
  // The question, its findings and the answer so far, a text box, and both buttons.
  for (const want of ["Question", "asked by build · round 2", QUESTION, "Rollback restores the previous mod.", "Drawer works.",
    "answered by Chris, ", "Answer and resume", "Answer"]) {
    assert.ok(text.includes(want), `detail lacks ${JSON.stringify(want)}:\n${text}`);
  }
  // When it was answered, in Brisbane time (09:30 on 13 Nov).
  assert.match(text, /answered by Chris, (13 Nov (2026 )?)?09:30/);
  assert.ok(drawn.ids.includes("answer-box"));
  assert.ok(drawn.ids.includes("act-answer_resume") && drawn.ids.includes("act-answer"));

  globalThis.__runs = [];
  globalThis.__next = { cards: [{ ...card, state: "running", question: null, reason: "" }], columns: [], steps: [], require_approval: true, path: "/work/shop",
    detail: { change: card.change, proposal: "", specs: [], tasks: [], rounds: [], groups: [], history: [], ticks: [] } };
  const decision = `Herdr check done by hand: drawer works.\nKeep rollback; don't run "$(rm -rf ~)" or 'quotes'.`;
  view.answerBox.set_value(decision);
  const cx = context();
  drawn.clicks["act-answer_resume"](null, cx);
  await settle(cx);
  assert.equal(globalThis.__runs.length, 1);
  const inv = globalThis.__runs[0];
  assert.deepEqual(inv.argv, ["tuckerbox", "answer", "fix-board-safety", "--resume", "-"]);
  assert.equal(inv.stdin, decision);
  assert.equal(inv.place, "exec");
  assert.equal(inv.on, "mini");
  assert.equal(inv.cwd, "/work/shop");
  // The board shows the command's result, empties the box and reads
  // again: the card has moved on from blocked.
  assert.equal(view.notice.text, "fix-board-safety: started");
  assert.equal(view.answerBox.value(), "");
  assert.equal(view.card.state, "running");
  assert.ok(!answerable(view.card));
});

test("Answer alone, and an empty answer is refused before anything runs", async () => {
  const card = blocked();
  const view = openCard(card);
  globalThis.__runs = [];
  globalThis.__next = { cards: [card], columns: [], steps: [], require_approval: true, path: "/work/shop",
    detail: { change: card.change, proposal: "", specs: [], tasks: [], rounds: [], groups: [], history: [], ticks: [] } };
  view.answerBox.set_value("  ");
  let cx = context();
  view.details().clicks["act-answer"](null, cx);
  await settle(cx);
  assert.equal(globalThis.__runs.length, 0);
  assert.match(view.notice.text, /Type an answer first/);
  view.notice = null;
  view.answerBox.set_value("use plan B");
  cx = context();
  view.details().clicks["act-answer"](null, cx);
  await settle(cx);
  assert.deepEqual(globalThis.__runs[0].argv, ["tuckerbox", "answer", "fix-board-safety", "-"]);
  assert.equal(globalThis.__runs[0].stdin, "use plan B");
});

test("a waiting terminal agent's card takes an answer, without resume; other cards take none", () => {
  const waiting = normaliseCard({
    change: "add-login", column: "step:review", state: "running", step: "review", round: 1, approval: { state: "approved" },
    activity: { step: "review", agent_state: "waiting for you", backend: "terminal", terminal: { address: "t7", name: "tb-1", machine: "" } },
    question: { kind: "waiting", step: "review", round: 1, summary: "Should I check the README as well?", findings: [], answers: [] },
  });
  assert.deepEqual(answerActions(waiting), ["answer"]);
  const text = openCard(waiting).details().text.join("\n");
  assert.ok(text.includes("the agent waits for you review · round 1") && text.includes("Should I check the README as well?"), text);
  assert.ok(!text.includes("Answer and resume"));
  // A waiting agent without a message still takes an answer.
  const bare = { ...waiting, question: null };
  assert.ok(openCard(bare).details().text.join("\n").includes("The agent waits in its terminal"));

  const working = { ...waiting, activity: { ...waiting.activity, agent_state: "working" } };
  assert.deepEqual(answerActions(working), []);
  assert.ok(!openCard(working).details().text.includes("Question"));
  assert.deepEqual(answerActions(blocked({ state: "stopped" })), []);
  assert.deepEqual(answerActions(blocked()), ["answer_resume", "answer"]);
  // The answer buttons are in the question box, not among the actions.
  assert.ok(!detailActions(blocked(), { steps: ["build"] }).some((a) => a.startsWith("answer")));
});

test("answer text: trimmed, not empty, at most 16 KiB of UTF-8", () => {
  assert.equal(answerText("  yes \n"), "yes");
  assert.throws(() => answerText(""), /Type an answer first/);
  assert.throws(() => commandLine("answer", "x", { text: " " }), /Type an answer first/);
  assert.equal(answerText("x".repeat(MAX_ANSWER)).length, MAX_ANSWER);
  assert.throws(() => answerText("é".repeat(MAX_ANSWER / 2 + 1)), /16 KiB/);
  assert.throws(() => invocation("answer", { change: "bad id!" }, { text: "x" }));
});

test("the text reaches tuckerbox answer on standard input, untouched by the shell", { skip: process.platform === "win32" && "no sh" }, () => {
  const bin = mkdtempSync(join(tmpdir(), "tb-answer-"));
  const out = join(bin, "out.json");
  // A stand-in tuckerbox that records its arguments and standard input.
  writeFileSync(join(bin, "tuckerbox"), `#!/bin/sh\nnode -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>require("fs").writeFileSync(process.argv[1],JSON.stringify({args:process.argv.slice(2),stdin:s})))' ${JSON.stringify(out)} "$@"\n`);
  chmodSync(join(bin, "tuckerbox"), 0o755);
  const text = `line one\n  "$HOME" $(echo no) \`echo no\` 'single' -n \\\\ %s %d\nlast`;
  const inv = invocation("answer_resume", blocked(), { text });
  const call = stdinCall(inv.argv, inv.stdin);
  assert.equal(call.command, "sh");
  execFileSync(call.command, call.args, { env: { ...process.env, PATH: `${bin}:${process.env.PATH}` } });
  const got = JSON.parse(readFileSync(out, "utf8"));
  assert.deepEqual(got.args, ["answer", "fix-board-safety", "--resume", "-"]);
  assert.equal(got.stdin, text);
});
