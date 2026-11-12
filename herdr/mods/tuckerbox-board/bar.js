// Top-bar entry points follow the same candidate key and stale-read rules as
// the drawer. Only this view can offer init in an unconfigured repository.
import { View, div } from "gpui-kit";
import { h_flex, v_flex } from "gpui-base";
import { props, settings, layout, run, openOverlay, closeOverlay, shared, setShared, terminals, theme } from "herdr";
import { MOD, candidates, locate, loadAll } from "./data.js";
import { counts, candidatesKey, DRAWER_POLL_MS } from "./model.js";
import { toggleCall, windowSpec } from "./actions.js";

const MENU = "tuckerbox.bar.menu";
export function findLayout(root, id) {
  if (root?.id === id) return root;
  for (const child of root?.children ?? []) { const found = findLayout(child, id); if (found) return found; }
  return null;
}
function toggle() { const c = toggleCall(MOD); run(c.command, c.args); }
function zoom() { return Number(settings()?.zoom) > 0 ? Number(settings().zoom) : 1; }
function control(id, label, action) {
  return div().id(id).px(8).py(5).text_size(12).cursor_pointer()
    .on_mouse_down("left", (_e, cx) => cx.stop_propagation()).on_click(action).child(label);
}

export class BarView extends View {
  init(p, cx) {
    this.at = p?.at ?? null;
    this.counts = p?.counts ?? null;
    this.last = this.at ? Date.now() : 0;
    this.want = candidatesKey(candidates(null));
    this.loading = new Set();
    this.width = null;
    cx.timer.after(30, (cx) => this.refresh(cx));
    cx.timer.every(1000, (cx) => this.tick(cx));
  }
  resize(width) {
    const px = Math.ceil(width / zoom());
    if (px === this.width) return;
    this.width = px;
    run("layout.resize", { id: props()?.block ?? "tuckerbox-bar", px });
  }
  follow(key, cx) {
    this.want = key; this.at = null; this.counts = null; this.last = 0; this.setup = null;
    this.resize(0); cx.notify();
  }
  tick(cx) {
    const key = candidatesKey(candidates(null));
    if (key !== this.want) this.follow(key, cx);
    if (!this.last || Date.now() - this.last >= (this.setup ? 2000 : DRAWER_POLL_MS)) this.refresh(cx);
  }
  refresh(cx) {
    const c = candidates(null), key = candidatesKey(c);
    if (key !== this.want) this.follow(key, cx);
    if (this.loading.has(key)) return;
    this.loading.add(key);
    cx.spawn(async (cx) => {
      let at = null, n = null;
      try {
        at = await locate(null, c, { includeUnconfigured: true, fresh: Boolean(this.setup) });
        if (at.found) { const b = await loadAll(at); n = b.error ? { error: b.error.message } : counts(b); }
      } catch { /* an unavailable machine has no actionable entry point */ }
      this.loading.delete(key);
      if (key !== this.want) return;
      this.at = at; this.counts = n; this.last = Date.now();
      if (this.setup) {
        const terminal = terminals().find((t) => t.name === this.setup.name);
        // Herdr keeps finished terminals and their exit code. Do not open a
        // board just because a failing init happened to leave a file behind.
        if (terminal) this.setup.seen = true;
        if (terminal?.state === "finished") {
          if (terminal.exit_code === 0 && at?.found) { openOverlay(windowSpec(MOD)); this.setup = null; }
          else if (terminal.exit_code !== 0) this.setup = null;
          else {
            this.setup.finished ??= Date.now();
            if (Date.now() - this.setup.finished > 10000) this.setup = null;
          }
        } else if (!terminal && (this.setup.seen || Date.now() - this.setup.started > 10000)) this.setup = null;
      }
      if (!at?.root) this.resize(0);
      cx.notify();
    });
  }
  startSetup(cx) {
    if (this.setup || !this.at?.root) return;
    const name = `tb-init-${Date.now()}`;
    this.setup = { name, started: Date.now(), seen: false };
    // terminal.open uses the shown space's machine; never execute a local
    // init for a remote space. Its pane retains init's output on failure.
    run("terminal.open", { argv: ["tuckerbox", "init"], folder: this.at.root, place: "right", name });
    this.last = 0; cx.notify();
  }
  menu(bounds) {
    const current = shared(MENU);
    if (current?.id) { closeOverlay(current.id); setShared(MENU, { closed: Date.now() }); return; }
    // Herdr dismisses a popup on mouse down, before the trigger's click.
    if (Date.now() - (current?.closed ?? 0) < 200) return;
    const z = zoom(), b = bounds ?? this.bounds;
    if (!b) return;
    const token = `${Date.now()}-${props()?.block ?? "bar"}`;
    const id = openOverlay({ mod: MOD, kind: "popup", width: 220, height: findLayout(layout(), "tuckerbox-panel") ? 120 : 84,
      at: { x: b.x + b.width - 220 * z, y: b.y + b.height + 5 * z }, props: { view: "bar-menu", token } });
    setShared(MENU, { id, token });
  }
  render() {
    if (!this.at?.root) { this.resize(0); return div(); }
    const root = h_flex().id("tuckerbox-bar-content").items_center().gap(3)
      .on_size_change((size) => this.resize(size.width));
    if (!this.at.found) return root.child(control("tuckerbox-setup", "Set up Tuckerbox", (_e, cx) => this.startSetup(cx)));
    return root.child(control("tuckerbox-toggle", "Tuckerbox", toggle))
      .when(Boolean(this.counts?.running), (el) => el.child(div().id("bar-running").rounded(4).px(4).text_color(theme().working).tooltip("Running changes").text_size(12).child(String(this.counts.running))))
      .when(Boolean(this.counts?.needs), (el) => el.child(div().id("bar-needs").rounded(4).px(4).text_color(theme().blocked).tooltip("Changes that need you").text_size(12).child(String(this.counts.needs))))
      .when(Boolean(this.counts?.error), (el) => el.child(div().child("!").tooltip(this.counts.error)))
      .child(control("tuckerbox-menu", "▾", () => this.menu())
        .on_mouse_down("left", (e, cx) => { this.bounds = e.bounds; cx.stop_propagation(); }));
  }
}

export class BarMenuView extends View {
  init() { this.token = props()?.token; }
  close() {
    if (shared(MENU)?.token === this.token) setShared(MENU, { closed: Date.now() });
  }
  choose(action) {
    action();
    closeOverlay(props()?.overlay);
    this.close();
  }
  panel() {
    const root = layout(), panel = findLayout(root, "panel"), stack = findLayout(root, "panel-body");
    if (panel && !panel.hidden && stack?.active === "tuckerbox-panel") run("layout.hide", { id: "panel" });
    else { run("layout.select", { stack: "panel-body", child: "tuckerbox-panel" }); run("layout.show", { id: "panel" }); }
  }
  render() {
    const item = (id, label, action) => control(id, label, () => this.choose(action));
    return v_flex().id("tuckerbox-bar-menu").p(4)
      .child(item("bar-board", "Board", toggle))
      .when(Boolean(findLayout(layout(), "tuckerbox-panel")), (el) => el.child(item("bar-panel", "Board in side panel", () => this.panel())))
      .child(item("bar-settings", "Settings", () => openOverlay({ ...windowSpec(MOD), props: { start: "settings" } })));
  }
}
