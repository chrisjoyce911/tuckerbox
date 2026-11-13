// Start, Run group, Resume and Re-run on another machine: the run engine
// these leave running must outlive the call, so they go through a short-lived
// herdr-core terminal (hc run) instead of exec, which ends every process it
// started. The real perform.js against a strict stand-in for the `herdr`
// module: any call a test didn't expect throws.   node --test test/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import * as actions from "../actions.js";

const { invocation, detachCall, detachedResult, DETACHED } = actions;

// The herdr stand-in. `expect` lists the calls a test allows, in order, each
// { fn, args, reply } (reply: a value to resolve with, or an Error to reject
// with); anything else, or a call out of order, fails the test.
const herdr = { expect: [], calls: [] };
function strict(fn) {
  return (...args) => {
    const want = herdr.expect.shift();
    herdr.calls.push({ fn, args });
    if (!want) throw new Error(`unexpected herdr.${fn}(${JSON.stringify(args)})`);
    assert.equal(fn, want.fn, `called herdr.${fn}, expected herdr.${want.fn}`);
    assert.deepEqual(args, want.args, `herdr.${fn} arguments`);
    return want.reply instanceof Error ? Promise.reject(want.reply) : Promise.resolve(want.reply);
  };
}
globalThis.__herdr = {
  exec: strict("exec"),
  hc: strict("hc"),
  run: () => { throw new Error("unexpected herdr.run"); },
  spaces: () => { throw new Error("unexpected herdr.spaces"); },
  panes: () => { throw new Error("unexpected herdr.panes"); },
  focused: () => { throw new Error("unexpected herdr.focused"); },
};
globalThis.__actions = actions;
const never = (name) => `() => { throw new Error("unexpected ${name}"); }`;
const stubs = `
const { exec, hc, run, spaces, panes, focused } = globalThis.__herdr;
const { shown, herdrCall, outcome, parseValidation, invocation, stdinCall, detachCall, detachedResult } = globalThis.__actions;
const startEdit = ${never("startEdit")}, tick = ${never("tick")}, markValidating = ${never("markValidating")}, setValidation = ${never("setValidation")}, keyCard = ${never("keyCard")};
const MOD = "tuckerbox-board";
`;
const source = readFileSync(new URL("../perform.js", import.meta.url), "utf8");
const { perform, changed } = await import(`data:text/javascript;base64,${Buffer.from(stubs + source.replace(/^import .*;\r?\n/gm, "")).toString("base64")}`);

function begin(expect) {
  herdr.expect = expect;
  herdr.calls = [];
}
function finish() {
  assert.deepEqual(herdr.expect, [], "every expected herdr call was made");
}

const remote = { change: "add-login", path: "/Users/sam/shop", on: "mini", step: "build" };
const local = { change: "add-login", path: "/work/shop", on: null };
const FLAGS = ["run", "--json", "--no-parent", "--no-notify", "--outlive-parent", "--timeout", "110"];
const runArgs = (argv, on = "mini", cwd = "/Users/sam/shop") => [...FLAGS, "--on", on, "--cwd", cwd, "--", ...argv];
const ran = (address, exit_code, output, extra = {}) => ({
  code: 0, stderr: "",
  stdout: JSON.stringify({ address, agent: "tuckerbox", cwd: "/Users/sam/shop", output, state: "live", turn: 0,
    wait: { children: [], ended: true, exit_code, reason: "exit", signal: null, status: null, terminal: address, turn: 0, ...extra } }),
});

