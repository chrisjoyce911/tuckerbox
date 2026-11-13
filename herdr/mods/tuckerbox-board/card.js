// One change in a dialog: proposal, tasks, approval, last validation,
// step settings with their pickers, dependencies, rounds, history and the
// actions its state offers. Everything
// comes from `tuckerbox board --json --change <id>`, read when it opens.
import { View, div } from "gpui-kit";
import { v_flex, h_flex, TextView, Textarea, TextareaState } from "gpui-base";
import { theme, props, closeOverlay } from "herdr";
import { loadDetail } from "./data.js";
import { normaliseCard, cardAt, columnName, newestFirst, eventText, fmtLocal, fmtBrisbane, taskGroups, approvalBadge, isActive, activityRows, waitingForYou, ago, answerable, showsApproval } from "./model.js";
import { detailActions, invocation, actionLabel, defaultStep, rerunSteps, runGroups, canEdit, EDIT_FILES, ACTIONS, answerActions, confirmText, claimLine } from "./actions.js";
import { perform, validate, watchEdits, changed } from "./perform.js";
import { editOf, editNote } from "./edits.js";
import { settingsRows, choose, canPick } from "./settings.js";
import { button, tiny, dot, stateChip, approvalChip, progressBar, section, closeButton, note, tone } from "./ui.js";

const SEVERITY_TONE = { critical: "blocked", high: "blocked", medium: "done", low: "muted", info: "dim" };

export class CardView extends View {
  init(p, cx) {
    const pr = props() ?? p ?? {};
    this.change = pr.change ?? "";
    this.board = pr.board ?? { columns: [], require_approval: true };
    // Keep the card's machine, checkout and operating system: the answer
    // actions pick their standard-input transport from os (stdinCall) and
    // may run before, or without, the detail read below.
    this.card = pr.card ? normaliseCard(pr.card, { on: cardAt(pr.card, this.board).on, path: pr.card.path ?? this.board.path ?? "", os: pr.card.os || this.board.os || "" }) : null;
    this.detail = pr.detail ?? null;   // a test harness may hand it in
    this.error = null;
    this.notice = null;
    // A harness may hand in a validation note; else it comes from edits.js.
    this.validation = pr.validation ?? null;
    this.busy = null;
    this.picking = null;  // { step, key } whose picker is open
    this.pending = null;  // a pick waiting for the person to confirm: { extra, confirm }
    this.step = this.card ? defaultStep(this.card, this.board) : "";
    this.handle = cx.focus_handle();
    this.handle.focus();
    // blocked-answers: the text box under a blocked or waiting card's
    // question (kept on the view: retained state is made in init).
    this.answerBox = TextareaState.new({ placeholder: "Your answer: the agent continues from it", rows: 4 });
    // Read on open, from a timer: a spawn straight from a dialog's init can stall.
    if (!this.detail) cx.timer.after(30, (cx) => this.load(cx));
    this.seen = changed();
    cx.timer.every(1000, (cx) => {
      const validating = watchEdits();
      if (validating.length) cx.spawn(async (cx) => { await Promise.all(validating); cx.notify(); });
      // A validation finished (here or in the board): redraw its note.
      if (changed() !== this.seen) { this.seen = changed(); cx.notify(); }
    });
  }

  load(cx) {
    const at = cardAt(this.card, this.board);
    cx.spawn(async (cx) => {
      let d;
      try { d = await loadDetail(this.change, at); } catch (e) { d = { error: { message: String(e?.message ?? e) } }; }
      if (d.error) this.error = d.error.message;
      else {
        this.error = null;
        const card = d.cards.find((c) => c.change === this.change);
        if (card) this.card = { ...card, on: at.on, path: d.path || at.cwd, os: card.os || d.os || this.card?.os || "" };
        this.board = { ...this.board, ship:d.ship,mode:d.mode,columns: d.columns, steps: d.steps, require_approval: d.require_approval, path: d.path };
        this.detail = d.detail && d.detail.change === this.change ? d.detail : null;
        if (!this.detail) this.error = `No detail for ${this.change} in what tuckerbox printed.`;
      }
      cx.notify();
    });
  }

