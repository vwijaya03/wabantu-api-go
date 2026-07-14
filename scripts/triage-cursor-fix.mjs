#!/usr/bin/env node
/**
 * Cursor Composer 2.5 routing fix for AI triage jobs.
 * Reads /tmp/triage_job.json (Encore internal API) and applies minimal patches.
 */
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Agent, CursorAgentError, JsonlLocalAgentStore } from "@cursor/sdk";

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

try {
  const result = await Agent.prompt(prompt, {
    apiKey,
    model: { id: "composer-2.5" },
    local: { cwd: process.cwd(), settingSources: [], store },
  });

  if (result.status === "error") {
    console.error("Composer run failed:", result.id);
    process.exit(2);
  }

  const agentId = result.agentId || result.agent_id || "";
  if (agentId) {
    writeFileSync("/tmp/cursor_agent_id.txt", agentId);
    console.error("cursor agent id:", agentId);
  }
  console.error("triage-cursor-fix: done", result.status);
} catch (err) {
  if (err instanceof CursorAgentError) {
    console.error("Cursor startup failed:", err.message, "retryable=", err.isRetryable);
    process.exit(1);
  }
  throw err;
}

function buildPrompt(job, analysis) {
  const mismatches = (analysis.mismatches || []).filter(
    (m) => !m.skipped && m.expectedPath && m.userText,
  );
  const regressionFailures = analysis.regressionFailures || [];
  const fixHints = analysis.fixHints || {};

  return `You are fixing deterministic WhatsApp AI routing in WABantu api-go.

## Scope (strict)
- ONLY edit: ai/autoreply.go and/or ai/conversation_sim.go
- Goal: simulator routing (ConversationSimulator.Turn) should match production paths for the cases below
- Do NOT change LLM reply text generation, webhook hot path, or unrelated files
- Minimal diff; match existing Go patterns; standard testing package only (no testify)

## Job
- id: ${job.id || ""}
- tenant: ${job.tenantSchema || ""}
- conversation: ${job.conversationId || ""}
- draft PR may exist: ${job.prUrl || "none"}

## Routing mismatches (production vs simulator at analyze time)
${JSON.stringify(mismatches, null, 2)}

## Regression test failures (if any)
${JSON.stringify(regressionFailures, null, 2)}

## Hints
${JSON.stringify(fixHints, null, 2)}

## Catalog snapshot
Auto-gen regression tests replay routing with the same tenant catalog/profile/KB frozen at analyze time
(embedded as triageAutoGenSnapshotJSON in conversation_regression_auto_gen_test.go).
Do NOT change the snapshot unless catalog data itself changed; fix routing in autoreply.go / conversation_sim.go.

## Tasks
1. Read ai/conversation_regression_auto_gen_test.go for failing cases (priorInputs must be replayed before input).
2. Fix routing so encore test passes:
   encore test ./ai/ -run TestConversationRegressionAutoGen -count=1
3. If tests still fail after reasonable routing fix, stop — do not hack tests.

When done, summarize which paths were fixed and why.`;
}
