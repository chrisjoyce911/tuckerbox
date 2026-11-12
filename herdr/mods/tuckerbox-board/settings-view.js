// The Settings view (settings-view): in place of the board's columns, the
// repository's and this machine's configuration from `tuckerbox config show
// --json`, each value with its source, and edits saved through `tuckerbox
// config set`. It writes no file. The board (board.js) holds one while
// Settings is open and hands it its ticks and space changes.
import { div } from "gpui-kit";
import { v_flex, h_flex, Textarea, TextareaState } from "gpui-base";
import { theme, spaces, panes, focused, run } from "herdr";
import { MOD, loadConfig, configExec, locate, candidates, fixturesFolder } from "./data.js";
import { candidatesKey, NO_CONFIG } from "./model.js";
import { herdrCall } from "./actions.js";
import { candidate } from "./edits.js";
import {
  SECTIONS, groups, workflowSteps, sectionValue, editableIn, shownValue, choices, tokens, tokenSource,
  edit, addStep, removeStep, moveStep, currentSteps, agentChoices, setArgv, tokenArgv, editArgv, parseSet,
  targetText, canSave, problemsFor, changedUnder, fileError, startPane, tickPane, segments, cliValue,
} from "./config.js";
import { button, tiny, note, section } from "./ui.js";

const CHECK_MS = 400;

export class SettingsView {
  // host: the board; self() gives its pane, for the space it follows.
  constructor(host) {
    this.host = host;
    this.config = null;    // normaliseConfig(...) or { error }
    this.at = null;        // where it was read: { root, on, ... }
    this.want = null;      // the candidates key shown (stale reads dropped)
    this.loading = new Set();
    this.reset();
  }

  reset() {
    this.pending = { repo: new Map(), machine: new Map() };
    this.checks = { repo: null, machine: null };     // parseSet() of the last dry run
    this.checking = { repo: false, machine: false };
    this.gen = { repo: 0, machine: 0 };              // bumped by every edit
    this.conflicts = { repo: new Set(), machine: new Set() };
    this.notice = null;    // { text, kind, link }
    this.busy = null;      // "save-repo" | "save-machine"
    this.editing = null;   // { section, key, box, error }
    this.picking = null;   // { section, key }
    this.adding = null;    // { box, agent, error }
    this.pane = null;      // a Set token / Edit file pane being watched
  }

  // The space or its folders changed: what was shown goes at once, edits
  // with it (they were for another repository), and it reads again.
  follow(key, cx) {
    this.want = key;
    this.config = null;
    this.at = null;
    this.reset();
    this.load(cx);
  }

  // Reads `tuckerbox config show --json` (settings-view: Settings data). A
  // read that finishes after the space or machine changed is dropped.
  load(cx, after = null) {
    const self = this.host.self?.() ?? null;
    const c = candidates(self);
    const key = candidatesKey(c);
    if (this.want !== null && key !== this.want) { this.follow(key, cx); return; }
    this.want = key;
    if (this.loading.has(key)) return;
    this.loading.add(key);
    cx.spawn(async (cx) => {
      let at = null, cfg;
      try {
        at = (await fixturesFolder()) ? { found: true, root: "", cwd: "", on: null } : await locate(self, c);
        cfg = at.found ? await loadConfig(at) : { error: { code: "no_config", message: NO_CONFIG } };
      } catch (e) { cfg = { error: { code: "failed", message: String(e?.message ?? e) } }; }
      this.loading.delete(key);
      if (key !== this.want) return; // for a space no longer shown: dropped
      const before = this.config;
      this.at = at;
      this.config = cfg;
      after?.(before, cfg, cx);
      // Pending edits are checked again against what is on file now.
      for (const s of SECTIONS) if (this.pending[s].size) this.schedule(s, cx);
      cx.notify();
    });
  }

  // ---- pending edits ----

  // A section is read-only while its save runs: the save sends the edits
  // as they were, and clears them when it succeeds.
  locked(section) {
    return this.busy === `save-${section}`;
  }

