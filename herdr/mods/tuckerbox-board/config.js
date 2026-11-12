// The Settings view's logic (settings-view): what `tuckerbox config show
// --json` says, sorted into sections and groups, the pending edits, and the
// `tuckerbox config` command lines that check and save them. The view writes
// no file: every change is a `tuckerbox config` command on the board's
// machine. No Herdr or gpui imports: node tests it (test/config.test.js).
//
// The JSON read here (config show --json, config set --json) is spelled out
// in README.md, "Settings: the JSON it reads".

export const SECTIONS = ["repo", "machine"];

// Agents Tuckerbox defines itself (internal/config BuiltinAgents).
export const BUILTIN_AGENTS = ["claude", "codex"];

// Groups by a key's first segment. Order is display order; a key no group
// claims goes to Other, so a new key is never hidden.
export const GROUPS = {
  repo: [
    { id: "workflow", name: "Workflow", prefixes: ["workflow", "steps"] },
    { id: "runs", name: "Runs", prefixes: ["max_parallel", "ship", "require_approval", "shipping", "mode", "fleet", "team"] },
    { id: "board", name: "Board", prefixes: ["board"] },
    { id: "autopilot", name: "Autopilot", prefixes: ["autopilot"] },
    { id: "overrides", name: "Change overrides", prefixes: ["overrides"] },
  ],
  machine: [
    { id: "permissions", name: "Permission profiles", prefixes: ["permissions"] },
    { id: "forges", name: "Forge tokens", prefixes: ["forges"] },
    { id: "editor", name: "Editor", prefixes: ["editor"] },
    { id: "machines", name: "Machines", prefixes: ["machines", "fleet_capacity", "machine"] },
  ],
};

// The order a step's settings show in.
export const STEP_KEYS = ["agent", "role", "model", "effort", "permissions", "on_reject", "max_rounds"];

const SCOPES = new Set(["repo", "machine", "either"]);
const TYPES = new Set(["string", "int", "bool", "list"]);

// ---- reading `config show --json` ----

function fileOf(f) {
  const e = f?.error;
  return {
    path: String(f?.path ?? ""),
    revision: typeof f?.revision === "string" && f.revision ? f.revision : null,
    error: e ? { message: String(e.message ?? e), line: Number.isInteger(e.line) ? e.line : null } : null,
  };
}

export function normaliseKey(k) {
  const scope = SCOPES.has(k?.scope) ? k.scope : "either";
  const secret = Boolean(k?.secret);
  const ov = k?.overridden;
  return {
    key: String(k?.key ?? ""),
    value: secret ? null : k?.value ?? null,
    source: String(k?.source ?? "default"),
    scope,
    file: String(k?.file ?? ""),
    type: TYPES.has(k?.type) ? k.type : "string",
    allowed: Array.isArray(k?.allowed) && k.allowed.length ? k.allowed.map(String) : null,
    default: secret ? null : k?.default ?? null,
    secret,
    // A secret is only ever set or not set; its value never reaches the mod.
    set: secret ? Boolean(k?.set ?? k?.value === "set") : undefined,
    overridden: ov && typeof ov === "object" && !secret ? { source: String(ov.source ?? "machine"), value: ov.value ?? null } : null,
  };
}

// { machine, files: { repo, machine, credentials }, keys } or { error }.
export function normaliseConfig(doc) {
  if (!doc || typeof doc !== "object" || !Array.isArray(doc.keys)) {
    return { error: { code: "failed", message: "tuckerbox config show --json gave no key list: is tuckerbox new enough?" } };
  }
  const files = doc.files ?? {};
  return {
    machine: String(doc.machine ?? ""),
    files: { repo: fileOf(files.repo), machine: fileOf(files.machine), credentials: fileOf(files.credentials) },
    keys: doc.keys.map(normaliseKey).filter((k) => k.key),
  };
}

