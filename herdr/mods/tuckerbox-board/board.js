// The board: a header with the repository and its counts, then one column per
// stage (derived by tuckerbox, never stored here) holding the change cards.
import { View, div, svg } from "gpui-kit";
import { v_flex, h_flex } from "gpui-base";
import { theme, props, openOverlay, closeOverlay, spaces, panes, focused } from "herdr";
import { MOD, loadBoard, where } from "./data.js";
import { group, counts, columnName, due, isShown, ago, needsYou } from "./model.js";
import { cardActions, invocation, actionLabel, defaultStep, ACTIONS } from "./actions.js";
import { perform, watchEdits, changed } from "./perform.js";
import { editOf, editNote } from "./edits.js";
import { button, tiny, dot, stateChip, approvalChip, progressBar, note, closeButton, SPINNER } from "./ui.js";

const COL_W = 236;

export class BoardView extends View {
  init(p, cx) {
    // `board` may be handed in (a test harness); otherwise it is read.
    this.board = p?.board ?? null;
    this.last = this.board ? Date.now() : 0;
    this.loading = false;
    this.notice = null; // { text, kind } from the last action
    this.handle = cx.focus_handle();
    // The first read from a timer: a spawn straight from init can stall in an overlay.
    cx.timer.after(30, (cx) => this.refresh(cx));
    this.seen = changed();
    cx.timer.every(1000, (cx) => {
      // Edits whose pane closed are validated here (and by any open dialog).
      const validating = watchEdits();
      if (validating.length) cx.spawn(async (cx) => { await Promise.all(validating); cx.notify(); });
      // An action in this or another copy changed something: read again now.
      if (changed() !== this.seen) { this.seen = changed(); this.last = 0; }
      if (due(this.last, Date.now(), this.shown())) this.refresh(cx);
    });
  }

  // This copy's pane: Herdr's `pane` prop, else the board pane that is
  // focused while this copy holds the keyboard (a mod pane keeps it when clicked).
  self() {
    if (props()?.pane) return props().pane;
    if (!this.pane && this.handle.is_focused()) {
      const f = focused();
      if (f?.pane && panes().some((p) => p.pane === f.pane && p.mod === MOD)) this.pane = f.pane;
    }
    return this.pane ?? null;
  }

  shown() {
    return isShown({ overlay: props()?.overlay, self: this.self(), spaces: spaces(), panes: panes(), mod: MOD });
  }

  refresh(cx) {
    if (this.loading) return;
    this.loading = true;
    const at = where(this.self());
    cx.spawn(async (cx) => {
      let b;
      try { b = await loadBoard(at); } catch (e) { b = { error: { code: "failed", message: String(e?.message ?? e) } }; }
      this.loading = false;
      this.last = Date.now();
      this.at = at;
      // Keep the last good board on screen when a later read fails.
      if (b.error && this.board && !this.board.error) this.readError = b.error.message;
      else { this.board = b; this.readError = null; }
      cx.notify();
    });
  }

  act(action, card, extra, cx) {
    if (this.busy) return;
    let inv;
    try { inv = invocation(action, card ?? { change: "", path: this.at?.cwd ?? "", on: this.at?.on ?? null }, extra); }
    catch (e) { this.notice = { text: String(e?.message ?? e), kind: "error" }; cx.notify(); return; }
    if (action === "init") { inv.cwd = this.at?.cwd ?? ""; inv.on = this.at?.on ?? null; }
    inv.card = card;
    inv.board = this.boardProps();
    this.busy = `${action}-${card?.change ?? ""}`;
    cx.notify();
    cx.spawn(async (cx) => {
      const r = await perform(inv, { dry: Boolean(this.board?.fixtures) });
      this.busy = null;
      this.notice = r.message ? { text: r.message, kind: r.ok || r.dry ? "info" : "error", change: card?.change ?? "" } : null;
      if (r.ok) this.last = 0; // read again at once: the card moves when tuckerbox says so
      cx.notify();
    });
  }

  boardProps() {
    const b = this.board ?? {};
    return { repo: b.repo, path: b.path, require_approval: b.require_approval, columns: b.columns, on: b.on, fixtures: Boolean(b.fixtures) };
  }

