// Small pieces both views draw the same way. Theme colors only.
import { div, svg } from "gpui-kit";
import { h_flex } from "gpui-base";
import { theme } from "herdr";
import { MOD } from "./data.js";
import { approvalBadge, stateLook, progress } from "./model.js";

export const SPINNER = `mods/${MOD}/spinner.svg`;

export const tone = (t, name) => t[name] ?? t.foreground;

export function button(id, text, action, opts = {}) {
  const t = theme();
  const { primary, danger, disabled } = opts;
  return h_flex().id(id).h(opts.h ?? 28).px(opts.px ?? 10).items_center().gap(6).rounded(7).flex_none()
    .border_1().border_color(primary ? t.solid : danger ? t.blocked : t.input)
    .bg(primary ? t.solid : t.raised)
    .text_color(primary ? t.solid_foreground : danger ? t.blocked : t.foreground)
    .text_size(opts.size ?? 12).font_medium()
    .when(Boolean(disabled), (el) => el.opacity(0.45))
    .when(Boolean(opts.tooltip), (el) => el.tooltip(opts.tooltip))
    .on_mouse_down("left", (_e, cx) => cx.stop_propagation())
    .when(!disabled, (el) => el.cursor_pointer().hover((s) => s.opacity(0.85))
      .on_click((e, cx) => { cx.stop_propagation(); action(e, cx); }))
    .child(text);
}

export const tiny = (id, text, action, opts = {}) => button(id, text, action, { h: 22, px: 7, size: 11, ...opts });

export const dot = (color, size = 8) => div().flex_none().w(size).h(size).rounded_full().bg(color);

// A run state as a glyph: a spinner while running, a pulsing dot when it needs you.
export function stateGlyph(state, size = 11) {
  const t = theme();
  if (state === "running") return svg(SPINNER).size(size).flex_none().text_color(t.working).spin(900);
  if (state === "blocked" || state === "failed") return div().flex_none().w(size - 2).h(size - 2).rounded_full().bg(t.blocked).loop("opacity", { from: 0.45, to: 1, period: 1200 });
  if (state === "ready") return div().flex_none().w(size - 2).h(size - 2).rounded_full().bg(t.done);
  if (state === "queued") return div().flex_none().w(size - 2).h(size - 2).rounded_full().border_1().border_color(t.dim);
  if (state === "stopped") return div().flex_none().w(size - 2).h(size - 2).rounded(2).bg(t.muted);
  return null;
}

export function stateChip(card) {
  const t = theme();
  const look = stateLook(card);
  if (!look) return null;
  return h_flex().flex_none().items_center().gap(5).text_size(10.5).text_color(tone(t, look.tone))
    .child(look.word).child(stateGlyph(card.state));
}

export function approvalChip(approval, opts = {}) {
  const t = theme();
  const b = approvalBadge(approval);
  const color = tone(t, b.tone);
  return h_flex().flex_none().items_center().gap(4).px(6).h(18).rounded(5).text_size(10.5)
    .border_1().border_color(approval?.state === "none" ? t.border : color).text_color(color)
    .child(b.mark).child(opts.short && approval?.state === "approved" ? "approved" : b.word);
}

// Task progress: "3/5" and a thin bar.
export function progressBar(tasks, w = 60) {
  const t = theme();
  const p = progress(tasks);
  return h_flex().flex_none().items_center().gap(6).text_size(10.5).text_color(t.dim)
    .child(div().w(w).h(4).rounded_full().bg(t.input)
      .child(div().h(4).rounded_full().bg(p.total && p.done === p.total ? t.done : t.accent).w(Math.round(w * p.ratio))))
    .child(`${p.done}/${p.total}`);
}

export function section(title, right) {
  const t = theme();
  return h_flex().items_center().pt(14).pb(6)
    .child(div().flex_1().text_size(11).font_semibold().text_color(t.dim).child(title.toUpperCase()))
    .when(Boolean(right), (el) => el.child(right));
}

export function closeButton(id, action) {
  const t = theme();
  return div().id(id).flex_none().w(26).h(26).rounded(6).flex().items_center().justify_center()
    .text_size(14).text_color(t.dim).cursor_pointer().hover((s) => s.bg(t.hover).text_color(t.foreground))
    .on_mouse_down("left", (_e, cx) => cx.stop_propagation())
    .on_click((e, cx) => { cx.stop_propagation(); action(e, cx); })
    .child("✕");
}

// A note box: an error (blocked color) or a plain notice.
export function note(id, text, kind = "info") {
  const t = theme();
  return div().id(id).p(8).rounded(6).bg(t.surface).border_1().border_color(kind === "error" ? t.blocked : t.border)
    .text_size(11.5).text_color(kind === "error" ? t.blocked : t.muted).child(text);
}