  change(section, fn, cx) {
    if (this.locked(section)) return false;
    try { fn(this.pending[section]); } catch (e) { this.notice = { text: String(e?.message ?? e), kind: "error" }; cx.notify(); return false; }
    this.notice = null;
    this.conflicts[section].clear();
    this.schedule(section, cx);
    cx.notify();
    return true;
  }

  // A dry run of exactly the pending edits, a moment after the last one.
  schedule(section, cx) {
    const gen = ++this.gen[section];
    this.checks[section] = null;
    if (!this.pending[section].size) { this.checking[section] = false; return; }
    this.checking[section] = true;
    cx.timer.after(CHECK_MS, (cx) => this.check(section, gen, cx));
  }

  check(section, gen, cx) {
    if (gen !== this.gen[section] || !this.config || this.config.error) return;
    let argv;
    try { argv = setArgv(section, this.pending[section], this.config.files[section].revision, { dryRun: true }); }
    catch (e) { this.checking[section] = false; this.checks[section] = parseSet({ code: 1, stderr: String(e?.message ?? e) }); cx.notify(); return; }
    const want = this.want;
    cx.spawn(async (cx) => {
      const r = this.config.fixtures ? fixtureCheck(section) : parseSet(await configExec(argv, this.at));
      if (gen !== this.gen[section] || want !== this.want) return; // edited again since, or another space
      this.checking[section] = false;
      this.checks[section] = r;
      cx.notify();
    });
  }

  // One `tuckerbox config set --if-match` for the section's pending edits.
  save(section, cx) {
    if (this.busy || !canSave(this.pending[section], this.checks[section])) return;
    let argv;
    try { argv = setArgv(section, this.pending[section], this.config.files[section].revision); }
    catch (e) { this.notice = { text: String(e?.message ?? e), kind: "error" }; cx.notify(); return; }
    if (this.config.fixtures) { this.notice = { text: `Fixtures: would run ${argv.join(" ")}`, kind: "info" }; cx.notify(); return; }
    this.busy = `save-${section}`;
    // Open fields and pickers of the section close: nothing changes it now.
    if (this.editing?.section === section) this.editing = null;
    if (this.picking?.section === section) this.picking = null;
    if (section === "repo") this.adding = null;
    const want = this.want;
    const gen = this.gen[section];
    cx.notify();
    cx.spawn(async (cx) => {
      const r = parseSet(await configExec(argv, this.at));
      this.busy = null;
      if (want !== this.want) return;
      if (r.ok) {
        this.pending[section].clear();
        this.checks[section] = null;
        this.gen[section]++;
        this.conflicts[section].clear();
        const lines = [r.message, r.link ? `Merge request: ${r.link}` : "", ...r.changes.map((c) => `${c.change}: ${c.key} ${c.message}`)].filter(Boolean);
        this.notice = { text: lines.join("\n") || "Saved.", kind: "info", link: r.link };
        this.load(cx);
      } else if (r.error?.code === "changed_since_read") {
        // settings-view: Save conflicts. Edits kept; read again; mark what moved.
        this.notice = { text: "Someone saved this file since it was read. It has been read again: check the marked keys and save again.", kind: "error" };
        this.load(cx, (before, after) => {
          if (!after.error) this.conflicts[section] = changedUnder(before, after, section, this.pending[section]);
        });
      } else {
        // The command's own message; the edits stay.
        const why = [r.error?.message, ...r.problems.filter((p) => p.new).map((p) => `${p.key}: ${p.message}`)].filter(Boolean);
        this.notice = { text: why.join("\n") || "The save failed.", kind: "error" };
        if (r.problems.length && gen === this.gen[section]) this.checks[section] = r;
      }
      cx.notify();
    });
  }

  // ---- panes: Set token, Edit file ----

