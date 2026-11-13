// What each card offers, and the tuckerbox command line behind every action.
// No Herdr or gpui imports: node tests it (test/actions.test.js).
//
// The board never writes a file or run state itself (herdr-board: Actions go
// through Tuckerbox). Every action is a `tuckerbox` command on the card's
// machine; the Herdr-only ones (Open proposal, Open worktree, Watch) open or
// focus a view and change nothing.
import { isActive, answerable, boardMode, claimText } from "./model.js";

export const ACTIONS = {
  mr: {label:"Open merge request ↗"},
  start: { label: "▶ Start", primary: true },
  approve: { label: "Approve", primary: true },
  unapprove: { label: "Unapprove" },
  stop: { label: "Stop", danger: true },
  resume: { label: "Resume", primary: true },
  rerun: { label: "Re-run step" },
  follow: { label: "Follow log ↗" },
  watch: { label: "Watch ↗", primary: true },
  answer: { label: "Answer" },
  answer_resume: { label: "Answer and resume", primary: true },
  proposal: { label: "Open proposal" },
  worktree: { label: "Open worktree" },
  edit: { label: "Edit" },
  validate: { label: "Validate" },
  tick: { label: "Tick" },
  untick: { label: "Untick" },
  priority: { label: "Move" },
  group: { label: "Run group…" },
  set: { label: "Set" },
  // modes-board: Dispatch in the header, Take over on expired cards.
  dispatch: { label: "Dispatch", primary: true },
  takeover: { label: "Take over", danger: true },
};

// Files `tuckerbox edit` opens; a spec delta is `spec:<capability>`.
export const EDIT_FILES = ["proposal", "design", "model", "tasks"];

const TASK_NUMBER = /^\d+\.\d+$/;
const GROUP_NUMBER = /^\d+$/;
const CHANGE_ID = /^[a-z0-9][a-z0-9-]*$/;
const STEP_ID = /^[A-Za-z0-9][A-Za-z0-9_-]*$/;
const SET_KEYS = ["agent", "model", "effort", "permissions"];
// A picked value: never empty, never an option, no spaces or control characters.
const SET_VALUE = /^[^\s=,-][^\s=,]*$/;

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
    // modes-board: Take over from the board (an expired fleet claim).
    case "takeover": return ["tuckerbox", "run", "--take-over", change];
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
      // A drag to reorder (herdr-board: Drag to reorder).
      const n = extra.position;
      if (!Number.isInteger(n) || n < 1) throw new Error(`priority needs a position from 1, not ${JSON.stringify(n)}`);
      return ["tuckerbox", "priority", change, String(n)];
    }
    // blocked-answers: the text goes on standard input ("-"), so quoting and
    // newlines are safe; see answerText and stdinCall.
    case "answer": answerText(extra.text); return ["tuckerbox", "answer", change, "-"];
    case "answer_resume": answerText(extra.text); return ["tuckerbox", "answer", change, "--resume", "-"];
    case "set": {
      // A settings picker (settings-board: Settings pickers); value null
      // is "repository default".
      if (!STEP_ID.test(extra.step ?? "")) throw new Error(`set needs a step id, not ${JSON.stringify(extra.step)}`);
      if (!SET_KEYS.includes(extra.key)) throw new Error(`set needs agent, model, effort or permissions, not ${JSON.stringify(extra.key)}`);
      const key = `step.${extra.step}.${extra.key}`;
      if (extra.value == null) return ["tuckerbox", "set", change, "--unset", key];
      if (!SET_VALUE.test(String(extra.value)) || /[\u0000-\u001f]/.test(String(extra.value))) throw new Error(`not a value to set: ${JSON.stringify(extra.value)}`);
      return ["tuckerbox", "set", change, `${key}=${extra.value}`];
    }
    case "mr":
    case "proposal":
    case "worktree":
    case "watch":
      return null;
    default:
      throw new Error(`unknown action ${JSON.stringify(action)}`);
  }
}

// The actions that start a run engine which goes on after the command
// returns (tuckerbox run, resume, rerun). For a card on another machine,
// exec goes through herdr-core, which ends every process it started when the
// command returns, the detached engine too; so there these run in a
// short-lived herdr-core terminal instead (detachCall). The app's own exec on
// this computer leaves a detached child alone, so local cards keep exec. The
// board shows their output or error either way. Answer and resume
// (tuckerbox answer --resume) restarts the engine too, so it goes the same way.
export const DETACHED = new Set(["start", "group", "resume", "rerun", "answer_resume", "takeover", "dispatch"]);

