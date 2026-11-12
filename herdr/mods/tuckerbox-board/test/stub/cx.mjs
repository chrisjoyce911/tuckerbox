// A view context for tests: timers are kept for the test to fire, spawned
// tasks are kept so the test can wait for them.
export function fakeCx() {
  const cx = {
    afters: [], everies: [], tasks: [], notified: 0,
    timer: { after: (_ms, fn) => cx.afters.push(fn), every: (_ms, fn) => cx.everies.push(fn) },
    spawn: (fn) => { const p = Promise.resolve().then(() => fn(cx)); cx.tasks.push(p); return p; },
    notify: () => { cx.notified++; },
    focus_handle: () => ({ is_focused: () => false }),
    stop_propagation: () => {},
  };
  cx.tick = () => cx.everies.forEach((fn) => fn(cx));
  cx.settle = async () => { for (let i = 0; i < 20; i++) await new Promise((r) => setImmediate(r)); };
  return cx;
}

// A promise the test resolves when it chooses.
export function gate() {
  let open;
  const p = new Promise((r) => { open = r; });
  return { p, open };
}