  openPane(argv, title, cx) {
    const at = this.at ?? {};
    const f = focused();
    const all = spaces();
    const s = all.find((x) => x.id === f?.space) ?? all.find((x) => x.active) ?? null;
    const shown = s ? { id: s.id, machine: s.machine ?? null } : null;
    try {
      const call = herdrCall({ action: "config", place: "pane", argv, on: at.on ?? null, cwd: at.root || at.cwd || "", title }, { shown, spaces: all, panes: panes(), mod: MOD });
      this.pane = startPane(panes(), Date.now(), { on: at.on ?? null, space: call.command === "terminal.open" ? shown?.id ?? null : null, folder: at.root || at.cwd || "" });
      run(call.command, call.args);
    } catch (e) { this.notice = { text: String(e?.message ?? e), kind: "error" }; }
    cx.notify();
  }

  setToken(key, cx) {
    let argv;
    try { argv = tokenArgv(key); } catch (e) { this.notice = { text: String(e?.message ?? e), kind: "error" }; cx.notify(); return; }
    if (this.config?.fixtures) { this.notice = { text: `Fixtures: would open a pane running ${argv.join(" ")}`, kind: "info" }; cx.notify(); return; }
    this.openPane(argv, `token ${segments(key)[1] ?? ""}`, cx);
    this.notice = { text: "Enter the token in the pane. Settings read again when it closes.", kind: "info" };
  }

  editFile(section, cx) {
    const argv = editArgv(section);
    if (this.config?.fixtures) { this.notice = { text: `Fixtures: would open a pane running ${argv.join(" ")}`, kind: "info" }; cx.notify(); return; }
    this.openPane(argv, section === "machine" ? "edit machine config" : "edit repository config", cx);
  }

  // From the board's timer: a watched pane that closed means read again.
  tick(cx) {
    if (!this.pane) return;
    const t = tickPane(this.pane, panes(), Date.now(), spaces(), candidate);
    this.pane = t.rec;
    if (t.closed) this.load(cx);
  }

  // ---- drawing ----

  render() {
    const tm = theme();
    const c = this.config;
    return v_flex().id("settings").flex_1().min_h(0).p(14).gap(8).overflow_y_scrollbar()
      .child(h_flex().items_center().gap(8)
        .child(tiny("settings-back", "← Back", (_e, cx) => this.host.closeSettings(cx)))
        .child(div().text_size(14).font_semibold().text_color(tm.foreground).child("Settings"))
        .child(div().flex_1())
        .child(tiny("settings-reload", "Read again", (_e, cx) => this.load(cx), { disabled: Boolean(this.busy) })))
      .when(Boolean(this.notice), (el) => el.child(h_flex().gap(8).items_start()
        .child(div().flex_1().child(note("settings-notice", this.notice.text, this.notice.kind)))
        .when(Boolean(this.notice.link), (el) => el.child(tiny("settings-link", "Open ↗", (_e, cx) => cx.open_url(this.notice.link))))
        .child(tiny("settings-notice-x", "Dismiss", (_e, cx) => { this.notice = null; cx.notify(); }))))
      .child(!c ? div().text_size(12).text_color(tm.dim).child("Reading the settings…")
        : c.error ? note("settings-error", c.error.message, "error")
          : v_flex().gap(8).child(this.section(c, "repo")).child(this.section(c, "machine")));
  }

  section(c, s) {
    const tm = theme();
    const name = c.machine || this.at?.on || "this computer";
    const title = s === "repo" ? "Repository" : `This machine only (${name})`;
    const err = fileError(c, s);
    const file = c.files[s];
    return v_flex().id(`section-${s}`).gap(4).p(10).rounded(8).bg(tm.chrome).border_1().border_color(tm.border)
      .child(h_flex().items_center().gap(8)
        .child(div().text_size(13).font_semibold().text_color(tm.foreground).child(title))
        .child(div().flex_1().min_w(0).truncate().text_size(10.5).text_color(tm.dim).child(file.path)))
      .child(div().text_size(11).text_color(tm.dim).child(s === "repo"
        ? "Committed with the repository: a save goes to a pull request, or to the checkout when this machine has no forge token."
        : "Neither committed nor shared: these apply on this machine only."))
      .when(Boolean(err), (el) => el.child(h_flex().items_start().gap(8)
        .child(div().flex_1().child(note(`file-error-${s}`, err, "error")))
        .child(button(`edit-file-${s}`, "Edit file", (_e, cx) => this.editFile(s, cx)))))
      .when(!err, (el) => el.children(groups(c, s).map((g) => this.group(c, s, g))))
      .when(!err, (el) => el.child(this.saveBar(s)));
  }

