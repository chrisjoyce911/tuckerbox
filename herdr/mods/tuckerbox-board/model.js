// The board's data rules, with no Herdr or gpui imports, so node can test them
// (test/model.test.js). The board document comes from `tuckerbox board --json`;
// README.md lists every field read here.

export const POLL_SHOWN_MS = 5000;
export const POLL_HIDDEN_MS = 30000;

const str = (v) => (typeof v === "string" ? v : "");
const num = (v) => (Number.isFinite(v) ? v : 0);
const list = (v) => (Array.isArray(v) ? v : []);

// A run is queued or running: a process holds the change's run lock, so edits,
// ticks and approval changes are refused (change-edits).
export const ACTIVE_STATES = new Set(["queued", "running"]);
// States that need the person: the card is flagged and counted in the header.
export const NEEDS_YOU_STATES = new Set(["blocked", "failed"]);

export function isActive(card) {
  return ACTIVE_STATES.has(card?.state);
}

export function needsYou(card) {
  return NEEDS_YOU_STATES.has(card?.state);
}

// One card with every field the mod reads, missing ones filled in. Missing,
// null and "" mean the same. `src` says where the card came from: the Herdr
// machine (`on`, null for this computer) and the checkout path there.
export function normaliseCard(c, src = {}) {
  const a = c?.approval ?? {};
  const t = c?.tasks ?? {};
  return {
    change: str(c?.change),
    title: str(c?.title) || str(c?.change).replace(/-/g, " "),
    column: str(c?.column),
    tasks: { done: num(t.done), total: num(t.total) },
    approval: {
      state: ["approved", "stale", "none"].includes(a.state) ? a.state : "none",
      by: str(a.by),
      at: str(a.at),
      committed: a.committed !== false,
    },
    state: str(c?.state),
    reason: str(c?.reason),
    badge: str(c?.badge),
    step: str(c?.step),
    agent: str(c?.agent),
    round: num(c?.round),
    since: str(c?.since),
    branch: str(c?.branch),
    worktree: str(c?.worktree),
    machine: str(c?.machine),
    edit_folder: str(c?.edit_folder),
    depends_on: list(c?.depends_on).map((d) => ({ change: str(d?.change), column: str(d?.column) })),
    archived: str(c?.archived),
    // The run's task scope (add-task-scope), or null for the whole change.
    scope: c?.scope && str(c.scope.spec) ? { spec: str(c.scope.spec), tasks: list(c.scope.tasks).map(str) } : null,
    on: src.on ?? null,
    path: src.path ?? "",
  };
}

// The board document, checked and filled in. An error document
// ({ error: { code, message } }) becomes { error }.
export function normaliseBoard(doc, src = {}) {
  if (!doc || typeof doc !== "object") return { error: { code: "bad_output", message: "tuckerbox board printed no JSON document" } };
  if (doc.error) return { error: { code: str(doc.error.code) || "error", message: str(doc.error.message) || String(doc.error) } };
  const path = str(doc.path);
  const s = { on: src.on ?? null, path };
  return {
    version: num(doc.version),
    repo: str(doc.repo) || path,
    path,
    machine: str(doc.machine),
    require_approval: doc.require_approval !== false,
    steps: list(doc.steps).map(str),
    columns: list(doc.columns).map((c) => ({ id: str(c?.id), kind: str(c?.kind), step: str(c?.step), name: str(c?.name) || str(c?.id) })),
    cards: list(doc.cards).map((c) => normaliseCard(c, s)),
    detail: doc.detail ? normaliseDetail(doc.detail) : null,
    on: s.on,
  };
}

// Parses what `tuckerbox board --json` printed. Exit code non-zero with a JSON
// error document is an error the board shows; anything else unparsable too.
export function parseBoard(stdout, stderr = "", code = 0, src = {}) {
  let doc = null;
  try { doc = JSON.parse(stdout); } catch { doc = null; }
  if (doc && doc.error) return normaliseBoard(doc, src);
  if (code !== 0 || !doc) {
    const message = (str(stderr).trim() || str(stdout).trim() || `tuckerbox board exited with code ${code}`).split("\n").slice(0, 6).join("\n");
    return { error: { code: "failed", message } };
  }
  return normaliseBoard(doc, src);
}