  dismiss() {
    closeOverlay(props()?.overlay);
  }

  act(action, extra, cx) {
    if (this.busy) return;
    // Take over asks first: work on the old machine is left behind.
    const ask = confirmText(action, this.card);
    if (ask && !extra?.confirmed) { this.pending = { action, extra: { ...extra, confirmed: true }, confirm: ask }; cx.notify(); return; }
    if (action === "validate") {
      this.busy = action;
      cx.notify();
      cx.spawn(async (cx) => {
        if (this.board.fixtures) this.notice = { text: `Fixtures: would run tuckerbox validate ${this.change} --json`, kind: "info" };
        else await validate(this.card);
        this.busy = null;
        cx.notify();
      });
      return;
    }
    let inv;
    try { inv = invocation(action, this.card, extra); }
    catch (e) { this.notice = { text: String(e?.message ?? e), kind: "error" }; cx.notify(); return; }
    inv.card = this.card;
    inv.board = this.board;
    this.busy = action;
    cx.notify();
    cx.spawn(async (cx) => {
      const r = await perform(inv, { dry: Boolean(this.board.fixtures),openURL:(url)=>cx.open_url(url) });
      this.busy = null;
      this.notice = r.message ? { text: r.message, kind: r.ok || r.dry ? "info" : "error" } : null;
      // A pane, tab or view opens beside the board: the dialog would cover it.
      if (r.ok && inv.place !== "exec") { this.dismiss(); return; }
      // An answer went in: empty the box; the read shows the card move on.
      if (r.ok && inv.stdin != null) this.answerBox?.set_value("");
      if (r.ok) this.load(cx);
      cx.notify();
    });
  }

  // A settings pick: run tuckerbox set at once, or ask first when it would
  // void the approval (settings-board: Warning before voiding approval).
  pick(step, key, value, cx) {
    this.picking = null;
    const r = choose(this.card, this.detail, step, key, value);
    if (r.noop) { cx.notify(); return; }
    if (r.confirm) { this.pending = r; cx.notify(); return; }
    this.act("set", r.extra, cx);
  }

  // ---- sections ----

  // blocked-answers: Answering from the board. What the run asks (the
  // blocked result's summary and findings, or the waiting agent's last
  // message), the answers so far, a text box, and Answer (and, on a blocked
  // card, Answer and resume), which run tuckerbox answer with the text on
  // standard input.
  question(c) {
    const tm = theme();
    const q = c.question;
    const acts = answerActions(c);
    const waiting = c.state !== "blocked";
    const asked = q ? `${waiting ? "the agent waits for you" : "asked by"} ${q.step} · round ${q.round}` : "";
    const none = waiting ? "The agent waits in its terminal: a question or a permission prompt. Answer types into it, or Watch it."
      : "The run is blocked, but its step left no question.";
    return v_flex().gap(6).text_size(12)
      .when(Boolean(asked), (el) => el.child(div().text_size(11).text_color(tm.dim).child(asked)))
      .child(div().text_color(q?.summary ? tm.foreground : tm.dim).child(q?.summary || none))
      .when((q?.findings.length ?? 0) > 0, (el) => el.child(this.findings(q.findings, "question")))
      .when((q?.answers.length ?? 0) > 0, (el) => el.child(v_flex().gap(4).pt(2)
        .children(q.answers.map((a, i) => v_flex().id(`answer-${i}`).gap(1).pl(8).border_l(2).border_color(tm.border)
          .child(div().text_size(10.5).text_color(tm.dim).child(`answered by ${a.by || "someone"}${a.at ? `, ${fmtBrisbane(a.at)}` : ""}`))
          .child(div().text_color(tm.muted).child(a.text))))))
      .when(Boolean(this.answerBox), (el) => el.child(div().w_full().p(6).rounded(6).border_1().border_color(tm.input).bg(tm.background)
        .child(Textarea.new(this.answerBox))))
      .child(h_flex().flex_wrap().gap(6).items_center().children(acts.map((a) => button(`act-${a}`,
        this.busy === a ? `${actionLabel(a)}…` : actionLabel(a), (_e, cx) => this.answer(a, cx),
        { primary: ACTIONS[a].primary, disabled: Boolean(this.busy) }))));
  }

