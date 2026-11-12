// Running an action (6.4 to 6.6). Every action is a tuckerbox command on the
// card's machine (exec, or for a remote card's run a short-lived herdr-core
// terminal), or a Herdr tab, pane or view that runs one or shows
// something; the board writes nothing itself. With the dev fixtures the
// board's paths are made up, so actions only say what they would run.
import { exec, hc, run, spaces, panes, focused } from "herdr";
import { shown, herdrCall, outcome, parseValidation, invocation, detachCall, detachedResult } from "./actions.js";
import { startEdit, tick, markValidating, setValidation, keyCard } from "./edits.js";
import { MOD } from "./data.js";

// Bumped after an action that changes run state or files, so every board
// copy reads again at once (module state is shared by the copies).
let epoch = 0;
export const changed = () => epoch;
const bump = () => { epoch += 1; };

const ACTION_MS = 120000;

async function tuckerbox(inv) {
  try {
    const r = await exec("tuckerbox", inv.argv.slice(1), { cwd: inv.cwd || undefined, on: inv.on, timeout: ACTION_MS });
    return { code: r.code ?? 0, stdout: r.stdout ?? "", stderr: r.stderr ?? "" };
  } catch (e) {
    // exec rejects when tuckerbox can't start there or the machine is unreachable.
    return { code: -1, stdout: "", stderr: String(e?.message ?? e) };
  }
}

// Start, Run group, Resume and Re-run on another machine: in a short-lived
// herdr-core terminal, since exec there would end the run engine they leave
// running (actions.js: DETACHED). Resolves with { ok, message }.
async function detached(inv) {
  let r;
  try {
    const x = await hc(detachCall(inv));
    r = { code: x?.code ?? 0, stdout: x?.stdout ?? "", stderr: x?.stderr ?? "" };
  } catch (e) {
    return { ok: false, message: String(e?.message ?? e) };
  }
  const res = detachedResult(r);
  // The board has the output: the finished terminal goes. A failed removal
  // leaves a finished terminal behind, nothing worse.
  if (res.remove) hc(["rm", "--json", res.remove]).catch(() => {});
  return { ok: res.ok, message: res.message };
}

function shownSpace() {
  const f = focused();
  const all = spaces();
  const s = all.find((x) => x.id === f?.space) ?? all.find((x) => x.active) ?? null;
  return s ? { id: s.id, machine: s.machine ?? null } : null;
}

// Runs inv; resolves with { ok, message, dry }.
export async function perform(inv, opts = {}) {
  if (opts.dry) {
    const what = inv.argv ? shown(inv.argv) : `${inv.action} (a Herdr view)`;
    return { ok: false, dry: true, message: `Fixtures: would run ${what}${inv.on ? ` on ${inv.on}` : ""}` };
  }
  if(inv.place==="browser"){try{if(!opts.openURL)throw new Error("browser action needs a click context");opts.openURL(inv.url);return {ok:true,message:""};}catch(e){return {ok:false,message:String(e?.message??e)};}}
  if (inv.place === "exec") {
    const r = inv.detach ? await detached(inv) : outcome(await tuckerbox(inv));
    if (r.ok) bump();
    return r;
  }
  try {
    const at = shownSpace();
    const call = herdrCall(inv, { shown: at, spaces: spaces(), panes: panes(), mod: MOD });
    // The editor's pane opens in the shown space (terminal.open) or in a new
    // space on the card's machine (space.new), in the card's checkout.
    if (inv.place === "pane") {
      startEdit(inv.card, inv.argv[3], panes(), Date.now(), { on: inv.on, space: call.command === "terminal.open" ? at?.id ?? null : null, folder: inv.cwd });
    }
    run(call.command, call.args);
    if (inv.place === "pane") return { ok: true, message: `Editing ${inv.argv[3]} in a pane: it validates when the pane closes` };
    if (inv.place === "tab") return { ok: true, message: `Following ${inv.argv[3]} in a tab` };
    return { ok: true, message: "" };
  } catch (e) {
    return { ok: false, message: String(e?.message ?? e) };
  }
}

// Validates a change (after an edit, or the Validate button) and keeps the
// result in memory for its card.
export async function validate(card) {
  markValidating(card);
  const inv = invocation("validate", card);
  const v = parseValidation(await tuckerbox(inv));
  setValidation(card, v);
  bump();
  return v;
}

// Called from every copy's timer: moves the edit records on and validates
// the ones whose pane closed. Idempotent across copies.
export function watchEdits() {
  return tick(panes(), Date.now(), spaces()).map((key) => validate(keyCard(key)));
}