  group(c, s, g) {
    const tm = theme();
    const body = s === "repo" && g.id === "workflow" ? this.workflow(c, g)
      : s === "machine" && g.id === "forges" ? this.tokens(c, g)
        : v_flex().gap(2).children(g.keys.map((k) => this.field(c, s, k, k.key)));
    return v_flex().id(`group-${s}-${g.id}`).gap(2).child(section(g.name)).child(body);
  }

  // The workflow's steps in order, each with its settings, and the add,
  // remove and move buttons (settings-view: Editing the workflow).
  workflow(c, g) {
    const tm = theme();
    const p = this.pending.repo;
    const steps = workflowSteps(c, p);
    const order = currentSteps(c, p);
    const rest = g.keys.filter((k) => segments(k.key)[0] !== "steps" && k.key !== "workflow.steps");
    return v_flex().gap(4)
      .child(div().text_size(11).text_color(tm.dim).child(`steps: ${order.join(" → ") || "none"}${p.has("workflow.steps") ? " (pending)" : ""}`))
      .children(problemsFor(this.checks.repo, "workflow.steps").map((x, i) => this.problem(`wf-problem-${i}`, x)))
      .children(steps.map((st) => {
        const i = order.indexOf(st.id);
        return v_flex().id(`step-${st.id}`).gap(2).p(6).rounded(6).bg(tm.surface)
          .child(h_flex().items_center().gap(6)
            .child(div().text_size(12).font_semibold().text_color(st.inWorkflow ? tm.foreground : tm.dim).child(st.id))
            .when(!st.inWorkflow, (el) => el.child(div().text_size(10.5).text_color(tm.dim).child("not in the workflow")))
            .child(div().flex_1())
            .when(st.inWorkflow, (el) => el
              .child(tiny(`step-up-${st.id}`, "↑", (_e, cx) => this.change("repo", (pm) => moveStep(pm, c, st.id, -1), cx), { disabled: i <= 0 || this.locked("repo") }))
              .child(tiny(`step-down-${st.id}`, "↓", (_e, cx) => this.change("repo", (pm) => moveStep(pm, c, st.id, 1), cx), { disabled: i >= order.length - 1 || this.locked("repo") }))
              .child(tiny(`step-remove-${st.id}`, "Remove", (_e, cx) => this.change("repo", (pm) => removeStep(pm, c, st.id), cx), { danger: true, disabled: this.locked("repo") }))))
          .children(st.keys.map((k) => this.field(c, "repo", k, segments(k.key).slice(2).join("."))))
          .when(st.keys.length === 0 && p.has(`steps.${st.id}.agent`), (el) => el.child(div().text_size(11).text_color(tm.accent).child(`agent → ${p.get(`steps.${st.id}.agent`)} (pending)`)));
      }))
      .child(this.addStepRow(c))
      .children(rest.map((k) => this.field(c, "repo", k, k.key)));
  }

  addStepRow(c) {
    const tm = theme();
    const a = this.adding;
    if (this.locked("repo")) return h_flex().child(tiny("step-add", "+ Add step", () => {}, { disabled: true }));
    if (!a) return h_flex().child(tiny("step-add", "+ Add step", (_e, cx) => {
      this.adding = { box: TextareaState.new({ placeholder: "step id", rows: 1 }), agent: agentChoices(c)[0], error: "" }; cx.notify();
    }));
    return v_flex().id("step-add-form").gap(4).p(6).rounded(6).bg(tm.surface)
      .child(div().w_full().p(4).rounded(6).border_1().border_color(tm.input).bg(tm.background).child(Textarea.new(a.box)))
      .child(h_flex().flex_wrap().items_center().gap(6).text_size(11).text_color(tm.dim).child("agent:")
        .children(agentChoices(c).map((ag) => tiny(`step-add-agent-${ag}`, ag, (_e, cx) => { a.agent = ag; cx.notify(); }, { primary: a.agent === ag }))))
      .when(Boolean(a.error), (el) => el.child(div().text_size(11).text_color(tm.blocked).child(a.error)))
      .child(h_flex().gap(6)
        .child(tiny("step-add-ok", "Add", (_e, cx) => this.addStep(c, cx), { primary: true }))
        .child(tiny("step-add-cancel", "Cancel", (_e, cx) => { this.adding = null; cx.notify(); })));
  }