  answer(action, cx) {
    this.act(action, { text: this.answerBox?.value() ?? "" }, cx);
  }

  // Each step's effective settings, with their source, and a picker for
  // each key the allow-list permits; none while a run holds the change.
  settings(c) {
    const tm = theme();
    const rows = settingsRows(c, this.detail);
    const open = this.picking;
    return v_flex().gap(4)
      .when(!canPick(c) && rows.length > 0 && !c.archived, (el) => el.child(div().pb(2).text_size(11).text_color(tm.dim)
        .child("Settings change only while no run is queued or running: it holds the change.")))
      .when(c.machines.length > 0, (el) => el.child(h_flex().gap(6).text_size(11.5)
        .child(div().text_color(tm.dim).child("machines")).child(div().text_color(tm.foreground).child(c.machines.join(", ")))))
      .children(rows.map((r) => v_flex().id(`settings-${r.step}`).gap(3).px(6).py(4).rounded(6).bg(tm.surface)
        .child(h_flex().items_center().gap(8).text_size(12)
          .child(div().font_semibold().text_color(tm.foreground).child(r.step))
          .when(r.started, (el) => el.child(div().text_size(10.5).text_color(tm.dim).child("as the running step started"))))
        .child(h_flex().flex_wrap().gap(10).text_size(11.5).children(r.keys.map((k) => {
          const id = `set-${r.step}-${k.key}`;
          return h_flex().id(id).items_center().gap(4).px(4).rounded(5)
            .child(div().text_color(tm.dim).child(k.key))
            .child(div().text_color(k.setting?.value ? tm.foreground : tm.muted).child(k.text))
            .child(div().text_size(10.5).text_color(k.source === "change" ? tm.accent : tm.dim).child(k.source))
            .when(Boolean(k.options), (el) => el.cursor_pointer().hover((s) => s.bg(tm.hover))
              .on_click((_e, cx) => { this.picking = open?.step === r.step && open?.key === k.key ? null : { step: r.step, key: k.key }; cx.notify(); })
              .child(div().text_color(tm.dim).child("▾")));
        })))
        .when(Boolean(open) && open.step === r.step, (el) => {
          const k = r.keys.find((x) => x.key === open.key);
          if (!k?.options) return el;
          return el.child(h_flex().flex_wrap().items_center().gap(6).pt(2).text_size(11).text_color(tm.dim)
            .child(`${open.key}:`)
            .children(k.options.map((o) => tiny(`pick-${r.step}-${k.key}-${o.value ?? "default"}`, o.label,
              (_e, cx) => this.pick(r.step, k.key, o.value, cx),
              { primary: o.value === null ? k.source !== "change" : k.source === "change" && k.setting?.value === o.value, disabled: Boolean(this.busy) }))));
        })));
  }

  // The question before a pick that voids the approval.
  confirmBox() {
    const p = this.pending;
    return h_flex().items_start().gap(8)
      .child(div().flex_1().child(note("confirm-set", p.confirm, "error")))
      .child(button("confirm-set-yes", p.action ? actionLabel(p.action) : "Change it", (_e, cx) => { this.pending = null; this.act(p.action ?? "set", p.extra, cx); }, { danger: true, disabled: Boolean(this.busy) }))
      .child(button("confirm-set-no", "Cancel", (_e, cx) => { this.pending = null; cx.notify(); }));
  }

