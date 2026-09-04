#!/usr/bin/env node
/**
 * WASM smoke test — proves the browser bundle can actually call into the Go core.
 *
 * It reproduces, outside a browser, exactly what `web/src/core-bridge/worker.ts`
 * does at runtime: load Go's `wasm_exec.js` runtime shim, instantiate
 * `web/public/core.wasm`, let `go.run` register the `gintrackCore` namespace on
 * `globalThis`, then call across the boundary and check the JSON envelope.
 *
 * Both inputs are build artifacts produced by `make wasm` and are git-ignored,
 * so this script is run as `make wasm-smoke` (which builds them first) or as
 * `npm run wasm:smoke` from `web/`.
 *
 * Usage: node scripts/wasm-smoke.mjs
 * Exit code: 0 when every assertion passes, 1 otherwise.
 */
import { createRequire } from "node:module";
import { readdir, readFile } from "node:fs/promises";
import { dirname, join, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";
import vm from "node:vm";

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), "..");
const WASM_EXEC = join(repoRoot, "web", "public", "wasm_exec.js");
const CORE_WASM = join(repoRoot, "web", "public", "core.wasm");

/** The fixture vault the bridge is exercised against, as the Go tests use it. */
const FIXTURE = join(repoRoot, "testdata", "fixtures", "project-basic");

/** Path and body of the item the core is asked to parse. */
const SAMPLE_PATH = "docs/.pmngr/stories/SMOKE-US-0001-wasm-smoke-test.md";
const SAMPLE_TEXT = `---
id: SMOKE-US-0001
type: story
title: Call the Go core from JavaScript
status: in_progress
priority: high
parent: SMOKE-EP-0001
milestone: SMOKE-M-0001
author: ci
assignees: [ci]
labels: [wasm, ci]
estimate: 3
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T00:00:00Z
links:
  - { kind: blocked_by, target: SMOKE-T-0001 }
---

## Description

As a maintainer, I want a smoke test that instantiates core.wasm outside a
browser, so that a broken WASM build fails CI instead of the first user.

## Acceptance Criteria

- [x] The module instantiates and registers globalThis.gintrackCore.
- [ ] parseItem returns a well-formed envelope.
`;

const checks = [];

/** Records one assertion and keeps going, so a run reports every failure at once. */
function check(name, ok, detail = "") {
  checks.push({ name, ok, detail });
  const mark = ok ? "ok  " : "FAIL";
  console.log(`  ${mark} ${name}${detail ? ` — ${detail}` : ""}`);
  return ok;
}

/**
 * Installs the globals Go's `wasm_exec.js` expects. Node 20+ already provides
 * every one of them, so this is a no-op there; it keeps the script working on
 * hosts where `crypto` or the text codecs are not global yet.
 */
function installNodeShims() {
  const require = createRequire(import.meta.url);
  globalThis.require ??= require;
  globalThis.fs ??= require("node:fs");
  globalThis.process ??= process;
  const util = require("node:util");
  globalThis.TextEncoder ??= util.TextEncoder;
  globalThis.TextDecoder ??= util.TextDecoder;
  globalThis.performance ??= require("node:perf_hooks").performance;
  globalThis.crypto ??= require("node:crypto").webcrypto;
}

/**
 * Reads the fixture vault into the `VaultFile[]` shape of `vault.load`: paths
 * are vault-relative and always use forward slashes, whatever the host is.
 */
async function readVault(root) {
  const files = [];
  const walk = async (dir) => {
    for (const entry of await readdir(dir, { withFileTypes: true })) {
      const full = join(dir, entry.name);
      if (entry.isDirectory()) {
        await walk(full);
        continue;
      }
      files.push({
        path: relative(root, full).split(sep).join("/"),
        text: await readFile(full, "utf8"),
      });
    }
  };
  await walk(root);
  files.sort((a, b) => (a.path < b.path ? -1 : 1));
  return files;
}

/** Evaluates `wasm_exec.js` — a classic script whose only effect is `globalThis.Go`. */
async function loadGoRuntime() {
  let source;
  try {
    source = await readFile(WASM_EXEC, "utf8");
  } catch {
    throw new Error(`${WASM_EXEC} is missing; run "make wasm" first`);
  }
  vm.runInThisContext(source, { filename: "wasm_exec.js" });
  if (typeof globalThis.Go !== "function") {
    throw new Error("wasm_exec.js did not define globalThis.Go");
  }
}