// What `tuckerbox config show --json` printed: the config, or { error } with
// the command's own message.
export function parseShow(stdout, stderr, code) {
  let doc = null;
  try { doc = JSON.parse(String(stdout ?? "")); } catch { doc = null; }
  if (doc && typeof doc === "object") return normaliseConfig(doc);
  const message = String(stderr ?? "").trim() || String(stdout ?? "").trim() || `tuckerbox config show exited with code ${code}`;
  return { error: { code: "failed", message } };
}

// ---- values ----

// The key's segments: dots split, except inside quotes (forges."git.example.org".token).
export function segments(key) {
  const out = [];
  let cur = "", q = false;
  for (const ch of String(key)) {
    if (ch === '"') { q = !q; cur += ch; } else if (ch === "." && !q) { out.push(cur); cur = ""; } else cur += ch;
  }
  out.push(cur);
  return out;
}

const unquote = (s) => String(s).replace(/^"(.*)"$/, "$1");

// A value as `config set` takes it: a list comma-separated, the rest as text.
export function cliValue(v) {
  if (v == null) return "";
  if (Array.isArray(v)) return v.map(String).join(",");
  return String(v);
}

// A value for people: "not set" for none.
export function shownValue(v) {
  if (v == null || (Array.isArray(v) && v.length === 0)) return "not set";
  if (Array.isArray(v)) return v.join(", ");
  return String(v);
}

// Whether a key can be set from this section.
export const editableIn = (k, section) => k.scope === "either" || k.scope === section;

// What a key shows in a section: { value, source, note }.
//   repo:    the effective value and its source, noting a machine value it hides
//   machine: this machine's own value: set in machine config, else the default
export function sectionValue(k, section) {
  if (section === "machine" && k.scope === "either") {
    if (k.source === "machine") return { value: k.value, source: "machine", note: "" };
    if (k.overridden?.source === "machine") {
      return { value: k.overridden.value, source: "machine", note: `the repository sets ${shownValue(k.value)}` };
    }
    return { value: k.default ?? (k.source === "default" ? k.value : null), source: "default", note: "" };
  }
  const value = k.value ?? (k.source === "default" ? k.default : null);
  const note = k.overridden ? `${k.overridden.source} config sets ${shownValue(k.overridden.value)}` : "";
  return { value, source: k.source, note };
}

// ---- sections and groups ----

function groupOf(section, key) {
  const first = segments(key)[0];
  return GROUPS[section].find((g) => g.prefixes.includes(first))?.id ?? "other";
}

// The keys each section lists, by scope: repo keys only in Repository,
// machine keys only in the machine section, either keys in both.
export function sectionKeys(cfg, section) {
  return (cfg?.keys ?? []).filter((k) => editableIn(k, section));
}

// A section's groups in order, each { id, name, keys }; empty groups dropped.
// A file that doesn't parse gives no groups: its section shows the error.
export function groups(cfg, section) {
  if (cfg?.files?.[section]?.error) return [];
  const all = [...GROUPS[section], { id: "other", name: "Other", prefixes: [] }];
  const by = new Map(all.map((g) => [g.id, []]));
  for (const k of sectionKeys(cfg, section)) by.get(groupOf(section, k.key)).push(k);
  return all.map((g) => ({ id: g.id, name: g.name, keys: by.get(g.id) })).filter((g) => g.keys.length > 0);
}

const find = (cfg, key) => (cfg?.keys ?? []).find((k) => k.key === key) ?? null;

// The workflow's steps in order, each { id, inWorkflow, keys } where keys
// are that step's settings (steps.<id>.<setting>) in STEP_KEYS order. A
// step with settings that the workflow doesn't list comes last.
// `pending` (a Map for the repo section) gives workflow.steps if edited.
export function workflowSteps(cfg, pending = new Map()) {
  const order = pending.has("workflow.steps")
    ? splitList(pending.get("workflow.steps"))
    : listOf(sectionValue(find(cfg, "workflow.steps") ?? { value: null }, "repo").value);
  const byStep = new Map();
  for (const k of cfg?.keys ?? []) {
    const s = segments(k.key);
    if (s[0] !== "steps" || s.length < 3) continue;
    const id = unquote(s[1]);
    if (!byStep.has(id)) byStep.set(id, []);
    byStep.get(id).push(k);
  }
  const rank = (k) => { const i = STEP_KEYS.indexOf(segments(k.key).slice(2).join(".")); return i < 0 ? STEP_KEYS.length : i; };
  const ids = [...order, ...[...byStep.keys()].filter((id) => !order.includes(id))];
  return ids.map((id) => ({ id, inWorkflow: order.includes(id), keys: (byStep.get(id) ?? []).slice().sort((a, b) => rank(a) - rank(b)) }));
}