  approvalRow(c) {
    const tm = theme();
    const a = c.approval;
    const b = approvalBadge(a);
    return h_flex().items_center().gap(8).text_size(11.5).flex_wrap()
      .child(approvalChip(a))
      .when(a.state !== "none" && Boolean(a.by), (el) => el.child(div().text_color(tm.muted).child(`by ${a.by}`)))
      .when(Boolean(a.at), (el) => el.child(div().text_color(tm.dim).child(fmtLocal(a.at))))
      .when(a.state !== "none", (el) => el.child(div().text_color(a.committed ? tm.dim : tm.blocked)
        .child(a.committed ? "committed" : "not committed: commit and push before tuckerbox run")))
      .when(!a.published, (el) => el.child(div().text_color(tm.blocked)
        .child("not published: it counts on this machine and is published with the next approve, unapprove or board read")))
      .when(a.state === "stale", (el) => el.child(div().text_color(tone(tm, b.tone)).child("the change was edited after approval")));
  }

  // The last edit and its validation (edits.js, in memory), with Validate
  // when the board couldn't watch the edit's pane, or to check again.
  validationBox(c) {
    const tm = theme();
    const n = this.validation ?? editNote(editOf(c));
    if (!n) return null;
    return h_flex().items_start().gap(8)
      .child(div().flex_1().child(note("validation", n.text, n.tone === "blocked" ? "error" : "info")))
      .when(Boolean(n.validate) && canEdit(c), (el) => el.child(button("act-validate", "Validate", (_e, cx) => this.act("validate", {}, cx), { disabled: Boolean(this.busy) })));
  }

  tasks(c) {
    const tm = theme();
    const d = this.detail;
    const editable = canEdit(c);
    return v_flex().gap(1).children(taskGroups(d.tasks).map((g, gi) => v_flex()
      .child(div().pt(gi ? 8 : 2).pb(3).text_size(11.5).font_medium().text_color(tm.muted).child(g.group || "Tasks"))
      .children(g.tasks.map((t) => {
        const action = t.checked ? "untick" : "tick";
        return h_flex().id(`task-${t.number}`).items_start().gap(8).px(6).py(3).rounded(6).text_size(12)
          .when(editable, (el) => el.cursor_pointer().hover((s) => s.bg(tm.hover))
            .on_click((_e, cx) => this.act(action, { task: t.number }, cx)))
          .child(div().flex_none().mt(1).w(14).h(14).rounded(3).border_1()
            .border_color(t.checked ? tm.accent : tm.input).bg(t.checked ? tm.accent : tm.background)
            .flex().items_center().justify_center().text_size(10).text_color(tm.accent_foreground ?? tm.background)
            .child(t.checked ? "✓" : ""))
          .child(div().flex_none().w(28).text_color(tm.dim).child(t.number))
          .child(div().flex_1().min_w(0).text_color(t.checked ? tm.muted : tm.foreground).when(t.checked, (el) => el.line_through()).child(t.text));
      }))));
  }

  // agent-activity: Activity on the board. Every activity field, then the
  // current run's ticks as a timeline, newest first.
  activity(c) {
    const tm = theme();
    const ticks = this.detail?.ticks ?? [];
    return v_flex().gap(3).text_size(11.5)
      .when(waitingForYou(c), (el) => el.child(div().pb(2).font_medium().text_color(tm.blocked).child("waiting for you: the agent asks something in its terminal")))
      .children(activityRows(c).map(([k, v], i) => h_flex().id(`activity-${i}`).items_start().gap(8)
        .child(div().flex_none().w(92).text_color(tm.dim).child(k))
        .child(div().flex_1().min_w(0).text_color(tm.foreground).child(v))))
      .when(ticks.length > 0, (el) => el.child(div().pt(6).pb(2).text_size(11).font_medium().text_color(tm.muted).child("Ticks"))
        .children(ticks.map((t, i) => h_flex().id(`tick-${i}`).items_start().gap(8)
          .child(div().flex_none().w(84).text_color(tm.dim).child(fmtBrisbane(t.at)))
          .child(dot(t.ticked ? tm.working : tm.dim, 7).mt(5))
          .child(div().flex_1().min_w(0).text_color(tm.muted).child(`${t.ticked ? "ticked" : "unticked"} ${t.number}${t.step ? ` in ${t.step}` : ""}${t.round ? ` · round ${t.round}` : ""}`))
          .child(div().flex_none().text_size(10.5).text_color(tm.dim).child(ago(t.at))))));
  }

