#!/usr/bin/env node
/**
 * Cursor Composer 2.5 routing fix for AI triage jobs.
 * Reads /tmp/triage_job.json (Encore internal API) and applies minimal patches.
 */
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";

// SDK reads CURSOR_RIPGREP_PATH during module init — must be set before import.
const rgPath = resolveRipgrepPath();
if (rgPath) {
  process.env.CURSOR_RIPGREP_PATH = rgPath;
  console.error("triage-cursor-fix: ripgrep=", rgPath);
} else {
  console.error("triage-cursor-fix: warning — ripgrep tidak ditemukan");
}

const { Agent, CursorAgentError, JsonlLocalAgentStore } = await import("@cursor/sdk");

const jobPath = process.env.TRIAGE_JOB_JSON || "/tmp/triage_job.json";
const apiKey = process.env.CURSOR_API_KEY?.trim();
if (!apiKey) {
  console.error("CURSOR_API_KEY required");
  process.exit(1);
}

const raw = readFileSync(jobPath, "utf8");
const job = JSON.parse(raw).job || {};
const analysis = job.analysis || {};

const prompt = buildPrompt(job, analysis);
console.error("triage-cursor-fix: prompting Composer 2.5…");

const storeDir = join(tmpdir(), "wabantu-cursor-triage", job.id || process.env.JOB_ID || "default");
mkdirSync(storeDir, { recursive: true });
const store = new JsonlLocalAgentStore(storeDir);

// Tanpa `await using` (butuh Node 24+) — runner CI masih Node 22.
// Dispose eksplisit di finally + process.exitCode agar cleanup tetap jalan.
async function main() {
  let agent;
  try {
    agent = await Agent.create({
      apiKey,
      model: { id: "composer-2.5" },
      local: { cwd: process.cwd(), settingSources: [], store },
    });

    if (agent.agentId) {
      writeFileSync("/tmp/cursor_agent_id.txt", agent.agentId);
      console.error("cursor agent id:", agent.agentId);
    }

    const run = await agent.send(prompt);
    for await (const event of run.stream()) {
      if (event.type === "tool_call" && event.status === "running") {
        console.error("triage-cursor-fix: tool →", event.name);
      } else if (event.type === "assistant") {
        for (const block of event.message?.content || []) {
          if (block.type === "text" && block.text?.trim()) {
            console.error("triage-cursor-fix:", block.text.trim().slice(0, 200));
          }
        }
      } else if (event.type === "status") {
        console.error("triage-cursor-fix: status", event.status);
      }
    }

    const result = await run.wait();
    if (result.status === "error") {
      console.error("Composer run failed:", result.id);
      return 2;
    }
    console.error("triage-cursor-fix: done", result.status);
    return 0;
  } catch (err) {
    if (err instanceof CursorAgentError) {
      console.error("Cursor startup failed:", err.message, "retryable=", err.isRetryable);
      return 1;
    }
    throw err;
  } finally {
    await agent?.[Symbol.asyncDispose]?.();
  }
}

process.exitCode = await main();

function resolveRipgrepPath() {
  const fromEnv = process.env.CURSOR_RIPGREP_PATH?.trim();
  if (fromEnv && existsSync(fromEnv)) return fromEnv;

  const require = createRequire(import.meta.url);
  const plat = `${process.platform}-${process.arch}`;
  try {
    const pkgDir = dirname(require.resolve(`@cursor/sdk-${plat}/package.json`));
    const bin = process.platform === "win32" ? "rg.exe" : "rg";
    const bundled = join(pkgDir, "bin", bin);
    if (existsSync(bundled)) return bundled;
  } catch {
    // optional platform package missing locally
  }
  return null;
}

function buildPrompt(job, analysis) {
  const mismatches = (analysis.mismatches || [])
    .filter((m) => !m.skipped && m.expectedPath && m.userText)
    .slice(0, 8);
  const regressionFailures = (analysis.regressionFailures || []).slice(0, 12);
  const fixHints = analysis.fixHints || {};

  const failingCases = regressionFailures.map((f) => ({
    caseName: f.caseName,
    gotPath: f.gotPath,
    wantPath: f.wantPath,
    replyPreview: f.replyPreview?.slice(0, 80),
  }));

  const mismatchSummary = mismatches.map((m) => ({
    userText: m.userText?.slice(0, 120),
    expectedPath: m.expectedPath,
    actualPath: m.actualPath,
    priorTurns: m.priorTurns?.slice(-3),
  }));

  return `You are fixing deterministic WhatsApp AI routing in WABantu api-go.

## Scope (strict)
- ONLY edit: ai/autoreply.go and/or ai/conversation_sim.go
- Goal: simulator routing (ConversationSimulator.Turn) should match production paths for the cases below
- Do NOT change LLM reply text generation, webhook hot path, or unrelated files
- Minimal diff; match existing Go patterns; standard testing package only (no testify)
- Do NOT run shell commands (no encore test) — CI runs regression after this step
- Do NOT modify test files or triageAutoGenSnapshotJSON

## Job
- id: ${job.id || ""}
- tenant: ${job.tenantSchema || ""}
- conversation: ${job.conversationId || ""}
- draft PR: ${job.prUrl || "none"}
- prior Composer fix attempts: ${analysis.cursorFixAttempts ?? 0}

## Failing regression cases (fix these first)
${JSON.stringify(failingCases, null, 2)}

## Routing mismatches at analyze time
${JSON.stringify(mismatchSummary, null, 2)}

## Hints
${JSON.stringify(fixHints, null, 2)}

## Tasks
1. Open ai/conversation_regression_auto_gen_test.go — read failing case names above; replay priorInputs before input.
2. Patch routing in autoreply.go / conversation_sim.go only.
3. Summarize which paths were fixed and why.`;
}
