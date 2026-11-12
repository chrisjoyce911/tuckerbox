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
  init: { label: "tuckerbox init", primary: true },
};

// Files `tuckerbox edit` opens; a spec delta is `spec:<capability>`.
export const EDIT_FILES = ["proposal", "design", "model", "tasks"];

const TASK_NUMBER = /^\d+\.\d+$/;
const CHANGE_ID = /^[a-z0-9][a-z0-9-]*$/;

// The tuckerbox command line for an action, as argv (no shell, so nothing is
// quoted). `extra` carries what the click adds: the step for rerun and follow,
// the file for edit, the task number for tick and untick. Returns null for the
// Herdr-only actions. Throws on input a command must never get.
export function commandLine(action, change, extra = {}) {
  if (action === "init") return ["tuckerbox", "init"];
  if (!CHANGE_ID.test(change ?? "")) throw new Error(`not a change id: ${JSON.stringify(change)}`);
  const step = () => {
    if (!extra.step) throw new Error(`${action} needs a step`);
    return extra.step;
  };
  switch (action) {
    case "start": return ["tuckerbox", "run", change];
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
  const out = { action, place, argv, on: card?.on ?? null, cwd: card?.path || "" };
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