test("which actions run detached: a remote card's start, group, resume, rerun, answer and resume, take over, and dispatch", () => {
  assert.deepEqual([...DETACHED].sort(), ["answer_resume", "dispatch", "group", "rerun", "resume", "start", "takeover"]);
  const extra = { step: "build", group: 2 };
  for (const a of ["start", "group", "resume", "rerun"]) {
    const r = invocation(a, remote, extra);
    assert.equal(r.place, "exec", a);
    assert.equal(r.detach, true, a);
    assert.equal(invocation(a, local, extra).detach, undefined, `${a} on this computer keeps exec`);
  }
  const answered = invocation("answer_resume", remote, { text: "use the v2 API" });
  assert.equal(answered.detach, true, "answer and resume restarts the engine");
  assert.equal(invocation("answer_resume", local, { text: "x" }).detach, undefined);
  assert.equal(invocation("answer", remote, { text: "x" }).detach, undefined, "a plain answer starts nothing");
  for (const a of ["approve", "unapprove", "stop", "validate"]) {
    assert.equal(invocation(a, remote).detach, undefined, `${a} returns before anything is left running`);
  }
});

test("detachCall: hc run on the card's machine and checkout, argv untouched", () => {
  const inv = invocation("rerun", remote, { step: "review" });
  assert.deepEqual(detachCall(inv), runArgs(["tuckerbox", "rerun", "add-login", "review"]));
  assert.deepEqual(detachCall({ ...invocation("group", remote, { group: 3 }), cwd: "" }),
    [...FLAGS, "--on", "mini", "--", "tuckerbox", "run", "add-login", "--tasks", "3"]);
  assert.throws(() => detachCall(invocation("approve", remote)));
  assert.throws(() => detachCall(invocation("start", local)));
});

// Answer and resume on another machine: the text still goes in on standard
// input, through sh (or PowerShell on Windows), inside the hc run terminal.
test("Answer and resume on another machine: hc run of the stdin call, no exec", async () => {
  const inv = invocation("answer_resume", remote, { text: "use the v2 API" });
  const sh = actions.stdinCall(inv.argv, inv.stdin, inv.os);
  assert.deepEqual(detachCall(inv), runArgs([sh.command, ...sh.args]));
  assert.deepEqual(sh.args.slice(-5), ["tuckerbox", "answer", "add-login", "--resume", "-"]);
  const win = invocation("answer_resume", { ...remote, os: "windows" }, { text: "use the v2 API" });
  const ps = actions.stdinCall(win.argv, win.stdin, "windows");
  assert.deepEqual(detachCall(win), runArgs([ps.command, ...ps.args]));
  begin([
    { fn: "hc", args: [runArgs([sh.command, ...sh.args])], reply: ran("mini/t9", 0, "add-login: answered; resumed") },
    { fn: "hc", args: [["rm", "--json", "mini/t9"]], reply: { code: 0, stdout: "", stderr: "" } },
  ]);
  const r = await perform(inv);
  assert.deepEqual(r, { ok: true, message: "add-login: answered; resumed" });
  finish();
});

test("Start on another machine: hc run, the command's output shown, the terminal removed, no exec", async () => {
  const before = changed();
  begin([
    { fn: "hc", args: [runArgs(["tuckerbox", "run", "add-login"])], reply: ran("mini/t7", 0, "add-login: started (pid 4242, queued) in /Users/sam/shop-worktrees/add-login") },
    { fn: "hc", args: [["rm", "--json", "mini/t7"]], reply: { code: 0, stdout: '{"removed":"mini/t7"}', stderr: "" } },
  ]);
  const r = await perform(invocation("start", remote));
  assert.deepEqual(r, { ok: true, message: "add-login: started (pid 4242, queued) in /Users/sam/shop-worktrees/add-login" });
  assert.equal(changed(), before + 1, "the board reads again at once");
  finish();
});

test("Resume and Re-run on another machine go the same way", async () => {
  for (const [action, argv] of [["resume", ["tuckerbox", "resume", "add-login"]], ["rerun", ["tuckerbox", "rerun", "add-login", "build"]]]) {
    begin([
      { fn: "hc", args: [runArgs(argv)], reply: ran("mini/t8", 0, "add-login: resumed") },
      { fn: "hc", args: [["rm", "--json", "mini/t8"]], reply: { code: 0, stdout: "", stderr: "" } },
    ]);
    assert.equal((await perform(invocation(action, remote, { step: "build" }))).ok, true, action);
    finish();
  }
});