/** Instantiates core.wasm and starts the Go program that registers the namespace. */
async function startCore() {
  let bytes;
  try {
    bytes = await readFile(CORE_WASM);
  } catch {
    throw new Error(`${CORE_WASM} is missing; run "make wasm" first`);
  }
  const go = new globalThis.Go();
  const { instance } = await WebAssembly.instantiate(bytes, go.importObject);

  // `wasm/main_js.go` ends in `select {}`, so this promise never resolves: the
  // core stays alive for the lifetime of the host, exactly as in the worker.
  // Only an early exit or a trap settles it, and that is a failure.
  go.run(instance).then(
    () => {
      console.error("FAIL the Go core exited instead of staying resident");
      process.exit(1);
    },
    (error) => {
      console.error("FAIL the Go core trapped:", error);
      process.exit(1);
    },
  );

  // `go.run` calls the module's `run` export synchronously before its first
  // await, so the namespace is already there; poll anyway rather than assume it.
  for (
    let attempt = 0;
    attempt < 100 && !globalThis.gintrackCore;
    attempt += 1
  ) {
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  if (!globalThis.gintrackCore) {
    throw new Error(
      "core.wasm ran but never registered globalThis.gintrackCore",
    );
  }
  return globalThis.gintrackCore;
}

/** Parses an envelope returned by the core, failing loudly on anything else. */
function envelope(label, raw) {
  if (typeof raw !== "string") {
    throw new Error(`${label} returned ${typeof raw}, expected a JSON string`);
  }
  try {
    return JSON.parse(raw);
  } catch (error) {
    throw new Error(`${label} returned invalid JSON: ${String(error)}`);
  }
}

function isNonEmptyString(value) {
  return typeof value === "string" && value.length > 0;
}

async function main() {
  installNodeShims();
  await loadGoRuntime();
  const core = await startCore();

  console.log("gintrackCore.version()");
  check("version() is exported", typeof core.version === "function");
  const versionEnvelope = envelope("version()", core.version());
  check("version() envelope has ok: true", versionEnvelope.ok === true);
  const build = versionEnvelope.data ?? {};
  check(
    "version is a string",
    isNonEmptyString(build.version),
    String(build.version),
  );
  check(
    "commit is a string",
    isNonEmptyString(build.commit),
    String(build.commit),
  );
  check("date is a string", isNonEmptyString(build.date), String(build.date));
  check(
    "schema is a number",
    typeof build.schema === "number",
    String(build.schema),
  );

  console.log("gintrackCore.parseItem(path, text)");
  check("parseItem() is exported", typeof core.parseItem === "function");
  const parsed = envelope(
    "parseItem()",
    core.parseItem(SAMPLE_PATH, SAMPLE_TEXT),
  );
  if (parsed.ok !== true) {
    check(
      "parseItem() envelope has ok: true",
      false,
      JSON.stringify(parsed.error),
    );
  } else {
    check("parseItem() envelope has ok: true", true);
    const item = parsed.data ?? {};
    check("id is SMOKE-US-0001", item.id === "SMOKE-US-0001", String(item.id));
    check("type is story", item.type === "story", String(item.type));
    check(
      "title is the front-matter title",
      item.title === "Call the Go core from JavaScript",
      String(item.title),
    );
    // `rev` is never stored in the file: the core computes it while parsing,
    // which is what makes it the proof that real core code ran.
    check(
      "rev is a non-empty content hash",
      isNonEmptyString(item.rev),
      String(item.rev),
    );
    check("path round-trips", item.path === SAMPLE_PATH, String(item.path));
    check(
      "body carries the Markdown",
      String(item.body ?? "").includes("## Description"),
    );
  }

  console.log("gintrackCore.parseItem() error path");
  const broken = envelope(
    "parseItem()",
    core.parseItem(SAMPLE_PATH, "no front matter here\n"),
  );
  check("a malformed item yields ok: false", broken.ok === false);
  check(
    "the failure carries a stable error code",
    isNonEmptyString(broken.error?.code),
    String(broken.error?.code),
  );

  console.log('gintrackCore.call("vault.load", …)');
  check("call() is exported", typeof core.call === "function");
  const vault = await readVault(FIXTURE);
  check(
    "the fixture vault was read",
    vault.length === 9,
    `${vault.length} files`,
  );

  const loaded = envelope(
    "call(vault.load)",
    core.call("vault.load", JSON.stringify({ files: vault })),
  );
  if (loaded.ok !== true) {
    check(
      "vault.load envelope has ok: true",
      false,
      JSON.stringify(loaded.error),
    );
  } else {
    check("vault.load envelope has ok: true", true);
    const stats = loaded.result ?? {};
    check(
      "one project was discovered",
      stats.projects === 1,
      String(stats.projects),
    );
    check("five items were indexed", stats.items === 5, String(stats.items));
    check(
      "one comment was indexed",
      stats.comments === 1,
      String(stats.comments),
    );
    check(
      "two knowledge-base pages were indexed",
      stats.pages === 2,
      String(stats.pages),
    );
    check(
      "the index has no errors",
      stats.errors === 0,
      JSON.stringify(stats.diagnostics),
    );
    check(
      "a fingerprint was computed",
      isNonEmptyString(stats.fingerprint),
      String(stats.fingerprint),
    );
  }

  console.log('gintrackCore.call("item.list", …)');
  const listed = envelope(
    "call(item.list)",
    core.call("item.list", JSON.stringify({ type: "story" })),
  );
  if (listed.ok !== true) {
    check(
      "item.list envelope has ok: true",
      false,
      JSON.stringify(listed.error),
    );
  } else {
    check("item.list envelope has ok: true", true);
    const page = listed.result ?? {};
    check("two stories are listed", page.total === 2, String(page.total));
    check(
      "the page carries the items",
      Array.isArray(page.items) && page.items.length === 2,
    );
    const ids = (page.items ?? []).map((item) => item.id).sort();
    check(
      "the ids are the fixture stories",
      ids.join(",") === "DEMO-US-0001,DEMO-US-0002",
      ids.join(","),
    );
    // A list call drops bodies: they are the bulk of a real vault.
    check(
      "list results carry no body",
      (page.items ?? []).every((item) => item.body === ""),
    );
  }

  console.log("gintrackCore.call() error path");
  const unknown = envelope("call(nope)", core.call("nope", "null"));
  check("an unknown method yields ok: false", unknown.ok === false);
  check(
    "the failure carries a stable error code",
    unknown.error?.code === "unknown_method",
    String(unknown.error?.code),
  );

  const failed = checks.filter((c) => !c.ok);
  console.log("");
  if (failed.length > 0) {
    console.error(
      `wasm-smoke: ${failed.length}/${checks.length} checks failed`,
    );
    process.exit(1);
  }
  console.log(`wasm-smoke: ${checks.length}/${checks.length} checks passed`);
  process.exit(0);
}

main().catch((error) => {
  console.error("wasm-smoke:", error instanceof Error ? error.message : error);
  process.exit(1);
});
