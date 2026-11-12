// What each card offers, and the tuckerbox command line behind every action.
// No Herdr or gpui imports: node tests it (test/actions.test.js).
//
// The board never writes a file or run state itself (herdr-board: Actions go
// through Tuckerbox). Every action is a `tuckerbox` command on the card's
// machine; the two Herdr-only ones (Open proposal, Open worktree) open a view
// and change nothing.
import { isActive } from "./model.js";

export const ACTIONS = {
  start: { label: "▶ Start", primary: true },
  approve: { label: "Approve", primary: true },
  unapprove: { label: "Unapprove" },
  stop: { label: "Stop", danger: true },
  resume: { label: "Resume", primary: true },
  rerun: { label: "Re-run step" },
  follow: { label: "Follow log ↗" },
  proposal: { label: "Open proposal" },
  worktree: { label: "Open worktree" },
  edit: { label: "Edit" },
  validate: { label: "Validate" },
  tick: { label: "Tick" },
  untick: { label: "Untick" },
  priority: { label: "Move" },
  group: { label: "Run group…" },
};

// Files `tuckerbox edit` opens; a spec delta is `spec:<capability>`.
export const EDIT_FILES = ["proposal", "design", "model", "tasks"];

const TASK_NUMBER = /^\d+\.\d+$/;
const GROUP_NUMBER = /^\d+$/;
const CHANGE_ID = /^[a-z0-9][a-z0-9-]*$/;

// The tuckerbox command line for an action, as argv (no shell, so nothing is
// quoted). `extra` carries what the click adds: the step for rerun and follow,
// the file for edit, the task number for tick and untick. Returns null for the
// Herdr-only actions. Throws on input a command must never get.
export function commandLine(action, change, extra = {}) {
  if (!CHANGE_ID.test(change ?? "")) throw new Error(`not a change id: ${JSON.stringify(change)}`);
  const step = () => {
    if (!extra.step) throw new Error(`${action} needs a step`);
    return extra.step;
  };
  switch (action) {
    case "start": return ["tuckerbox", "run", change];
    case "group":
      if (!GROUP_NUMBER.test(String(extra.group ?? ""))) throw new Error("group needs a task group number like 2");
      return ["tuckerbox", "run", change, "--tasks", String(extra.group)];
    case "approve": return ["tuckerbox", "approve", change];
    case "unapprove": return ["tuckerbox", "unapprove", change];
    case "stop": return ["tuckerbox", "stop", change];
    case "resume": return ["tuckerbox", "resume", change];
    case "rerun": return ["tuckerbox", "rerun", change, step()];
    case "follow": return ["tuckerbox", "logs", change, step(), "--follow"];
    case "edit": {
      const f = extra.file ?? "";
      const ok = EDIT_FILES.includes(f) || /^spec:[a-z0-9][a-z0-9-]*$/.test(f);
      if (!ok) throw new Error(`edit needs proposal, design, model, tasks or spec:<capability>, not ${JSON.stringify(f)}`);
      return ["tuckerbox", "edit", change, f];
    }
    case "validate": return ["tuckerbox", "validate", change, "--json"];
    case "tick":
    case "untick":
      if (!TASK_NUMBER.test(extra.task ?? "")) throw new Error(`${action} needs a task number like 2.1`);
      return ["tuckerbox", "tasks", change, action, extra.task];
    case "priority": {
      // A drag to reorder (change-priority: Drag to reorder).
      const n = extra.position;
      if (!Number.isInteger(n) || n < 1) throw new Error(`priority needs a position from 1, not ${JSON.stringify(n)}`);
      return ["tuckerbox", "priority", change, String(n)];
    }
    case "proposal":
    case "worktree":
      return null;
    default:
      throw new Error(`unknown action ${JSON.stringify(action)}`);
  }
}

// Where an action runs and how it shows (design: Actions):
//   exec: run and show its output or error
//   pane: a Herdr pane beside the board running the command (edit)
//   tab:  a Herdr tab running the command (follow log)
//   view: a Herdr view, no command (open proposal, open worktree)
// `on` is the card's Herdr machine (null: this computer), `cwd` its checkout there.
export function invocation(action, card, extra = {}) {
  const argv = commandLine(action, card?.change, extra);
  const place = action === "edit" ? "pane" : action === "follow" ? "tab" : argv ? "exec" : "view";
  const out = { action, place, argv, change: card?.change ?? "", on: card?.on ?? null, cwd: card?.path || "" };
  if (action === "follow") out.title = `${card?.change} ${extra.step} log`;
  if (action === "edit") out.title = `edit ${card?.change} ${extra.file}`;
  if (action === "proposal") out.file = card?.edit_folder ? `${card.edit_folder}/openspec/changes/${card.change}/proposal.md` : "";
  if (action === "worktree") out.folder = card?.worktree ?? "";
  return out;
}