const listOf = (v) => (Array.isArray(v) ? v.map(String) : v == null || v === "" ? [] : splitList(String(v)));
export const splitList = (s) => String(s ?? "").split(",").map((x) => x.trim()).filter(Boolean);

// The agents a step may name: the built-in ones and any [agents.<name>] in the effective config.
export function agentChoices(cfg) {
  const out = [...BUILTIN_AGENTS];
  for (const k of cfg?.keys ?? []) {
    const s = segments(k.key);
    if (s[0] === "agents" && s.length >= 2 && !out.includes(unquote(s[1]))) out.push(unquote(s[1]));
  }
  return out;
}

// The fixed values a key's picker offers, or null for a text field.
export function choices(cfg, k) {
  if (k.allowed) return k.allowed;
  if (k.type === "bool") return ["true", "false"];
  const s = segments(k.key);
  if (s[0] === "steps" && s[2] === "agent") return agentChoices(cfg);
  if (s[0] === "steps" && s[2] === "on_reject") return null;
  return null;
}

// Forge tokens: one row per host, { host, key, set, source }. No value.
export function tokens(cfg) {
  return (cfg?.keys ?? []).filter((k) => k.secret && segments(k.key)[0] === "forges")
    .map((k) => ({ host: unquote(segments(k.key)[1] ?? ""), key: k.key, set: Boolean(k.set), source: k.source || "" }));
}

// Where a token in use comes from, for people.
export function tokenSource(src) {
  if (!src) return "";
  if (src === "credentials") return "from the credentials file";
  if (src.startsWith("env:")) return `from $${src.slice(4)}`;
  if (src === "gh" || src === "glab") return `from ${src}`;
  return `from ${src}`;
}

// ---- pending edits ----
// Each section has a Map key -> value: a string as `config set` takes it,
// or null to unset (--unset). An edit back to the value on file is dropped.