  addStep(c, cx) {
    if (this.locked("repo")) return;
    const a = this.adding;
    try { addStep(this.pending.repo, c, a.box.value().trim(), a.agent); }
    catch (e) { a.error = String(e?.message ?? e); cx.notify(); return; }
    this.adding = null;
    this.change("repo", () => {}, cx);
  }

  tokens(c, g) {
    const tm = theme();
    const rows = tokens(c);
    return v_flex().gap(2)
      .children(rows.map((t) => h_flex().id(`token-${t.host}`).items_center().gap(8).px(6).py(3).rounded(6).text_size(12)
        .child(div().flex_1().min_w(0).truncate().text_color(tm.foreground).child(t.host))
        .child(div().text_color(t.set ? tm.done ?? tm.foreground : tm.dim).child(t.set ? "set" : "not set"))
        .when(t.set && Boolean(t.source), (el) => el.child(div().text_size(10.5).text_color(tm.dim).child(tokenSource(t.source))))
        .child(tiny(`set-token-${t.host}`, "Set token", (_e, cx) => this.setToken(t.key, cx)))))
      .children(g.keys.filter((k) => !k.secret).map((k) => this.field(c, "machine", k, k.key)));
  }

  problem(id, p) {
    const tm = theme();
    return div().id(id).text_size(11).text_color(p.new ? tm.blocked : tm.dim).child(`${p.new ? "" : "already: "}${p.message}`);
  }

  // One key: its value (pending shown), source, what it hides, problems,
  // and how to change it where its scope allows.
  field(c, s, k, label) {
    const tm = theme();
    const id = `field-${s}-${k.key}`;
    const v = sectionValue(k, s);
    const p = this.pending[s];
    const has = p.has(k.key);
    const pend = has ? p.get(k.key) : undefined;
    const can = editableIn(k, s) && !k.secret && !this.locked(s);
    const opts = can ? choices(c, k) : null;
    const editing = this.editing?.section === s && this.editing?.key === k.key ? this.editing : null;
    const picking = this.picking?.section === s && this.picking?.key === k.key;
    const onFile = k.file === s || (s === "machine" && v.source === "machine");
    const probs = problemsFor(this.checks[s], k.key);
    return v_flex().id(id).gap(2).px(6).py(3).rounded(6)
      .child(h_flex().items_center().gap(8).text_size(12)
        .child(div().flex_none().w(150).truncate().text_color(tm.dim).child(label))
        .child(div().flex_1().min_w(0).truncate().text_color(has ? tm.muted : tm.foreground).when(has, (el) => el.line_through()).child(shownValue(v.value)))
        .when(has, (el) => el.child(div().text_color(tm.accent).child(pend === null ? "→ unset" : `→ ${pend}`)))
        .child(div().px(5).rounded(4).bg(tm.raised).text_size(10.5).text_color(v.source === s ? tm.accent : tm.dim).child(v.source))
        .when(can && !editing, (el) => el
          .child(tiny(`${id}-change`, "Change", (_e, cx) => this.startChange(s, k, opts, cx)))
          .when(onFile && !(has && pend === null), (el) => el.child(tiny(`${id}-unset`, "Unset", (_e, cx) => this.change(s, (pm) => edit(pm, c, s, k.key, null), cx))))
          .when(has, (el) => el.child(tiny(`${id}-undo`, "Undo", (_e, cx) => this.change(s, (pm) => pm.delete(k.key), cx))))))
      .when(Boolean(v.note), (el) => el.child(div().text_size(10.5).text_color(tm.dim).child(v.note)))
      .when(this.conflicts[s].has(k.key), (el) => el.child(div().text_size(11).text_color(tm.blocked).child(`changed by someone else: now ${shownValue(v.value)}`)))
      .children(probs.map((x, i) => this.problem(`${id}-problem-${i}`, x)))
      .when(picking && Boolean(opts), (el) => el.child(h_flex().flex_wrap().gap(6).children(opts.map((o) =>
        tiny(`${id}-pick-${o}`, o, (_e, cx) => { this.picking = null; this.change(s, (pm) => edit(pm, c, s, k.key, o), cx); },
          { primary: (has ? pend : cliValue(v.value)) === o })))))
      .when(Boolean(editing), (el) => el.child(v_flex().gap(4)
        .child(div().w_full().p(4).rounded(6).border_1().border_color(tm.input).bg(tm.background).child(Textarea.new(editing.box)))
        .when(k.type === "list", (el) => el.child(div().text_size(10.5).text_color(tm.dim).child("comma-separated")))
        .when(Boolean(editing.error), (el) => el.child(div().text_size(11).text_color(tm.blocked).child(editing.error)))
        .child(h_flex().gap(6)
          .child(tiny(`${id}-apply`, "Apply", (_e, cx) => this.applyText(c, cx), { primary: true }))
          .child(tiny(`${id}-cancel`, "Cancel", (_e, cx) => { this.editing = null; cx.notify(); })))));
  }