// The actions a card offers in its detail, in button order. `board` gives
// require_approval and the workflow's steps.
export function detailActions(card, board) {
  if (!card) return [];
  const out = [];
  const archived = card.column === "done" || Boolean(card.archived);
  if (archived) return out;
  const active = isActive(card);
  const hasRun = Boolean(card.state);
  const approvalOff = board?.require_approval === false;
  if (!hasRun && (card.column === "approved" || (approvalOff && card.column === "proposed"))) out.push("start");
  if (card.state === "stopped" || card.state === "blocked") out.push("resume");
  if (["stopped", "blocked", "failed", "ready"].includes(card.state) && rerunSteps(card, board).length) out.push("rerun");
  if (active) out.push("stop");
  if (card.step && ["running", "blocked", "stopped", "failed"].includes(card.state)) out.push("follow");
  if (!active && card.approval.state !== "approved") out.push("approve");
  if (!active && card.approval.state === "approved") out.push("unapprove");
  if (card.edit_folder) out.push("proposal");
  if (card.worktree) out.push("worktree");
  return out;
}

// Run group… (add-task-scope): the task groups a card's detail offers to
// run, each { number, title, ticked, total }. It is offered where Start is,
// and on Ready cards whose change still has unticked tasks, and lists only
// groups with unticked tasks (ship tasks are not counted in `groups`). Empty
// means no Run group….
export function runGroups(card, board, detail) {
  if (!card || !detail) return [];
  const where = detailActions(card, board).includes("start") || card.state === "ready";
  if (!where) return [];
  return (Array.isArray(detail.groups) ? detail.groups : [])
    .filter((g) => Number.isInteger(g?.number) && Number(g.ticked) < Number(g.total))
    .map((g) => ({ number: g.number, title: String(g.title ?? ""), ticked: Number(g.ticked), total: Number(g.total) }));
}

// The one or two buttons on the card itself: what moves it on.
export function cardActions(card, board) {
  const all = detailActions(card, board);
  const pick = ["start", "approve", "resume", "stop"].filter((a) => all.includes(a));
  if (!pick.length && all.includes("rerun") && card.state === "failed") pick.push("rerun");
  // Start not offered before approval: Approve is the way on (and only it).
  return pick.slice(0, 2);
}

// Edit, tick and untick: not while a run is queued or running (it holds the lock).
export function canEdit(card) {
  return Boolean(card) && !isActive(card) && card.column !== "done" && !card.archived;
}

// The step a Re-run or Follow log button means by default: the card's step,
// else (a ready run) the workflow's first step.
export function defaultStep(card, board) {
  return card?.step || rerunSteps(card, board)[0] || "";
}

export function rerunSteps(_card, board) {
  if (board?.steps?.length) return board.steps;
  return (board?.columns ?? []).filter((c) => c.kind === "step" && c.step).map((c) => c.step);
}

// The label an action shows, with its step or file where that helps.
export function actionLabel(action, extra = {}) {
  const base = ACTIONS[action]?.label ?? action;
  if (action === "rerun" && extra.step) return `Re-run ${extra.step}`;
  if (action === "follow" && extra.step) return `Follow ${extra.step} log ↗`;
  return base;
}

// The command line as one string for people (logs, the "not wired yet" note).
export function shown(argv) {
  return (argv ?? []).map((a) => (/^[\w@%+=:,./-]+$/.test(a) ? a : `'${a.replace(/'/g, "'\\''")}'`)).join(" ");
}

const machineOf = (on) => on || null;

// The Herdr command that shows a tab, pane or view action (6.4, 6.5):
// { command, args } for run(). `ctx` is what Herdr shows now: `shown`, the
// shown space ({ id, machine }); `spaces` and `panes`, the live lists; `mod`,
// this mod's name. A tab or pane on the shown space's machine opens there;
// on another machine it opens as a new space there, since tabs and splits
// stay on their space's machine.
export function herdrCall(inv, ctx = {}) {
  const shown = ctx.shown ?? null;
  const here = shown && machineOf(shown.machine) === machineOf(inv.on);
  const name = inv.title || inv.argv?.slice(1, 4).join(" ") || inv.action;
  const elsewhere = () => ({ command: "space.new", args: { name, machine: inv.on ?? "", folder: inv.cwd, argv: inv.argv } });
  switch (inv.place) {
    case "tab":
      return here ? { command: "tab.new", args: { name, space: shown.id, folder: inv.cwd, argv: inv.argv } } : elsewhere();
    case "pane":
      return here ? { command: "terminal.open", args: { argv: inv.argv, folder: inv.cwd, place: "right", name } } : elsewhere();
    case "view":
      if (inv.action === "proposal") {
        return { command: "pane.split", args: { direction: "right", mod: ctx.mod, props: { view: "proposal", change: inv.change, card: inv.card, board: inv.board } } };
      }
      if (inv.action === "worktree") {
        const open = (ctx.spaces ?? []).find((s) => machineOf(s.machine) === machineOf(inv.on) && spaceAt(s, inv.folder, ctx.panes));
        return open ? { command: "space.select", args: { id: open.id } }
          : { command: "space.new", args: { name: inv.change, folder: inv.folder, machine: inv.on ?? "" } };
      }
      break;
  }
  throw new Error(`${inv.action} is not a Herdr view, tab or pane`);
}

