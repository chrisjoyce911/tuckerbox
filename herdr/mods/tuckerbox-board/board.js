// The board: a header with the repository and its counts, then one column per
// stage (derived by tuckerbox, never stored here) holding the change cards.
import { View, div, svg } from "gpui-kit";
import { v_flex, h_flex } from "gpui-base";
import { theme, props, openOverlay, closeOverlay, spaces, panes, focused, run, notify } from "herdr";
import { MOD, loadAll, locate, candidates, fixturesFolder } from "./data.js";
import { group, counts, columnName, due, isShown, ago, needsYou, candidatesKey, openGate, NO_CONFIG, sortedColumn, dropPosition, eventBox, liveDrop, priorityCard } from "./model.js";
import { cardActions, invocation, actionLabel, defaultStep, ACTIONS, dockCall, undockCalls } from "./actions.js";
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
      // The space or its folders changed (a window follows the shown space): look again now.
      const key = candidatesKey(candidates(this.self()));
      if (key !== this.where) { this.where = key; this.last = 0; }
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
    const self = this.self();
    cx.spawn(async (cx) => {
      let b, at;
      try {
        // The fixtures need no repository; otherwise find this space's (herdr-board: Which repository).
        at = (await fixturesFolder()) ? { found: true, root: "", cwd: "", on: null, space: null } : await locate(self);
        b = at.found ? await loadAll(at) : { error: { code: "no_config", message: "" } };
      } catch (e) { b = { error: { code: "failed", message: String(e?.message ?? e) } }; }
      this.loading = false;
      this.last = Date.now();
      const same = at && this.at && at.root === this.at.root && at.on === this.at.on;
      this.at = at ?? this.at;
      if (b.error?.code === "not_a_repository") b = { error: { code: "no_config", message: b.error.message } };
      const gate = openGate({ overlay: props()?.overlay, everFound: this.everFound, found: !b.error || b.error.code !== "no_config" });
      if (gate === "close") {
        // Opened (key, drawer, mod.open) where there is no Tuckerbox config: no board.
        closeOverlay(props().overlay);
        notify({ title: NO_CONFIG, level: "info", timeout: 3000 });
        return;
      }
      if (gate === "board") this.everFound = true;
      // Keep the last good board of the same repository on screen when a later read fails.
      if (b.error && b.error.code !== "no_config" && same && this.board && !this.board.error) this.readError = b.error.message;
      else { this.board = b; this.readError = null; }
      cx.notify();
    });
  }

  act(action, card, extra, cx) {
    if (this.busy) return;
    let inv;
    try { inv = invocation(action, card ?? { change: "", path: this.at?.cwd ?? "", on: this.at?.on ?? null }, extra); }
    catch (e) { this.notice = { text: String(e?.message ?? e), kind: "error" }; cx.notify(); return; }
    inv.card = card;
    inv.board = this.boardProps();
    this.busy = `${action}-${card?.change ?? ""}`;
    cx.notify();
    cx.spawn(async (cx) => {
      const r = await perform(inv, { dry: Boolean(this.board?.fixtures),openURL:(url)=>cx.open_url(url) });
      this.busy = null;
      this.notice = r.message ? { text: r.message, kind: r.ok || r.dry ? "info" : "error", change: card?.change ?? "" } : null;
      if (r.ok) this.last = 0; // read again at once: the card moves when tuckerbox says so
      cx.notify();
    });
  }

  boardProps() {
    const b = this.board ?? {};
    return { ship:b.ship,repo: b.repo, path: b.path, require_approval: b.require_approval, columns: b.columns, steps: b.steps, on: b.on, fixtures: Boolean(b.fixtures) };
  }

  // Dock: this window becomes a pane beside the shown space's terminals,
  // pinned to the repository it shows; the window closes.
  dock(cx) {
    try {
      const call = dockCall({ root: this.at?.root, on: this.at?.on ?? null, space: this.at?.space ?? focused()?.space,
        spaces: spaces(), panes: panes(), focused: focused(), mod: MOD });
      run(call.command, call.args);
      if (props()?.overlay) closeOverlay(props().overlay);
    } catch (e) { this.notice = { text: String(e?.message ?? e), kind: "error" }; cx.notify(); }
  }

  // Undock: back to the window, and this pane goes. Close: only the pane goes.
  undock(cx, keepWindow) {
    try {
      const u = undockCalls(this.self(), MOD);
      if (keepWindow) openOverlay(u.overlay);
      run(u.close.command, u.close.args);
    } catch (e) { this.notice = { text: String(e?.message ?? e), kind: "error" }; cx.notify(); }
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

  // A release over a card of the same sorted column runs tuckerbox priority
  // <change> <n> on the board's machine, in its checkout, whichever machine
  // the card's run is on; the card moves when the next read shows it. A
  // release anywhere else cancels the drag.
  endDrag(e, cx) {
    const { drag, dragColumn } = this;
    const drop = liveDrop(this.drop, e?.position);
    this.press = null; this.drag = null; this.drop = null; this.dragColumn = null;
    if (!drag) return;
    this.dragged = true; // the release is not a click on the card
    cx.notify();
    if (!drop || !this.board || this.board.error) return;
    const col = group(this.board).find((c) => c.id === dragColumn);
    const n = dropPosition(col?.cards ?? [], drag, drop.over, drop.below);
    const card = col?.cards.find((c) => c.change === drag);
    if (n && card) this.act("priority", priorityCard(card, this.board), { position: n }, cx);
  }

  // The pointer moved while dragging: over a card of the dragged card's
  // column it becomes the target; anywhere else the old target goes.
  dragMove(e, cx, c) {
    if (!this.drag) return;
    if (c && this.dragColumn === c.column) {
      const below = e.local_position.y > (e.bounds?.height ?? 0) / 2;
      if (this.drop?.over !== c.change || this.drop?.below !== below) { this.drop = { over: c.change, below, box: eventBox(e) }; cx.notify(); }
      else this.drop.box = eventBox(e);
      return;
    }
    if (this.drop && !liveDrop(this.drop, e.position)) { this.drop = null; cx.notify(); }
  }

  // ---- drawing ----

  card(c) {
    const tm = theme();
    const b = this.board;
    const flag = needsYou(c);
    const acts = cardActions(c, b);
    const step = defaultStep(c, b);
    const dragging = this.drag === c.change;
    const target = this.drop?.over === c.change ? this.drop : null;
    return v_flex().id(`card-${c.change}`).w_full().p(10).gap(7).rounded(8).flex_none()
      .bg(tm.surface).border_1().border_color(target || dragging ? tm.accent : flag ? tm.blocked : tm.border)
      .when(dragging, (el) => el.opacity(0.4))
      .when(Boolean(target), (el) => (target.below ? el.border_b(3) : el.border_t(3)))
      .cursor_pointer().hover((s) => s.border_color(flag ? tm.blocked : tm.accent))
      // Drag to reorder (change-priority: Drag to reorder): a press starts it
      // once the pointer moves; the card under the pointer, and which half of
      // it, says where it lands.
      .when(sortedColumn(c.column) && c.position > 0, (el) => el
        .on_mouse_down("left", (e) => { this.press = { change: c.change, column: c.column, x: e.position.x, y: e.position.y }; })
        .on_mouse_move((e, cx) => this.dragMove(e, cx, c)))
      .on_click(() => { if (!this.dragged) this.openCard(c); this.dragged = false; })
      .child(h_flex().items_start().gap(8)
        .child(v_flex().flex_1().min_w(0)
          .child(div().text_size(12.5).font_medium().text_color(tm.foreground).child(c.title))
          .when(c.title !== c.change, (el) => el.child(div().text_size(10.5).text_color(tm.dim).truncate().child(c.change))))
        .when(c.position > 0, (el) => el.child(div().text_size(10.5).text_color(tm.dim).tooltip(c.priority ? `priority ${c.priority}` : "no priority set").child(`#${c.position}`)))
        .child(stateChip(c) ?? div()))
      .child(h_flex().items_center().gap(6)
        .child(approvalChip(c.approval, { short: true }))
        .when(!c.approval.committed && c.approval.state !== "none", (el) => el.child(div().text_size(10.5).text_color(tm.blocked).child("not committed")))
        .child(div().flex_1())
        .when(c.tasks.total > 0, (el) => el.child(progressBar(c.tasks, 46))))
      .when(Boolean(c.badge || c.step || c.agent || c.scope), (el) => el.child(h_flex().items_center().gap(6).text_size(10.5).text_color(tm.dim)
        .when(Boolean(c.badge || c.step), (el) => el.child(div().px(5).rounded(4).bg(tm.raised).text_color(tm.muted).child(c.badge || c.step)))
        .when(c.round > 1, (el) => el.child(div().child(`round ${c.round}`)))
        .when(Boolean(c.scope), (el) => el.child(div().child(`tasks ${c.scope.spec}`)))
        .when(Boolean(c.agent), (el) => el.child(div().child("·")).child(div().child(c.agent)))
        .child(div().flex_1())
        .when(Boolean(c.machine), (el) => el.child(div().truncate().max_w(90).child(c.machine)))))
      .when(Boolean(c.mr), (el)=>el.child(h_flex().items_center().gap(6).text_size(11)
        .child(div().id(`mr-link-${c.change}`).cursor_pointer().text_color(tm.accent).hover(s=>s.underline())
          .on_mouse_down("left",(_e,cx)=>cx.stop_propagation())
          .on_click((_e,cx)=>{cx.stop_propagation();this.act("mr", c, {}, cx);}).child(`#${c.mr.number}`))
        .when(Boolean(c.ci),(el)=>el.child(div().px(5).rounded(4).bg(tm.raised).text_color(c.ci.state==="red"?tm.blocked:c.ci.state==="green"?tm.working:tm.dim).child(`CI ${c.ci.state}`)))
        .when(c.ship_round>0,(el)=>el.child(div().text_color(tm.dim).child(`ship round ${c.ship_round}`)))))
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
      // Machines that couldn't be asked: named, their cards missing, the others shown.
      .children((b?.unreachable ?? []).map((u, i) => div().id(`unreachable-${i}`).text_size(11).text_color(tm.blocked)
        .tooltip(u.message).child(`${u.machine ?? "this computer"} unreachable`)))
      .when(Boolean(b?.machine), (el) => el.child(div().text_size(11).text_color(tm.dim).child(b.machine)))
      // Dock only with a board to show: no Tuckerbox config, no Dock.
      .when(Boolean(overlay && b && !b.fixtures), (el) => el.child(tiny("dock", "Dock ⇥", (_e, cx) => this.dock(cx), { tooltip: "Show the board as a pane beside the terminals" })))
      .when(Boolean(!overlay && this.self()), (el) => el
        .child(tiny("undock", "Undock", (_e, cx) => this.undock(cx, true), { tooltip: "Back to the board window" }))
        .child(closeButton("close-pane", (_e, cx) => this.undock(cx, false))))
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
    if (!b) return this.empty("Reading the board", "Looking for a Tuckerbox repository in this space.");
    if (b.error?.code === "no_config") {
      return this.empty(NO_CONFIG, "The board shows a git repository with .tuckerbox/config.toml, found from this space's terminals.");
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
      .on_mouse_move((e, cx) => {
        if (this.drag) { this.dragMove(e, cx, null); return; }
        if (!this.press) return;
        if (Math.hypot(e.position.x - this.press.x, e.position.y - this.press.y) > 6) {
          this.drag = this.press.change; this.dragColumn = this.press.column; this.drop = null; cx.notify();
        }
      })
      .on_mouse_up("left", (e, cx) => this.endDrag(e, cx))
      .on_key_down((e, cx) => { if (e.key === "escape" && props()?.overlay) { closeOverlay(props().overlay); cx.stop_propagation(); } })
      .child(this.header())
      .when(Boolean(this.notice), (el) => el.child(h_flex().px(14).pt(8).gap(8).items_center()
        .child(div().flex_1().child(note("notice", this.notice.text, this.notice.kind)))
        .child(tiny("notice-x", "Dismiss", (_e, cx) => { this.notice = null; cx.notify(); }))))
      .child(this.body());
  }
}