  openCard(card) {
    const b = this.board;
    openOverlay({
      mod: MOD, kind: "dialog", width: 760, height: 780,
      props: {
        view: "card", change: card.change, card,
        board: this.boardProps(),
        fixtures: props()?.fixtures ?? null,
      },
    });
  }

  // ---- drawing ----

  card(c) {
    const tm = theme();
    const b = this.board;
    const flag = needsYou(c);
    const acts = cardActions(c, b);
    const step = defaultStep(c, b);
    return v_flex().id(`card-${c.change}`).w_full().p(10).gap(7).rounded(8).flex_none()
      .bg(tm.surface).border_1().border_color(flag ? tm.blocked : tm.border)
      .cursor_pointer().hover((s) => s.border_color(flag ? tm.blocked : tm.accent))
      .on_click(() => this.openCard(c))
      .child(h_flex().items_start().gap(8)
        .child(v_flex().flex_1().min_w(0)
          .child(div().text_size(12.5).font_medium().text_color(tm.foreground).child(c.title))
          .when(c.title !== c.change, (el) => el.child(div().text_size(10.5).text_color(tm.dim).truncate().child(c.change))))
        .child(stateChip(c) ?? div()))
      .child(h_flex().items_center().gap(6)
        .child(approvalChip(c.approval, { short: true }))
        .when(!c.approval.committed && c.approval.state !== "none", (el) => el.child(div().text_size(10.5).text_color(tm.blocked).child("not committed")))
        .child(div().flex_1())
        .when(c.tasks.total > 0, (el) => el.child(progressBar(c.tasks, 46))))
      .when(Boolean(c.step || c.agent), (el) => el.child(h_flex().items_center().gap(6).text_size(10.5).text_color(tm.dim)
        .when(Boolean(c.step), (el) => el.child(div().text_color(tm.muted).child(c.step)))
        .when(c.round > 1, (el) => el.child(div().child(`round ${c.round}`)))
        .when(Boolean(c.agent), (el) => el.child(div().child("·")).child(div().child(c.agent)))
        .child(div().flex_1())
        .when(Boolean(c.machine), (el) => el.child(div().truncate().max_w(90).child(c.machine)))))
      .when(Boolean(c.reason), (el) => el.child(div().text_size(11).text_color(flag ? tm.blocked : tm.dim).child(c.reason)))
      .when(c.depends_on.length > 0 && !c.archived, (el) => el.child(div().text_size(11).text_color(tm.dim)
        .child(`after ${c.depends_on.map((d) => `${d.change} (${columnName(b, d.column)})`).join(", ")}`)))
      .when(Boolean(c.branch), (el) => el.child(div().text_size(10.5).text_color(tm.dim).truncate().child(`⎇ ${c.branch}`)))
      .when(Boolean(editNote(editOf(c))), (el) => { const n = editNote(editOf(c)); return el.child(div().text_size(11).text_color(tm[n.tone] ?? tm.dim).child(n.text)); })
      .child(h_flex().items_center().gap(6).text_size(10.5).text_color(tm.dim)
        .child(div().child(c.archived ? `archived ${c.archived}` : c.since ? ago(c.since) : ""))
        .child(div().flex_1())
        .children(acts.map((a) => {
          const extra = a === "rerun" || a === "follow" ? { step } : {};
          return tiny(`${a}-${c.change}`, actionLabel(a, extra), (_e, cx) => this.act(a, c, extra, cx),
            { primary: ACTIONS[a].primary, danger: ACTIONS[a].danger, disabled: Boolean(this.busy) });
        })));
  }

  column(col) {
    const tm = theme();
    return v_flex().id(`col-${col.id}`).w(COL_W).flex_none().h_full().rounded(10).p(6).gap(6)
      .bg(tm.chrome).border_1().border_color(tm.border)
      .child(h_flex().items_center().px(6).pt(4).pb(2).gap(6)
        .child(div().text_size(12).font_semibold().text_color(tm.foreground).child(col.name))
        .child(div().text_size(11).text_color(tm.dim).child(String(col.cards.length)))
        .child(div().flex_1())
        .when(col.cards.some(needsYou), (el) => el.child(dot(tm.blocked, 7))))
      .child(v_flex().id(`col-list-${col.id}`).flex_1().min_h(0).gap(6).overflow_y_scrollbar()
        .children(col.cards.map((c) => this.card(c)))
        .when(col.cards.length === 0, (el) => el.child(div().p(10).text_size(11.5).text_color(tm.dim).text_center().child("—"))));
  }