const norm = (p) => String(p ?? "").replace(/\\/g, "/").replace(/\/+$/, "");
const inside = (p, root) => Boolean(root) && (norm(p) === norm(root) || norm(p).startsWith(`${norm(root)}/`));

// A space is at a folder when it was made there or a terminal of it is there now.
function spaceAt(space, folder, panes = []) {
  if (!folder) return false;
  if (inside(space.folder, folder)) return true;
  const tabs = new Set((space.tabs ?? []).map((t) => t.id));
  return panes.some((p) => tabs.has(p.tab) && inside(p.cwd, folder));
}

// What a finished tuckerbox command means for the board: ok, and the
// message to show. A failure shows the command's own error (stderr first);
// a success shows its output, which carries notes such as "not committed".
export function outcome(r) {
  const out = String(r?.stdout ?? "").trim();
  const err = String(r?.stderr ?? "").trim();
  if ((r?.code ?? 1) === 0) return { ok: true, message: [out, err].filter(Boolean).join("\n") || "done" };
  return { ok: false, message: err || out || `exited with code ${r?.code}` };
}

// `tuckerbox validate --json`: it exits 1 for an invalid change and still
// prints the document, so the document decides. { valid, errors: [message] }
// or { error } when there is no document.
export function parseValidation(r) {
  let doc = null;
  try { doc = JSON.parse(String(r?.stdout ?? "")); } catch { doc = null; }
  if (!doc || typeof doc.valid !== "boolean") return { error: outcome(r).message };
  const errors = (Array.isArray(doc.errors) ? doc.errors : []).map((e) => (typeof e === "string" ? e : [e?.path, e?.message].filter(Boolean).join(": ")));
  return { valid: doc.valid, errors };
}

// ---- opening, docking, hiding (herdr-board: Opening the board, Docking) ----

// The board window, as mod.json's `overlay` describes it: what `mod.open`
// toggles (the toggle key, the drawer) and what Undock opens again.
export function windowSpec(mod) {
  return { mod, kind: "window", width: 0.86, height: 0.84, key: mod, keep: true };
}

// The toggle: the key tuckerbox herdr install --keys binds, and the drawer's
// click. `mod.open` opens the board window, or closes it when it is open.
export function toggleCall(mod) {
  return { command: "mod.open", args: { name: mod } };
}

// Dock: the board as a pane beside the terminals of the shown space, pinned
// to the repository it shows (`props.folder`, `props.machine`). It splits the
// focused pane when that is a terminal in that space, else the shown tab's
// first terminal, else it opens a tab with the board as its pane.
export function dockCall({ root, on, space, spaces, panes, focused, mod }) {
  if (!root) throw new Error("nothing to dock: no Tuckerbox repository here");
  const props = { folder: root, machine: on ?? null };
  const s = (spaces ?? []).find((x) => x.id === space) ?? null;
  const tabs = new Set((s?.tabs ?? []).map((t) => t.id));
  const active = new Set((s?.tabs ?? []).filter((t) => t.active).map((t) => t.id));
  const terms = (panes ?? []).filter((p) => tabs.has(p.tab) && p.terminal && !p.mod);
  const target = terms.find((p) => p.pane === focused?.pane) ?? terms.find((p) => active.has(p.tab)) ?? null;
  if (target) return { command: "pane.split", args: { direction: "right", pane: target.pane, mod, props } };
  return { command: "tab.new", args: { ...(s ? { space: s.id } : {}), name: "Tuckerbox", mod, props } };
}

// Undock: the board back in its window, then this pane closed. Close: only
// the pane. Both need this copy's own pane id.
export function undockCalls(pane, mod) {
  if (!pane) throw new Error("this board doesn't know its pane yet: click it first");
  return { overlay: windowSpec(mod), close: { command: "pane.close", args: { pane } } };
}
