import { element } from "./element.mjs";
export const v_flex = () => element("v_flex");
export const h_flex = () => element("h_flex");
export const TextView = { markdown: (id, source) => element("text_view", id, source) };
export const Scrollbar = { horizontal: (id) => element("scrollbar", id), vertical: (id) => element("scrollbar", id), new: (id) => element("scrollbar", id) };
export const TextareaState = { new: (options = {}) => {
  let value = options.value ?? "";
  return { options, value: () => value, set_value: (v) => { value = String(v); }, on: () => true, focus() {} };
} };
export const Textarea = { new: (state) => element("textarea", state) };
