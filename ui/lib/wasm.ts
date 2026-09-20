// Loads the Go simulator compiled to WebAssembly and wraps its two entry points.
//
// The browser runs the same code as the command line tool. Nothing here
// reimplements the simulation, so the chart cannot disagree with the benchmark.

import type { PresetInfo, SweepRequest, SweepResult } from "./types";

type Bridge = {
  presets: () => string;
  sweep: (requestJson: string) => string;
};

declare global {
  interface Window {
    // Go's loader shim defines this constructor.
    Go?: new () => {
      importObject: WebAssembly.Imports;
      run: (instance: WebAssembly.Instance) => Promise<void>;
    };
    jitterbench?: Bridge;
  }
}

let loading: Promise<Bridge> | null = null;

function loadScript(src: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const existing = document.querySelector<HTMLScriptElement>(`script[src="${src}"]`);
    if (existing) {
      resolve();
      return;
    }
    const el = document.createElement("script");
    el.src = src;
    el.onload = () => resolve();
    el.onerror = () => reject(new Error(`failed to load ${src}`));
    document.head.appendChild(el);
  });
}

/** Resolves once the wasm module has published its exports. */
export function loadBridge(): Promise<Bridge> {
  if (loading) return loading;

  loading = (async () => {
    await loadScript("./wasm_exec.js");
    if (!window.Go) {
      throw new Error("wasm_exec.js loaded but did not define Go");
    }

    const go = new window.Go();

    // instantiateStreaming avoids buffering the whole 4.7MB before compiling, but
    // it needs the server to send application/wasm. Fall back when it does not.
    let instance: WebAssembly.Instance;
    try {
      const streamed = await WebAssembly.instantiateStreaming(fetch("./jitterbench.wasm"), go.importObject);
      instance = streamed.instance;
    } catch {
      const bytes = await (await fetch("./jitterbench.wasm")).arrayBuffer();
      const compiled = await WebAssembly.instantiate(bytes, go.importObject);
      instance = compiled.instance;
    }

    // Deliberately not awaited. The Go main blocks forever so its exports stay
    // callable, so this promise never resolves; awaiting it would hang here.
    void go.run(instance);

    // go.run publishes the global synchronously in practice, but poll rather than
    // assume, so a change in Go's startup does not turn into a null dereference.
    const deadline = Date.now() + 10_000;
    while (!window.jitterbench) {
      if (Date.now() > deadline) {
        throw new Error("wasm started but never published the jitterbench global");
      }
      await new Promise((r) => setTimeout(r, 10));
    }
    return window.jitterbench;
  })();

  // A failed load must not be cached, or one flaky fetch breaks the page until
  // it is reloaded.
  loading.catch(() => {
    loading = null;
  });

  return loading;
}

function parse<T>(json: string): T {
  const value = JSON.parse(json) as T | { error: string };
  if (value && typeof value === "object" && "error" in value) {
    throw new Error((value as { error: string }).error);
  }
  return value as T;
}

export async function getPresets(): Promise<PresetInfo[]> {
  const bridge = await loadBridge();
  return parse<PresetInfo[]>(bridge.presets());
}

export async function runSweep(request: SweepRequest): Promise<SweepResult> {
  const bridge = await loadBridge();
  return parse<SweepResult>(bridge.sweep(JSON.stringify(request)));
}