  deps(c) {
    const tm = theme();
    return v_flex().gap(2).children(c.depends_on.map((d, i) => {
      const ok = d.column === "ready" || d.column === "done";
      return h_flex().id(`dep-${i}`).items_center().gap(8).px(6).py(4).rounded(6).text_size(12)
        .child(dot(ok ? tm.working : tm.blocked, 7))
        .child(div().flex_1().min_w(0).truncate().text_color(tm.foreground).child(d.change))
        .child(div().flex_none().text_size(11).text_color(ok ? tm.working : tm.dim).child(columnName(this.board, d.column)));
    }));
  }

  findings(list, key) {
    const tm = theme();
    return v_flex().gap(3).children(list.map((f, i) => h_flex().id(`${key}-f${i}`).items_start().gap(8).text_size(11.5)
      .child(div().flex_none().w(52).text_color(tone(tm, SEVERITY_TONE[f.severity] ?? "dim")).child(f.severity || "note"))
      .child(v_flex().flex_1().min_w(0)
        .when(Boolean(f.file), (el) => el.child(div().text_size(10.5).text_color(tm.dim).truncate().child(f.file)))
        .child(div().text_color(tm.foreground).child(f.detail)))));
  }

  rounds() {
    const tm = theme();
    return v_flex().gap(8).children(this.detail.rounds.map((r) => {
      const bad = r.result === "rejected" || r.result === "blocked" || r.result === "failed";
      return v_flex().id(`round-${r.round}`).gap(5).p(8).rounded(7).bg(tm.surface).border_1().border_color(tm.border)
        .child(h_flex().items_center().gap(8).text_size(12)
          .child(div().font_semibold().text_color(tm.foreground).child(`Round ${r.round}`))
          .when(Boolean(r.step), (el) => el.child(div().text_color(tm.dim).child(r.step)))
          .when(Boolean(r.scope), (el) => el.child(div().text_color(tm.dim).child(`tasks ${r.scope}`)))
          .child(div().text_color(bad ? tm.blocked : r.result ? tm.working : tm.dim).child(r.result || "in progress"))
          .child(div().flex_1())
          .when(Boolean(r.ended), (el) => el.child(div().text_size(11).text_color(tm.dim).child(fmtLocal(r.ended)))))
        .when(Boolean(r.summary), (el) => el.child(div().text_size(11.5).text_color(tm.muted).child(r.summary)))
        .when(r.findings.length > 0, (el) => el.child(this.findings(r.findings, `round-${r.round}`)));
    }));
  }

  history() {
    const tm = theme();
    return v_flex().gap(0).children(newestFirst(this.detail.history).map((h, i) => {
      const bad = h.event === "rejected" || h.event === "blocked" || h.event === "failed";
      return h_flex().id(`hist-${i}`).items_start().gap(8).py(4).text_size(11.5)
        .child(div().flex_none().w(84).text_color(tm.dim).child(fmtLocal(h.at)))
        .child(dot(bad ? tm.blocked : h.event === "ready" ? tm.done : tm.dim, 7).mt(5))
        .child(div().flex_none().w(52).text_color(tm.dim).child(h.round ? `round ${h.round}` : ""))
        .child(div().flex_1().min_w(0).text_color(bad ? tm.foreground : tm.muted).child(eventText(h)))
        .when(Boolean(h.agent), (el) => el.child(div().flex_none().text_size(10.5).text_color(tm.dim).child(h.agent)));
    }));
  }