  header() {
    const tm = theme();
    const b = this.board && !this.board.error ? this.board : null;
    const n = counts(b);
    const overlay = props()?.overlay;
    const repo = b?.repo || this.at?.cwd || "";
    return h_flex().items_center().gap(10).px(14).h(46).flex_none().border_b(1).border_color(tm.border)
      .child(div().text_size(15).font_semibold().text_color(tm.foreground).child("Tuckerbox"))
      .child(div().text_size(12.5).text_color(tm.muted).truncate().max_w(320).child(repo))
      .when(Boolean(b), (el) => el.child(div().text_size(11.5).text_color(tm.dim).child(`${n.total} change${n.total === 1 ? "" : "s"}`)))
      .when(n.running > 0, (el) => el.child(h_flex().items_center().gap(5).text_size(11.5).text_color(tm.working)
        .child(svg(SPINNER).size(11).text_color(tm.working).spin(900)).child(`${n.running} running`)))
      .when(n.needs > 0, (el) => el.child(h_flex().items_center().gap(5).text_size(11.5).text_color(tm.blocked)
        .child(dot(tm.blocked, 8)).child(`${n.needs} need${n.needs === 1 ? "s" : ""} you`)))
      .when(Boolean(b && !b.require_approval), (el) => el.child(div().text_size(11).text_color(tm.dim).child("approval off")))
      .when(Boolean(this.readError), (el) => el.child(div().text_size(11).text_color(tm.blocked).truncate().max_w(260).tooltip(this.readError).child("last read failed")))
      .child(div().flex_1())
      .when(Boolean(b?.machine), (el) => el.child(div().text_size(11).text_color(tm.dim).child(b.machine)))
      .when(Boolean(overlay), (el) => el.child(closeButton("close", () => closeOverlay(overlay))));
  }

  empty(title, text, action) {
    const tm = theme();
    return div().flex_1().flex().items_center().justify_center().child(
      v_flex().items_center().gap(8).p(20).max_w(460).rounded(10).bg(tm.surface).border_1().border_color(tm.border)
        .child(div().text_size(14).font_semibold().text_color(tm.foreground).child(title))
        .child(div().text_size(12).text_color(tm.muted).text_center().child(text))
        .when(Boolean(action), (el) => el.child(action)));
  }

  body() {
    const b = this.board;
    if (!b) return this.empty("Reading the board", "Running tuckerbox board --json in this space's folder.");
    if (b.error?.code === "not_a_repository") {
      return this.empty("Not a Tuckerbox repository", `${this.at?.cwd || "This space's folder"} has no Tuckerbox config. ${b.error.message}`.trim(),
        button("init", "Run tuckerbox init here", (_e, cx) => this.act("init", null, {}, cx), { primary: true }));
    }
    if (b.error) {
      return this.empty("Can't read the board", b.error.message,
        button("retry", "Try again", (_e, cx) => { this.last = 0; this.refresh(cx); }));
    }
    return h_flex().id("columns").flex_1().min_h(0).p(12).gap(10).overflow_x_scrollbar()
      .children(group(b).map((c) => this.column(c)));
  }

  render() {
    const tm = theme();
    this.self();
    return v_flex().id("tuckerbox-board").size_full().bg(tm.canvas).text_color(tm.foreground)
      .track_focus(this.handle)
      .on_key_down((e, cx) => { if (e.key === "escape" && props()?.overlay) { closeOverlay(props().overlay); cx.stop_propagation(); } })
      .child(this.header())
      .when(Boolean(this.notice), (el) => el.child(h_flex().px(14).pt(8).gap(8).items_center()
        .child(div().flex_1().child(note("notice", this.notice.text, this.notice.kind)))
        .child(tiny("notice-x", "Dismiss", (_e, cx) => { this.notice = null; cx.notify(); }))))
      .child(this.body());
  }
}