export function normaliseDetail(d) {
  return {
    change: str(d?.change),
    proposal: str(d?.proposal),
    specs: list(d?.specs).map((s) => ({ capability: str(s?.capability), text: str(s?.text) })),
    tasks: list(d?.tasks).map((t) => ({ number: str(t?.number), group: str(t?.group), text: str(t?.text), checked: t?.checked === true })),
    rounds: list(d?.rounds).map((r) => ({
      round: num(r?.round), result: str(r?.result), step: str(r?.step), summary: str(r?.summary),
      findings: list(r?.findings).map(finding), ended: str(r?.ended), scope: str(r?.scope),
    })),
    history: list(d?.history).map((h) => ({
      at: str(h?.at), event: str(h?.event), step: str(h?.step), round: num(h?.round), agent: str(h?.agent),
      machine: str(h?.machine), result: str(h?.result), to: str(h?.to), summary: str(h?.summary),
      reason: str(h?.reason), findings: list(h?.findings).map(finding),
      scope: str(h?.scope), old_scope: str(h?.old_scope),
    })),
    // Task groups with ticked and total counts, ship tasks left out (add-task-scope).
    groups: list(d?.groups).map((g) => ({ number: num(g?.number), title: str(g?.title), ticked: num(g?.ticked), total: num(g?.total) })),
  };
}

const finding = (f) => ({ severity: str(f?.severity), file: str(f?.file), detail: str(f?.detail) });

// Cards by column, in the document's column order. A card whose column the
// document doesn't list gets a column of its own at the end, so it is never lost.
export function group(board) {
  const cols = board.columns.map((c) => ({ ...c, cards: [] }));
  const byId = new Map(cols.map((c) => [c.id, c]));
  for (const card of board.cards) {
    let col = byId.get(card.column);
    if (!col) {
      col = { id: card.column, kind: "other", step: "", name: card.column || "No column", cards: [] };
      byId.set(card.column, col);
      cols.push(col);
    }
    col.cards.push(card);
  }
  return cols;
}

export function counts(board) {
  const cards = board?.cards ?? [];
  return {
    running: cards.filter((c) => c.state === "running").length,
    needs: cards.filter(needsYou).length,
    total: cards.length,
  };
}

export function columnName(board, id) {
  if (!id) return "unknown";
  return board?.columns?.find((c) => c.id === id)?.name ?? id;
}

// The approval badge: the word and the theme color name it is drawn in.
export function approvalBadge(approval) {
  const a = approval ?? { state: "none" };
  if (a.state === "approved") return { word: "approved", tone: "working", mark: "✓" };
  if (a.state === "stale") return { word: "stale approval", tone: "blocked", mark: "!" };
  return { word: "not approved", tone: "dim", mark: "○" };
}

// The run's state in words, and its theme color name.
export function stateLook(card) {
  switch (card?.state) {
    case "running": return { word: "running", tone: "working" };
    case "queued": return { word: "queued", tone: "dim" };
    case "blocked": return { word: "blocked", tone: "blocked" };
    case "failed": return { word: "failed", tone: "blocked" };
    case "stopped": return { word: "stopped", tone: "muted" };
    case "ready": return { word: "ready", tone: "done" };
    default: return null;
  }
}

export const EVENT_WORDS = {
  started: "Run started",
  queued: "Queued",
  step_started: "Step started",
  step_finished: "Step finished",
  handoff: "Handed on",
  rejected: "Rejected",
  blocked: "Blocked",
  stopped: "Stopped",
  resumed: "Resumed",
  failed: "Failed",
  ready: "Ready",
  scope_started: "Started again",
};