  editRow(c) {
    const files = [...EDIT_FILES, ...(this.detail?.specs ?? []).map((s) => `spec:${s.capability}`)];
    return h_flex().flex_wrap().gap(6).children(files.map((f) => tiny(`edit-${f}`, f.startsWith("spec:") ? `${f.slice(5)} spec` : f,
      (_e, cx) => this.act("edit", { file: f }, cx))));
  }

  actions(c) {
    const tm = theme();
    const list = detailActions(c, this.board);
    const steps = rerunSteps(c, this.board);
    const groups = runGroups(c, this.board, this.detail);
    return v_flex().gap(6)
      .when(groups.length > 0, (el) => el.child(h_flex().flex_wrap().items_center().gap(6).text_size(11).text_color(tm.dim)
        .child(this.busy === "group" ? "Run group…  starting" : ACTIONS.group.label)
        .children(groups.map((g) => tiny(`group-${g.number}`, `${g.number}. ${g.title} (${g.ticked}/${g.total})`,
          (_e, cx) => this.act("group", { group: g.number }, cx), { disabled: Boolean(this.busy) })))))
      .child(h_flex().flex_wrap().gap(6).items_center().children(list.map((a) => {
        const extra = a === "rerun" || a === "follow" ? { step: a === "follow" ? c.step : this.step } : {};
        return button(`act-${a}`, this.busy === a ? `${actionLabel(a, extra)}…` : actionLabel(a, extra), (_e, cx) => this.act(a, extra, cx),
          { primary: ACTIONS[a].primary, danger: ACTIONS[a].danger, disabled: Boolean(this.busy) });
      })))
      .when(list.includes("rerun") && steps.length > 1, (el) => el.child(h_flex().items_center().gap(6).text_size(11).text_color(tm.dim)
        .child("Re-run from")
        .children(steps.map((s) => tiny(`step-${s}`, s, (_e, cx) => { this.step = s; cx.notify(); }, { primary: s === this.step })))));
  }

