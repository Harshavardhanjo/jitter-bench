// Compiles the Go simulator to WebAssembly and copies Go's loader shim next to
// it. Run from the ui/ directory by npm run wasm, and wired into dev and build so
// the binary can never be stale relative to the Go source it came from.
import { execFileSync } from "node:child_process";
import { copyFileSync, mkdirSync, statSync } from "node:fs";
import { join, resolve } from "node:path";

const repoRoot = resolve(import.meta.dirname, "..", "..");
const publicDir = resolve(import.meta.dirname, "..", "public");
mkdirSync(publicDir, { recursive: true });

const goroot = execFileSync("go", ["env", "GOROOT"], { encoding: "utf8" }).trim();

// wasm_exec.js moved from misc/wasm to lib/wasm in Go 1.24. Try both so the
// build works on either.
const candidates = [join(goroot, "lib", "wasm", "wasm_exec.js"), join(goroot, "misc", "wasm", "wasm_exec.js")];
const shim = candidates.find((p) => {
  try {
    return statSync(p).isFile();
  } catch {
    return false;
  }
});
if (!shim) {
  throw new Error(`could not find wasm_exec.js under ${goroot}; looked in lib/wasm and misc/wasm`);
}

const out = join(publicDir, "jitterbench.wasm");
execFileSync("go", ["build", "-o", out, "./wasm"], {
  cwd: repoRoot,
  env: { ...process.env, GOOS: "js", GOARCH: "wasm" },
  stdio: "inherit",
});
copyFileSync(shim, join(publicDir, "wasm_exec.js"));

const { size } = statSync(out);
console.log(`wasm ${(size / 1e6).toFixed(2)} MB -> public/jitterbench.wasm`);
console.log(`shim ${shim} -> public/wasm_exec.js`);
