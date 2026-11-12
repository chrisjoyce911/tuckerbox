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
      findings: list(r?.findings).map(finding), ended: str(r?.ended),
    })),
    history: list(d?.history).map((h) => ({
      at: str(h?.at), event: str(h?.event), step: str(h?.step), round: num(h?.round), agent: str(h?.agent),
      machine: str(h?.machine), result: str(h?.result), to: str(h?.to), summary: str(h?.summary),
      reason: str(h?.reason), findings: list(h?.findings).map(finding),
    })),
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
};

// One history line in words: "Handed on build → review: summary".
export function eventText(h) {
  const head = EVENT_WORDS[h.event] ?? h.event;
  const step = h.step ? (h.event === "handoff" && h.to ? ` ${h.step} → ${h.to}` : ` ${h.step}`) : "";
  const tail = h.event === "step_finished" && h.result ? ` (${h.result})`
    : h.event === "rejected" && h.findings.length ? ` with ${h.findings.length} finding${h.findings.length === 1 ? "" : "s"}`
      : "";
  const note = h.summary || h.reason;
  return `${head}${step}${tail}${note ? `: ${note}` : ""}`;
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

// Whether this copy is on screen. A window or dialog is shown while open. A
// pane is shown when its space is the active one and that space's active tab
// holds a board pane. Herdr keeps hidden copies running, so this is a guess
// from the live lists, erring on "shown" when the board can't tell.
export function isShown({ overlay, spaceId, spaces, panes, mod }) {
  if (overlay) return true;
  const space = list(spaces).find((s) => s.id === spaceId) ?? list(spaces).find((s) => s.active);
  if (!space) return true;
  if (!space.active) return false;
  const tab = list(space.tabs).find((t) => t.active);
  if (!tab) return true;
  const mine = list(panes).filter((p) => p.mod === mod);
  if (!mine.length) return true;
  return mine.some((p) => p.tab === tab.id);
}

// The folder of `file` from a stack trace line such as
// "at f (/home/me/mods/tuckerbox-board/data.js?v=3:12:9)". Null if not found.
export function dirFromStack(stack, file) {
  const name = file.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const m = new RegExp(`(?:\\(|at |^)\\s*((?:file://)?[^()\\n]*?)[\\\\/]${name}(?:\\?[^:)\\s]*)?:\\d+:\\d+`, "m").exec(String(stack));
  if (!m) return null;
  return m[1].replace(/^file:\/\//, "").trim() || null;
}