  details() {
    const tm = theme();
    const c = this.card;
    const d = this.detail;
    const col = columnName(this.board, c.column);
    return v_flex().size_full()
      .child(h_flex().items_center().gap(8)
        .child(v_flex().flex_1().min_w(0)
          .child(div().truncate().text_size(15).font_semibold().text_color(tm.foreground).child(c.title))
          .child(div().text_size(11).text_color(tm.dim).child(c.change)))
        .child(stateChip(c) ?? div())
        .child(closeButton("close", () => this.dismiss())))
      .child(h_flex().pt(6).items_center().gap(10).text_size(11.5)
        .child(div().px(7).py(2).rounded(5).bg(tm.raised).text_color(tm.muted).child(col))
        .when(Boolean(c.step), (el) => el.child(div().text_color(tm.muted).child(`${c.step}${c.round ? ` · round ${c.round}` : ""}`)))
        .when(Boolean(c.scope), (el) => el.child(div().px(6).py(1).rounded(5).bg(tm.raised).text_color(tm.muted).child(`tasks ${c.scope.spec}`)))
        .when(Boolean(c.agent), (el) => el.child(div().text_color(tm.dim).child(c.agent)))
        .when(c.tasks.total > 0, (el) => el.child(progressBar(c.tasks, 70)))
        .child(div().flex_1())
        .when(Boolean(c.machine), (el) => el.child(div().text_color(tm.dim).child(c.machine))))
      // modes-board: who holds the change, and in fleet mode its epoch and last renewal.
      .when(Boolean(claimLine(c, this.board)), (el) => el.child(h_flex().id("claim").pt(4).gap(8).text_size(11.5)
        .text_color(c.claim.state === "expired" ? tm.blocked : tm.muted)
        .child(div().child(`claim: ${claimLine(c, this.board)}`))
        .when(c.claim.epoch > 0, (el) => el.child(div().text_color(tm.dim).child(`epoch ${c.claim.epoch}`)))
        .when(Boolean(c.claim.renewed_at), (el) => el.child(div().text_color(tm.dim).child(`renewed ${fmtLocal(c.claim.renewed_at)}`)))))
      .when(Boolean(c.mr), (el)=>el.child(h_flex().items_center().gap(6).text_size(11)
        .child(div().id(`mr-link-${c.change}`).cursor_pointer().text_color(tm.accent).hover(s=>s.underline())
          .on_mouse_down("left",(_e,cx)=>cx.stop_propagation())
          .on_click((_e,cx)=>{cx.stop_propagation();this.act("mr", {}, cx);}).child(`#${c.mr.number}`))
        .when(Boolean(c.ci),(el)=>el.child(div().px(5).rounded(4).bg(tm.raised).text_color(c.ci.state==="red"?tm.blocked:c.ci.state==="green"?tm.working:tm.dim).child(`CI ${c.ci.state}`)))
        .when(c.ship_round>0,(el)=>el.child(div().text_color(tm.dim).child(`ship round ${c.ship_round}`)))))
      .when(Boolean(c.reason), (el) => el.child(div().pt(6).text_size(12).text_color(c.state === "blocked" || c.state === "failed" ? tm.blocked : tm.muted).child(c.reason)))
      .when(answerable(c), (el) => el.child(section("Question")).child(this.question(c)))
      .when(showsApproval(c), (el) => el.child(div().pt(8).child(this.approvalRow(c))))
      .when(Boolean(this.validationBox(c)), (el) => el.child(div().pt(8).child(this.validationBox(c))))
      .when(Boolean(this.error), (el) => el.child(div().pt(8).child(note("error", this.error, "error"))))
      .when(Boolean(this.notice), (el) => el.child(div().pt(8).child(note("notice", this.notice.text, this.notice.kind))))
      .when(Boolean(this.pending), (el) => el.child(div().pt(8).child(this.confirmBox())))
      .child(v_flex().id("card-scroll").flex_1().min_h(0).overflow_y_scrollbar().pr(4)
        .when(!d && !this.error, (el) => el.child(div().pt(14).text_size(12).text_color(tm.dim).child("Reading the change…")))
        .when(Boolean(d), (el) => el
          .child(section("Proposal"))
          .child(d.proposal ? TextView.markdown("proposal", d.proposal).selectable(true).text_size(12.5) : div().text_size(12).text_color(tm.dim).child("No proposal.md."))
          .when(Boolean(c.activity), (el) => el.child(section("Activity")).child(this.activity(c)))
          .child(section("Tasks", c.tasks.total ? progressBar(c.tasks, 80) : null))
          .when(isActive(c), (el) => el.child(div().pb(4).text_size(11).text_color(tm.dim).child("Ticking waits until the run stops: it holds the change.")))
          .child(d.tasks.length ? this.tasks(c) : div().text_size(12).text_color(tm.dim).child("No tasks."))
          .when(c.settings.length > 0, (el) => el.child(section("Settings")).child(this.settings(c)))
          .when(c.depends_on.length > 0, (el) => el.child(section("Depends on")).child(this.deps(c)))
          .when(d.rounds.length > 0, (el) => el.child(section("Rounds")).child(this.rounds()))
          .when(canEdit(c), (el) => el.child(section("Edit")).child(this.editRow(c)))
          .child(section("History"))
          .child(d.history.length ? this.history() : div().text_size(12).text_color(tm.dim).child("No runs yet."))))
      .child(div().pt(12).child(this.actions(c)));
  }

  render() {
    const tm = theme();
    return v_flex().id("card-view").track_focus(this.handle)
      .on_key_down((e, cx) => { if (e.key === "escape") { this.dismiss(); cx.stop_propagation(); } })
      .size_full().p(18).bg(tm.popover).text_color(tm.foreground).text_size(12.5)
      .child(this.card ? this.details() : div().text_size(12).text_color(tm.dim).child(`No card for ${this.change}.`));
  }
}