// One history line in words: "Handed on build → review: summary".
export function eventText(h) {
  const head = EVENT_WORDS[h.event] ?? h.event;
  const step = h.step ? (h.event === "handoff" && h.to ? ` ${h.step} → ${h.to}` : ` ${h.step}`) : "";
  const tail = h.event === "step_finished" && h.result ? ` (${h.result})`
    : h.event === "rejected" && h.findings.length ? ` with ${h.findings.length} finding${h.findings.length === 1 ? "" : "s"}`
      : "";
  const note = h.summary || h.reason;
  const scope = h.event === "scope_started" ? ` for tasks ${h.scope || "all"} (was ${h.old_scope || "all"})`
    : h.scope ? ` (tasks ${h.scope})` : "";
  return `${head}${step}${tail}${scope}${note ? `: ${note}` : ""}`;
}

// History newest first. Events with the same time keep their reverse file order.
export function newestFirst(history) {
  return list(history).map((h, i) => ({ h, i, t: Date.parse(h.at) || 0 }))
    .sort((a, b) => b.t - a.t || b.i - a.i).map((x) => x.h);
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const pad = (n) => String(n).padStart(2, "0");

// A time in this computer's time zone: "09:15" today, else "12 Nov 09:15",
// with the year when it isn't this year's.
export function fmtLocal(iso, now = new Date()) {
  const t = Date.parse(iso);
  if (!iso || Number.isNaN(t)) return "";
  const d = new Date(t);
  const hm = `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  if (d.getFullYear() === now.getFullYear() && d.getMonth() === now.getMonth() && d.getDate() === now.getDate()) return hm;
  const day = `${d.getDate()} ${MONTHS[d.getMonth()]}`;
  return d.getFullYear() === now.getFullYear() ? `${day} ${hm}` : `${day} ${d.getFullYear()} ${hm}`;
}

export function ago(iso, now = Date.now()) {
  const t = Date.parse(iso);
  if (!iso || Number.isNaN(t)) return "";
  const s = Math.max(0, Math.round((now - t) / 1000));
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}

// Tasks under their tasks.md headings, in order.
export function taskGroups(tasks) {
  const out = [];
  for (const t of list(tasks)) {
    const last = out[out.length - 1];
    if (last && last.group === t.group) last.tasks.push(t);
    else out.push({ group: t.group, tasks: [t] });
  }
  return out;
}

export function progress(tasks) {
  const total = num(tasks?.total);
  const done = num(tasks?.done);
  return { done, total, ratio: total > 0 ? Math.min(1, done / total) : 0 };
}

// Polling (design: 5 s while shown, 30 s while hidden).
export function pollInterval(shown) {
  return shown ? POLL_SHOWN_MS : POLL_HIDDEN_MS;
}

// Whether a read is due: never read yet, or the interval has passed.
export function due(last, now, shown) {
  return !last || now - last >= pollInterval(shown);
}

// Which pane is this copy's: Herdr's `pane` prop when it gives one, else the
// one learned when this copy had the keyboard (BoardView), else the only
// board pane there is. Null when it can't tell (none, or several).
export function selfPane({ self, panes, mod }) {
  if (self) return self;
  const mine = list(panes).filter((p) => p.mod === mod);
  return mine.length === 1 ? mine[0].pane : null;
}

// The space and tab holding a pane, or null.
export function paneHome(paneId, { spaces, panes }) {
  const p = list(panes).find((x) => x.pane === paneId);
  if (!p) return null;
  for (const s of list(spaces)) {
    const tab = list(s.tabs).find((t) => t.id === p.tab);
    if (tab) return { space: s, tab, pane: p };
  }
  return null;
}

// The space a board belongs to: its `space` prop, else the space holding its
// own pane, else (a window, a layout block) the shown space. A `space` prop
// naming a space that is gone gives null: the board must not drift to another.
export function boardSpace({ props, spaces, panes, self }) {
  const all = list(spaces);
  if (props?.space) return all.find((s) => s.id === props.space) ?? null;
  const home = self ? paneHome(self, { spaces, panes }) : null;
  if (home) return home.space;
  return all.find((s) => s.active) ?? null;
}

// The folders to look in for the board's repository, in order (herdr-board:
// Which repository): a `folder` prop alone (a docked pane); else the focused
// pane's live folder when that pane is in the board's own space; else every
// other pane of that space with a folder, the shown tab's first; else the
// space's folder. `on` is the `machine` prop (a docked pane), else that
// space's machine, so folders and the machine always come from the same space.
export function repoCandidates({ props, spaces, panes, focused, self }) {
  const p = props ?? {};
  const space = boardSpace({ props: p, spaces, panes, self });
  const on = p.machine ?? p.on ?? space?.machine ?? null;
  const id = space?.id ?? null;
  if (typeof p.folder === "string" && p.folder) return { cwds: [p.folder], on, space: id, pinned: true };
  if (!space) return { cwds: [], on, space: null, pinned: false };
  const tabs = list(space.tabs);
  const ids = new Set(tabs.map((t) => t.id));
  const active = new Set(tabs.filter((t) => t.active).map((t) => t.id));
  const inSpace = list(panes).filter((x) => ids.has(x.tab) && x.cwd);
  const ordered = [
    ...inSpace.filter((x) => x.pane === focused?.pane),
    ...inSpace.filter((x) => x.pane !== focused?.pane && active.has(x.tab)),
    ...inSpace.filter((x) => x.pane !== focused?.pane && !active.has(x.tab)),
  ].map((x) => x.cwd);
  if (space.folder) ordered.push(space.folder);
  return { cwds: [...new Set(ordered)], on, space: id, pinned: false };
}

// The repository from what each candidate folder turned out to be: `probes`
// lines up with `cands.cwds`, each { root, config } (root: the git checkout
// the folder is in, config: it has .tuckerbox/config.toml) or null when it
// couldn't be looked at. The first folder in a repository with a Tuckerbox
// config wins; a repository without one never counts. Without one,
// found is false and the board, drawer and Dock show nothing for this space.
export function pickRepo(cands, probes) {
  const i = list(cands?.cwds).findIndex((_c, k) => probes?.[k]?.config && probes[k].root);
  const base = { on: cands?.on ?? null, space: cands?.space ?? null };
  if (i < 0) return { ...base, found: false, root: "", cwd: "" };
  return { ...base, found: true, root: probes[i].root, cwd: probes[i].root };
}

// A key that changes when the candidate folders do: cheap to compute every
// second, so a board or drawer follows a space switch without waiting a poll.
export function candidatesKey(c) {
  return JSON.stringify([c?.space ?? null, c?.on ?? null, list(c?.cwds)]);
}

// What a board window shows after locating (herdr-board: Opening the board):
// an open from a key, the drawer or mod.open in a space with no Tuckerbox
// config closes at once ("close": the window goes, a short toast says why);
// once it has shown a board, a later space without one shows "message".
export function openGate({ overlay, everFound, found }) {
  if (found) return "board";
  return overlay && !everFound ? "close" : "message";
}

export const NO_CONFIG = "No Tuckerbox config in this space";

// Whether this copy is on screen. A window or dialog is shown while open. A
// pane is shown when its space and its tab are the shown ones, and no other
// pane of that tab is zoomed over it. Herdr keeps hidden copies running, so
// this decides between the shown and hidden poll intervals. A copy that can't
// tell which pane is its own (several board panes, none known) counts as
// shown if any board pane is, until it learns its pane.
export function isShown({ overlay, self, spaces, panes, mod }) {
  if (overlay) return true;
  const visible = (id) => {
    const home = paneHome(id, { spaces, panes });
    if (!home) return null;
    return home.space.active && home.tab.active && (!home.tab.zoomed || home.pane.zoomed);
  };
  const me = selfPane({ self, panes, mod });
  if (me) return visible(me) ?? true;
  const mine = list(panes).filter((p) => p.mod === mod);
  if (!mine.length) return true; // not a pane: a layout block
  return mine.some((p) => visible(p.pane));
}

// The folder of `file` from a stack trace line such as
// "at f (/home/me/mods/tuckerbox-board/data.js?v=3:12:9)". Null if not found.
export function dirFromStack(stack, file) {
  const name = file.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const m = new RegExp(`(?:\\(|at |^)\\s*((?:file://)?[^()\\n]*?)[\\\\/]${name}(?:\\?[^:)\\s]*)?:\\d+:\\d+`, "m").exec(String(stack));
  if (!m) return null;
  return m[1].replace(/^file:\/\//, "").trim() || null;
}

// The drawer reads the counts this often (herdr-board: The drawer).
export const DRAWER_POLL_MS = 30000;

// The drawer's rows, edge side first: the repository, then running and
// needs-you counts (each only when there are some), or the read's error.
export function drawerRows(n, repo) {
  const rows = [{ kind: "repo", count: null, text: repo || "Tuckerbox", tone: "foreground" }];
  if (n?.error) return [...rows, { kind: "error", count: null, text: "can't read the board", tone: "blocked" }];
  if (n?.running) rows.push({ kind: "running", count: n.running, text: "running", tone: "working" });
  if (n?.needs) rows.push({ kind: "needs", count: n.needs, text: n.needs === 1 ? "needs you" : "need you", tone: "blocked" });
  return rows;
}

// Runs from every machine (herdr-board). The board reads its own repository on
// the space's machine (`own`, null for this computer), then asks each other
// connected Herdr machine for the same repository by its key. These are the
// machines to ask: every one `machines()` lists but `own`. A machine whose
// layout is `unavailable` (an offline core) is not asked; it is unreachable.
export function otherMachines(list, own = null) {
  return (Array.isArray(list) ? list : [])
    .filter((m) => m && (m.name ?? null) !== (own ?? null))
    .map((m) => ({ name: m.name ?? null, reachable: m.layout !== "unavailable" }));
}

// A repository with no remote has its checkout path as its key, and lives on
// one machine only (design: Matching a repository across machines).
export function sharedRepo(board) {
  return Boolean(board && !board.error && board.repo && board.repo !== board.path);
}

// A card from a machine whose board layout differs (board.columns is also a
// machine setting) goes in the matching column of the board shown: a step or
// Queued column becomes In progress, In progress becomes the step's column or Queued.
export function fitColumn(card, columns) {
  const ids = new Set((columns ?? []).map((c) => c.id));
  if (ids.has(card.column)) return card.column;
  if ((card.column.startsWith("step:") || card.column === "queued") && ids.has("in_progress")) return "in_progress";
  if (card.column === "in_progress") {
    // A queued run keeps the step it will run, so queued wins over the step.
    const queued = card.state === "queued" || card.badge === "queued";
    if (!queued && card.step && ids.has(`step:${card.step}`)) return `step:${card.step}`;
    if (ids.has("queued")) return "queued";
  }
  return card.column;
}

// Where a card's commands run: its own machine and checkout. A card's `on` of
// null means this computer, so the board's machine stands in only when the
// card names no machine at all (a card handed in without its source).
export function cardAt(card, board) {
  const known = card != null && card.on !== undefined;
  return {
    cwd: card?.path || board?.path || "",
    on: known ? card.on : (board?.on ?? null),
  };
}

// How much a machine's copy of a card says: a queued or running run beats any
// other run, which beats no run. Equal copies keep the board's own.
function weight(card) {
  if (isActive(card)) return 2;
  return card.state ? 1 : 0;
}

// The board shown: the own board's columns and cards, with each other
// machine's cards merged in by change id. `others` holds { machine, board }
// with a normalised board or { error }. A machine without a checkout of the
// repository (repo_not_found) adds nothing; any other failure names the
// machine in `unreachable` and hides no card.
export function mergeBoards(own, others = []) {
  if (!own || own.error) return own;
  const cards = own.cards.slice();
  const at = new Map(cards.map((c, i) => [c.change, i]));
  const unreachable = [];
  for (const o of others) {
    const b = o?.board;
    if (!b || b.error) {
      if (b?.error?.code === "repo_not_found") continue;
      unreachable.push({ machine: o?.machine ?? null, message: b?.error?.message || "unreachable" });
      continue;
    }
    if (b.repo !== own.repo) continue;
    for (const c of b.cards) {
      const card = { ...c, column: fitColumn(c, own.columns) };
      const i = at.get(card.change);
      if (i === undefined) { at.set(card.change, cards.length); cards.push(card); }
      else if (weight(card) > weight(cards[i])) cards[i] = card;
    }
  }
  return { ...own, cards, unreachable };
}