test("a command that fails shows its own error, and the terminal still goes", async () => {
  const before = changed();
  const msg = "tuckerbox run: add-login: change add-login already has an active run (pid 35246)";
  begin([
    { fn: "hc", args: [runArgs(["tuckerbox", "run", "add-login"])], reply: ran("mini/t9", 1, msg) },
    { fn: "hc", args: [["rm", "--json", "mini/t9"]], reply: { code: 0, stdout: "", stderr: "" } },
  ]);
  assert.deepEqual(await perform(invocation("start", remote)), { ok: false, message: msg });
  assert.equal(changed(), before, "nothing changed");
  finish();
});

test("a terminal that can't start shows hc's error (from stderr or stdout), nothing to remove", async () => {
  const spawn = JSON.stringify({ error: { hint: "check the program name and cwd", kind: "spawn_failed", message: 'cannot start "tuckerbox": No such file or directory (os error 2)' } });
  const want = 'cannot start "tuckerbox": No such file or directory (os error 2) (check the program name and cwd)';
  for (const reply of [{ code: 1, stdout: "", stderr: spawn }, { code: 1, stdout: spawn, stderr: "" }]) {
    begin([{ fn: "hc", args: [runArgs(["tuckerbox", "run", "add-login"])], reply }]);
    assert.deepEqual(await perform(invocation("start", remote)), { ok: false, message: want });
    finish();
  }
  begin([{ fn: "hc", args: [runArgs(["tuckerbox", "run", "add-login"])], reply: { code: 4, stdout: "", stderr: "hc: not connected" } }]);
  assert.deepEqual(await perform(invocation("start", remote)), { ok: false, message: "hc: not connected" });
  finish();
  begin([{ fn: "hc", args: [runArgs(["tuckerbox", "run", "add-login"])], reply: new Error("hc is not installed") }]);
  assert.deepEqual(await perform(invocation("start", remote)), { ok: false, message: "hc is not installed" });
  finish();
});

test("still going when the wait checks in: shown as starting, the terminal kept", async () => {
  begin([{ fn: "hc", args: [runArgs(["tuckerbox", "run", "add-login"])], reply: ran("mini/t5", null, "", { ended: false, reason: "timeout" }) }]);
  assert.deepEqual(await perform(invocation("start", remote)), { ok: true, message: "still starting in terminal mini/t5" });
  finish();
});

test("a removal that fails changes nothing for the board", async () => {
  begin([
    { fn: "hc", args: [runArgs(["tuckerbox", "run", "add-login"])], reply: ran("mini/t6", 0, "started") },
    { fn: "hc", args: [["rm", "--json", "mini/t6"]], reply: new Error("gone already") },
  ]);
  assert.deepEqual(await perform(invocation("start", remote)), { ok: true, message: "started" });
  finish();
});

test("on this computer, and for actions that return, it is still exec", async () => {
  begin([{ fn: "exec", args: ["tuckerbox", ["run", "add-login"], { cwd: "/work/shop", on: null, timeout: 120000 }], reply: { code: 0, stdout: "add-login: started", stderr: "" } }]);
  assert.deepEqual(await perform(invocation("start", local)), { ok: true, message: "add-login: started" });
  finish();
  begin([{ fn: "exec", args: ["tuckerbox", ["stop", "add-login"], { cwd: "/Users/sam/shop", on: "mini", timeout: 120000 }], reply: { code: 0, stdout: "stopped", stderr: "" } }]);
  assert.deepEqual(await perform(invocation("stop", remote)), { ok: true, message: "stopped" });
  finish();
});

test("detachedResult: a signal with no output names the signal", () => {
  const r = detachedResult(ran("mini/t3", null, "", { signal: "SIGKILL", reason: "killed" }));
  assert.deepEqual(r, { ok: false, message: "ended by signal SIGKILL", remove: "mini/t3" });
});
