// A strict stand-in for gpui-kit elements: every member must be one the
// committed list (herdr-api.json, taken from Herdr's gpui-kit.d.ts) names, or
// it throws, so a test can't pass on a call Herdr doesn't have. Each element
// records its calls and children for tests to inspect.
import { readFileSync } from "node:fs";

export const API = JSON.parse(readFileSync(new URL("./herdr-api.json", import.meta.url), "utf8"));
const MEMBERS = new Set(API.element);

export function element(kind, ...args) {
  const node = { kind, args, calls: [], kids: [] };
  const el = new Proxy(node, {
    get(t, k, self) {
      if (typeof k === "symbol" || Object.hasOwn(t, k)) return t[k];
      if (k === "then") return undefined; // not a promise
      if (!MEMBERS.has(k)) throw new TypeError(`gpui-kit.d.ts declares no element member ${k}() (test/stub/herdr-api.json)`);
      if (k === "when") return (yes, branch) => (yes ? branch(self) : self);
      if (k === "child") return (c) => { t.kids.push(c); return self; };
      if (k === "children") return (cs) => { for (const c of cs) t.kids.push(c); return self; };
      return (...a) => { t.calls.push([k, ...a]); return self; };
    },
  });
  return el;
}

const isEl = (x) => x && typeof x === "object" && Array.isArray(x.calls);

// The arguments of every call of member on el (itself only).
export const callsOf = (el, member) => el.calls.filter((c) => c[0] === member).map((c) => c.slice(1));
export const has = (el, member) => callsOf(el, member).length > 0;
export const idOf = (el) => callsOf(el, "id")[0]?.[0] ?? null;

// Every element under el, el first.
export function all(el) {
  const out = [];
  const walk = (x) => { if (!isEl(x)) return; out.push(x); x.kids.forEach(walk); };
  walk(el);
  return out;
}

export const find = (el, id) => all(el).find((x) => idOf(x) === id) ?? null;

// The text el draws, in order.
export function text(el) {
  const out = [];
  const walk = (x) => { if (typeof x === "string" || typeof x === "number") out.push(String(x)); else if (isEl(x)) x.kids.forEach(walk); };
  walk(el);
  return out;
}