// Where an action runs and how it shows (design: Actions):
//   exec: run and show its output or error (`detach`: a remote card's run,
//         in a short-lived herdr-core terminal so the engine keeps going)
//   pane: a Herdr pane beside the board running the command (edit)
//   tab:  a Herdr tab running the command (follow log)
//   view: a Herdr view, no command (open proposal, open worktree)
// `on` is the card's Herdr machine (null: this computer), `cwd` its checkout there.
export function invocation(action, card, extra = {}) {
  const argv = commandLine(action, card?.change, extra);
  const place = action === "mr" ? "browser" : action === "edit" ? "pane" : action === "follow" ? "tab" : argv ? "exec" : "view";
  const out = { action, place, argv, change: card?.change ?? "", on: card?.on ?? null, cwd: card?.path || "" };
  if (DETACHED.has(action) && out.on) out.detach = true;
  if(action==="mr"){const link=card?.mr?.link??"";if(!/^https:\/\/[^/?#\s]+(?:[/?#]|$)/.test(link) || link.split("/")[2].includes("@"))throw new Error("merge request needs an HTTPS link");out.url=link;}
  if (action === "answer" || action === "answer_resume") {
    out.stdin = answerText(extra.text);
    out.os = card?.os ?? "";
  }
  if (action === "follow") out.title = `${card?.change} ${extra.step} log`;
  if (action === "edit") out.title = `edit ${card?.change} ${extra.file}`;
  if (action === "proposal") out.file = card?.edit_folder ? `${card.edit_folder}/openspec/changes/${card.change}/proposal.md` : "";
  if (action === "worktree") out.folder = card?.worktree ?? "";
  if (action === "watch") {
    const addr = card?.activity?.terminal?.address ?? "";
    if (!/^[A-Za-z0-9._-]+$/.test(addr)) throw new Error("Watch needs the agent's terminal address");
    out.terminal = addr;
  }
  return out;
}

// The most an answer may be: tuckerbox answer refuses more (16 KiB).
export const MAX_ANSWER = 16 * 1024;

// An answer's text, trimmed as tuckerbox answer trims it. Throws on an
// empty one, or one over MAX_ANSWER bytes of UTF-8.
export function answerText(text) {
  const t = String(text ?? "").trim();
  if (!t) throw new Error("Type an answer first.");
  const bytes = typeof TextEncoder === "function" ? new TextEncoder().encode(t).length : t.length;
  if (bytes > MAX_ANSWER) throw new Error(`The answer is ${bytes} bytes; tuckerbox takes up to ${MAX_ANSWER} (16 KiB).`);
  return t;
}

// The answer actions a card's detail offers under its question: Answer on
// blocked and waiting cards, and Answer and resume on blocked ones.
export function answerActions(card) {
  if (!answerable(card)) return [];
  return card.state === "blocked" ? ["answer_resume", "answer"] : ["answer"];
}

// Herdr's exec gives a program no standard input, so a command that reads
// its input from "-" runs under a shell of the card's machine, which pipes
// the text in. `os` is that machine's operating system (the board
// document's os): Windows has no sh, so there it runs under PowerShell.
export function stdinCall(argv, text, os = "") {
  return os === "windows" ? powershellCall(argv, text) : shCall(argv, text);
}

// sh: the text is sh's $1, an argument it never parses: nothing in it is
// quoted or run.
export const STDIN_SCRIPT = 'text=$1; shift; printf \'%s\' "$text" | "$@"';
function shCall(argv, text) {
  return { command: "sh", args: ["-c", STDIN_SCRIPT, "sh", String(text), ...argv] };
}

// PowerShell (powershell.exe, on every Windows): the text goes in the
// script as base64 of its UTF-8, which has no quote, space or $ for
// PowerShell or Windows' command line to read, and the script pipes it,
// as UTF-8, into the command. The script has no double quote, which
// Windows' command line would take apart. 16 KiB of text is about 22,000
// characters of base64, inside Windows' 32,767-character command line.
// Windows PowerShell's .NET opens the command's standard input with
// [Console]::InputEncoding, which on a UTF-8 console (code page 65001)
// writes a byte order mark first; so that is set to UTF-8 without one too
// (where there is no console to set, there is no mark to stop).
export function powershellCall(argv, text) {
  const b64 = base64(utf8(String(text)));
  const lit = (a) => `'${String(a).replace(/'/g, "''")}'`;
  const script = [
    "$OutputEncoding = New-Object System.Text.UTF8Encoding $false",
    "try { [Console]::InputEncoding = $OutputEncoding } catch { }",
    `$t = [System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String('${b64}'))`,
    `$t | & ${argv.map(lit).join(" ")}`,
    "exit $LASTEXITCODE",
  ].join("; ");
  return { command: "powershell", args: ["-NoProfile", "-NonInteractive", "-Command", script] };
}

function utf8(s) {
  if (typeof TextEncoder === "function") return new TextEncoder().encode(s);
  const out = [];
  for (const ch of s) {
    let c = ch.codePointAt(0);
    if (c < 0x80) out.push(c);
    else if (c < 0x800) out.push(0xc0 | (c >> 6), 0x80 | (c & 63));
    else if (c < 0x10000) out.push(0xe0 | (c >> 12), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
    else out.push(0xf0 | (c >> 18), 0x80 | ((c >> 12) & 63), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
  }
  return out;
}

const B64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
function base64(bytes) {
  let out = "";
  for (let i = 0; i < bytes.length; i += 3) {
    const n = (bytes[i] << 16) | ((bytes[i + 1] ?? 0) << 8) | (bytes[i + 2] ?? 0);
    out += B64[(n >> 18) & 63] + B64[(n >> 12) & 63];
    out += i + 1 < bytes.length ? B64[(n >> 6) & 63] : "=";
    out += i + 2 < bytes.length ? B64[n & 63] : "=";
  }
  return out;
}

// agent-terminals: Watch from the board. A running card whose agent works in
// a herdr-core terminal offers Watch; one running headless keeps Follow log.
export function watchable(card) {
  return card?.state === "running" && card?.activity?.backend === "terminal" && Boolean(card.activity.terminal?.address);
}

// The actions a card offers in its detail, in button order. `board` gives
// require_approval and the workflow's steps.
export function detailActions(card, board) {
  if (!card) return [];
  const out = [];
  if(card.mr?.link)out.push("mr");
  const archived = card.column === "done" || Boolean(card.archived);
  if (archived) return out;
  const active = isActive(card);
  const hasRun = Boolean(card.state);
  // In fleet and team mode a claimed change is someone's: no Start
  // (tuckerbox run would refuse it); an expired fleet claim offers Take over.
  const claimed = boardMode(board) !== "solo" && Boolean(card.claim);
  if (!hasRun && !claimed && startable(card, board)) out.push("start");
  if (takeoverOffered(card, board)) out.push("takeover");
  if (card.state === "stopped" || card.state === "blocked" || (card.state==="ready" && board?.ship==="mr" && card.mr?.state==="open")) out.push("resume");
  if (["stopped", "blocked", "failed", "ready"].includes(card.state) && rerunSteps(card, board).length) out.push("rerun");
  if (active) out.push("stop");
  if (watchable(card)) out.push("watch");
  else if (card.step && ["running", "blocked", "stopped", "failed"].includes(card.state)) out.push("follow");
  if (!active && card.approval.state !== "approved") out.push("approve");
  if (!active && card.approval.state === "approved") out.push("unapprove");
  if (card.edit_folder) out.push("proposal");
  if (card.worktree) out.push("worktree");
  return out;
}

// A card Start would start: Approved, or Proposed with approval off.
function startable(card, board) {
  return card.column === "approved" || (board?.require_approval === false && card.column === "proposed");
}

// modes-board: Take over from the board. In fleet mode, a card whose claim
// expired offers Take over.
export function takeoverOffered(card, board) {
  return boardMode(board) === "fleet" && card?.claim?.state === "expired" && card.column !== "done" && !card.archived;
}

// modes-board: Dispatch from the board. In fleet mode the header offers
// Dispatch while at least one Approved card has no claim.
export function dispatchOffered(board) {
  if (!board || board.error || boardMode(board) !== "fleet") return false;
  return (board.cards ?? []).some((c) => !c.claim && !c.state && !c.archived && startable(c, board));
}

// Dispatch runs `tuckerbox dispatch` on the board's own machine, in its
// checkout (`at`: { cwd, on }). It starts runs, so on another machine it goes
// through a short-lived herdr-core terminal, as Start does.
export function dispatchInvocation(at = {}) {
  const on = at.on ?? null;
  const out = { action: "dispatch", place: "exec", argv: ["tuckerbox", "dispatch"], change: "", on, cwd: at.cwd || "" };
  if (on) out.detach = true;
  return out;
}

// What the person confirms before an action, or "" for none. Take over
// leaves the old machine's work behind (fleet-claims: Taking over).
export function confirmText(action, card) {
  if (action !== "takeover") return "";
  const from = card?.claim?.machine || "the other machine";
  return `Take over ${card?.change} from ${from}? Its claim expired. Work on ${from} (its worktree and anything not pushed) is left behind there; this machine starts the change again from what is on the remote.`;
}

// The claim line on a card: "expired on mini", "sam on studio".
export function claimLine(card, board) {
  if (boardMode(board) === "solo" || !card?.claim) return "";
  return claimText(card.claim);
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
  const pick = ["start", "approve", "takeover", "resume", "watch", "stop"].filter((a) => all.includes(a));
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
      // Watch: Herdr focuses the agent's terminal and opens its tab (from its
      // herdr.space and herdr.tab labels) if it isn't showing. A terminal on
      // another machine is addressed as <machine>/<address>.
      if (inv.action === "watch") return { command: "agent.focus", args: { terminal: inv.on ? `${inv.on}/${inv.terminal}` : inv.terminal } };
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

// The `hc` arguments for a detached action: run the command in a terminal on
// the card's machine, in its checkout, wait for it to return (tuckerbox run
// returns once the engine is started) and print the result as JSON. Not
// --rm: the terminal stays until the board has read its output, then
// detachedResult says to remove it. --outlive-parent and --no-parent: the
// terminal, and so the engine, belongs to nobody that could end it.
export const DETACH_WAIT_S = 110;
export function detachCall(inv) {
  if (!inv?.detach || !Array.isArray(inv.argv) || !inv.argv.length) throw new Error(`${inv?.action} does not run detached`);
  // An answer's text goes on standard input, through the machine's shell
  // (stdinCall), as it does under exec.
  const call = inv.stdin != null ? stdinCall(inv.argv, inv.stdin, inv.os) : null;
  return [
    "run", "--json", "--no-parent", "--no-notify", "--outlive-parent", "--timeout", String(DETACH_WAIT_S),
    ...(inv.on ? ["--on", inv.on] : []),
    ...(inv.cwd ? ["--cwd", inv.cwd] : []),
    "--", ...(call ? [call.command, ...call.args] : inv.argv),
  ];
}

// What `hc run --json` (an { code, stdout, stderr } from hc()) means for the
// board: { ok, message, remove }. `remove` is the finished terminal to drop
// with `hc rm` (null when there is none, or it is still running). A terminal
// that could not start gives hc's error; one that ended gives the command's
// own exit code and output, as outcome() shows them.
export function detachedResult(r) {
  // hc prints its JSON on stdout, and an error of its own on stderr.
  const text = String(r?.stdout ?? "").trim();
  const err = String(r?.stderr ?? "").trim();
  let doc = null;
  for (const t of [text, err]) {
    try { doc = JSON.parse(t); } catch { doc = null; }
    if (doc && typeof doc === "object") break;
    doc = null;
  }
  if (doc?.error) {
    const e = doc.error;
    return { ok: false, message: [e.message || e.kind || "hc run failed", e.hint ? `(${e.hint})` : ""].filter(Boolean).join(" "), remove: null };
  }
  if (!doc || typeof doc.address !== "string") {
    return { ok: false, message: err || text || `hc run exited with code ${r?.code}`, remove: null };
  }
  const w = doc.wait ?? {};
  if (!w.ended) {
    return { ok: true, message: `still starting in terminal ${doc.address}`, remove: null };
  }
  const code = Number.isInteger(w.exit_code) ? w.exit_code : (w.signal ? 128 : 1);
  const res = outcome({ code, stdout: code === 0 ? doc.output : "", stderr: code === 0 ? "" : doc.output });
  if (code !== 0 && !String(doc.output ?? "").trim() && w.signal) res.message = `ended by signal ${w.signal}`;
  return { ...res, remove: doc.address };
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