  startChange(s, k, opts, cx) {
    if (this.locked(s)) return;
    if (opts) { this.picking = this.picking?.key === k.key && this.picking?.section === s ? null : { section: s, key: k.key }; cx.notify(); return; }
    const p = this.pending[s];
    const value = p.has(k.key) && p.get(k.key) !== null ? p.get(k.key) : cliValue(sectionValue(k, s).value);
    this.editing = { section: s, key: k.key, box: TextareaState.new({ value, rows: 1 }), error: "" };
    cx.notify();
  }

  applyText(c, cx) {
    const e = this.editing;
    if (!e || this.locked(e.section)) return;
    try { edit(this.pending[e.section], c, e.section, e.key, e.box.value()); }
    catch (err) { e.error = String(err?.message ?? err); cx.notify(); return; }
    this.editing = null;
    this.change(e.section, () => {}, cx);
  }

  saveBar(s) {
    const tm = theme();
    const p = this.pending[s];
    if (!p.size) return div();
    const chk = this.checks[s];
    const ok = canSave(p, chk);
    const other = (chk?.problems ?? []).filter((x) => !p.has(x.key) && !x.key.startsWith("steps.") && x.key !== "workflow.steps");
    const status = this.checking[s] ? "Checking…"
      : chk?.error ? chk.error.message
        : chk && !ok ? "Save is off: an edit adds a problem." : targetText(chk);
    return v_flex().id(`save-bar-${s}`).gap(4).pt(8)
      .children(other.map((x, i) => this.problem(`problem-${s}-${i}`, x)))
      .children((chk?.changes ?? []).map((x, i) => div().id(`change-note-${s}-${i}`).text_size(11).text_color(tm.dim).child(`${x.change}: ${x.key} ${x.message}`)))
      .child(h_flex().items_center().gap(8)
        .child(div().flex_1().text_size(11.5).text_color(chk?.error || (chk && !ok) ? tm.blocked : tm.muted).child(status))
        .child(button(`discard-${s}`, "Discard", (_e, cx) => { p.clear(); this.conflicts[s].clear(); this.schedule(s, cx); cx.notify(); }, { disabled: Boolean(this.busy) }))
        .child(button(`save-${s}`, this.busy === `save-${s}` ? "Saving…" : `Save ${p.size} edit${p.size === 1 ? "" : "s"}`,
          (_e, cx) => this.save(s, cx), { primary: true, disabled: !ok || Boolean(this.busy) })));
  }
}

// With the dev fixtures nothing runs: a check says where it would go.
function fixtureCheck(section) {
  return { ok: true, dryRun: true, target: section === "machine" ? "machine" : "local", reason: "fixtures: nothing runs", problems: [], changes: [], link: "", message: "", error: null };
}