const KEY = /^[A-Za-z0-9_-]+(\.([A-Za-z0-9_-]+|"[^"\s=]+"))*$/;

export function checkKey(key) {
  if (!KEY.test(String(key ?? ""))) throw new Error(`not a config key: ${JSON.stringify(key)}`);
  return key;
}

export function checkValue(v) {
  const s = String(v ?? "");
  if (!s.trim()) throw new Error("Type a value, or Unset the key.");
  if (/[\u0000-\u001f\u007f]/.test(s)) throw new Error("A value can't hold a line break or control character.");
  return s.trim();
}

// The value a key has on file in a section, as `config set` takes it ("" none).
function onFile(cfg, section, key) {
  const k = find(cfg, key);
  if (!k || k.file !== section) return null;
  return cliValue(section === "machine" && k.scope === "either" ? sectionValue(k, "machine").value : k.value);
}

// Adds an edit to `pending` (a Map); returns the Map. Throws on input that
// must never reach a command.
export function edit(pending, cfg, section, key, value) {
  checkKey(key);
  const k = find(cfg, key);
  if (k?.secret) throw new Error("Tokens are set only at the token prompt: use Set token.");
  if (k && !editableIn(k, section)) {
    throw new Error(`${key} belongs in ${k.scope === "machine" ? "this machine's config" : "repository config"}`);
  }
  const v = value === null ? null : checkValue(value);
  const now = onFile(cfg, section, key);
  if ((v === null && now === null) || (v !== null && v === now)) pending.delete(key);
  else pending.set(key, v);
  return pending;
}

// The workflow edits (repository): add a step (an id and an agent), remove
// one, move one up or down. Each is pending edits to workflow.steps and
// steps.<id>.*.
const STEP_ID = /^[A-Za-z0-9][A-Za-z0-9_-]*$/;

export function currentSteps(cfg, pending) {
  return workflowSteps(cfg, pending).filter((s) => s.inWorkflow).map((s) => s.id);
}

export function addStep(pending, cfg, id, agent) {
  if (!STEP_ID.test(String(id ?? ""))) throw new Error(`A step id is letters, digits, - and _: not ${JSON.stringify(id)}`);
  const steps = currentSteps(cfg, pending);
  if (steps.includes(id)) throw new Error(`The workflow already has a step ${id}.`);
  if (!agent) throw new Error("Pick the step's agent.");
  edit(pending, cfg, "repo", "workflow.steps", [...steps, id].join(","));
  edit(pending, cfg, "repo", `steps.${id}.agent`, agent);
  return pending;
}

export function removeStep(pending, cfg, id) {
  const steps = currentSteps(cfg, pending);
  if (!steps.includes(id)) throw new Error(`No step ${id} in the workflow.`);
  if (steps.length === 1) throw new Error("The workflow needs at least one step.");
  edit(pending, cfg, "repo", "workflow.steps", steps.filter((s) => s !== id).join(","));
  // Its settings in repository config go too; pending ones are dropped.
  for (const key of [...pending.keys()]) if (key.startsWith(`steps.${id}.`)) pending.delete(key);
  for (const k of cfg?.keys ?? []) if (k.key.startsWith(`steps.${id}.`) && k.file === "repo") pending.set(k.key, null);
  return pending;
}

export function moveStep(pending, cfg, id, by) {
  const steps = currentSteps(cfg, pending);
  const i = steps.indexOf(id);
  const j = i + by;
  if (i < 0 || j < 0 || j >= steps.length) return pending;
  [steps[i], steps[j]] = [steps[j], steps[i]];
  edit(pending, cfg, "repo", "workflow.steps", steps.join(","));
  return pending;
}

// ---- commands ----

// `tuckerbox config set` as argv (no shell): the section's pending edits in
// one save, checked against `revision` (none: the file must still be absent).
export function setArgv(section, pending, revision, { dryRun = false } = {}) {
  if (!SECTIONS.includes(section)) throw new Error(`no section ${section}`);
  if (!pending?.size) throw new Error("nothing to save");
  const sets = [], unsets = [];
  for (const [key, v] of pending) {
    checkKey(key);
    if (v === null) unsets.push("--unset", key);
    else sets.push(`${key}=${checkValue(v)}`);
  }
  return ["tuckerbox", "config", "set", ...(section === "machine" ? ["--machine"] : []), "--json",
    ...(dryRun ? ["--dry-run"] : []), "--if-match", revision || "none", ...sets, ...unsets];
}

export const showArgv = () => ["tuckerbox", "config", "show", "--json"];

// The token prompt: a pane runs this; the token goes only through it.
export function tokenArgv(key) {
  checkKey(key);
  if (segments(key)[0] !== "forges" || segments(key).at(-1) !== "token") throw new Error(`not a forge token key: ${key}`);
  return ["tuckerbox", "config", "set", "--machine", key];
}

export const editArgv = (section) => ["tuckerbox", "config", "edit", ...(section === "machine" ? ["--machine"] : [])];

// What `config set --json` printed: { ok, dryRun, target, reason, problems,
// changes, link, message, error }. Output that isn't the JSON document (an
// older tuckerbox, a crash) becomes an error with the command's own text.
export function parseSet(r) {
  let doc = null;
  try { doc = JSON.parse(String(r?.stdout ?? "")); } catch { doc = null; }
  const code = r?.code ?? 1;
  if (!doc || typeof doc !== "object" || typeof doc.ok !== "boolean") {
    const text = String(r?.stderr ?? "").trim() || String(r?.stdout ?? "").trim() || `exited with code ${code}`;
    return { ok: false, target: "", reason: "", problems: [], changes: [], link: "", message: "", error: { code: "failed", message: text } };
  }
  const problems = (Array.isArray(doc.problems) ? doc.problems : []).map((p) => ({
    file: String(p?.file ?? ""), key: String(p?.key ?? ""), message: String(p?.message ?? ""), new: Boolean(p?.new),
  }));
  const changes = (Array.isArray(doc.changes) ? doc.changes : []).map((c) => ({
    change: String(c?.change ?? ""), key: String(c?.key ?? ""), message: String(c?.message ?? ""),
  }));
  const link = typeof doc.link === "string" && /^https:\/\/[^\s]+$/.test(doc.link) ? doc.link : "";
  const err = doc.error ? { code: String(doc.error.code ?? "failed"), message: String(doc.error.message ?? "") } : null;
  return {
    ok: doc.ok && code === 0, dryRun: Boolean(doc.dry_run), target: String(doc.target ?? ""), reason: String(doc.reason ?? ""),
    problems, changes, link, message: String(doc.message ?? ""),
    error: err ?? (doc.ok && code !== 0 ? { code: "failed", message: String(r?.stderr ?? "").trim() || `exited with code ${code}` } : null),
  };
}

// Where a save goes, for people.
export function targetText(check) {
  if (!check?.target) return "";
  const where = { pr: "Saves as a pull request", local: "Saves to the checkout, not committed", machine: "Saves to this machine's config" }[check.target] ?? `Saves to ${check.target}`;
  return check.reason ? `${where}: ${check.reason}` : where;
}

// Whether Save may run: pending edits, a check of exactly these edits that
// added no problem and was not refused.
export function canSave(pending, check) {
  if (!pending?.size || !check) return false;
  if (check.error) return false;
  return !check.problems.some((p) => p.new);
}

// The problems a check found for one key (shown by that field).
export const problemsFor = (check, key) => (check?.problems ?? []).filter((p) => p.key === key || p.key.startsWith(`${key}.`));

// After a save refused as changed since read: the pending keys whose value
// on file changed between the two reads.
export function changedUnder(before, after, section, pending) {
  const out = new Set();
  for (const key of pending?.keys() ?? []) {
    const a = find(before, key), b = find(after, key);
    const va = a ? cliValue(sectionValue(a, section).value) : "";
    const vb = b ? cliValue(sectionValue(b, section).value) : "";
    if (va !== vb || (a?.source ?? "") !== (b?.source ?? "")) out.add(key);
  }
  return out;
}

// The section's file problem line: "file:line: message".
export function fileError(cfg, section) {
  const f = cfg?.files?.[section];
  if (!f?.error) return null;
  return `${f.path || (section === "repo" ? ".tuckerbox/config.toml" : "machine config")}${f.error.line ? `:${f.error.line}` : ""}: ${f.error.message}`;
}

// ---- the panes it opens (Set token, Edit file) ----
// Herdr's run() says nothing of the pane it opened; as edits.js does, the
// view notes the panes before and claims the one new terminal pane on the
// board's machine in the checkout. When it closes, the view reads again.

export function startPane(panesNow, now, target) {
  return { state: "opening", pane: null, before: new Set((panesNow ?? []).map((p) => p.pane)), started: now, ...target };
}

const OPEN_MS = 10000;

// Moves a pane record on: { rec, closed }. `closed` is true once, when the
// claimed pane is gone (or none was ever seen: then read again anyway).
export function tickPane(rec, panesNow, now, spacesNow, candidate) {
  if (!rec) return { rec: null, closed: false };
  const panes = panesNow ?? [];
  if (rec.state === "opening") {
    const c = panes.filter((p) => candidate(rec, p, spacesNow));
    if (c.length === 1) return { rec: { ...rec, state: "open", pane: c[0].pane }, closed: false };
    if (now - rec.started > OPEN_MS) return { rec: null, closed: true };
    return { rec, closed: false };
  }
  if (rec.state === "open" && !panes.some((p) => p.pane === rec.pane)) return { rec: null, closed: true };
  return { rec, closed: false };
}
