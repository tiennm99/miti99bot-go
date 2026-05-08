# Project Cleanup Audit — 260424-1821

Scope: full repo focused on twentyq (reworked today) + surrounding docs/scripts. Tests green, biome clean.

## Findings

### 1. Stale module doc — claims function calling that was removed
- **Severity:** high
- **File:** `src/modules/twentyq/README.md:5` and `:67`
- **Problem:** README says the module uses "function calling" and references `prompts.js` as exporting `ANSWER_FUNCTION_SCHEMA` that declares a `submit_answer` tool. Neither exists — `ai-client.js` and `prompts.js` now explicitly document the opposite approach (JSON-in-content, no tools array). Readers will hunt for missing symbols.
- **Fix:** Replace line 5 sentence with e.g. "judges every user input with a Workers AI LLM (`@cf/google/gemma-4-26b-a4b-it`) that emits one-line JSON parsed from the response body." Rewrite line 67 to: "`prompts.js` — `buildSystemPrompt(state)` injects secret + history; `buildStartRoundPrompt(target)` produces the round-opening prompt." Delete the tool-call parse-shape claim on line 71 (no tool-call shape exists anymore; `ai-client` only extracts plain text + JSON-in-content).

### 2. Stale top-of-file comment — same function-calling claim
- **Severity:** high
- **File:** `src/modules/twentyq/index.js:4-7`
- **Problem:** Header still advertises "function calling — the model returns { is_guess, answer, hint }". Contradicts `ai-client.js:4-8` and `prompts.js:2-9` in the same directory.
- **Fix:** Replace lines 5-7 with "judges each user input via Workers AI (`@cf/google/gemma-4-26b-a4b-it`) — the model emits one-line JSON `{ is_guess, answer, hint }` parsed from the response body."

### 3. Stale codebase-summary — twentyq row + test counts + dep versions wrong
- **Severity:** high
- **File:** `docs/codebase-summary.md:26, 65, 66, 74, 77-84`
- **Problem:**
  - Line 26 twentyq row says "via function calling" — same stale claim as #1/#2.
  - Line 65: vitest listed as ^2.1.0, package.json has ^4.1.4.
  - Line 66: wrangler listed as ^3.90.0, package.json has ^4.84.0.
  - Line 74: "200 tests across 21 test files" — actual is 449 (user stated) across many more files.
  - Lines 77-84: missing rows for `semantle`, `doantu`, `twentyq`, `lolschedule`. Misleading — suggests only 4 modules are tested.
- **Fix:** Delete "via function calling" on line 26 (say "judges each yes/no question and generates fresh hints"). Bump versions to match `package.json`. Regenerate test-count line from actual test run output. Add rows for semantle/doantu/twentyq/lolschedule with real counts from `npx vitest list`.

### 4. Stale architecture file tree (omits 4 modules + 2 files)
- **Severity:** medium
- **File:** `docs/architecture.md:19-42` and `:105-113`
- **Problem:** The ASCII tree shows only `util, trading, wordle, loldle, misc` and omits the snippet of `moduleRegistry` at line 105-113 which predates doantu/semantle/twentyq/lolschedule. It also omits `cron-dispatcher.js` and `validate-cron.js`. Readers trusting this doc will think those modules don't exist.
- **Fix:** Extend the tree to include `lolschedule/`, `semantle/`, `doantu/`, `twentyq/`, `cron-dispatcher.js`, `validate-cron.js`. Update the inline `moduleRegistry` snippet at 105-113 to match the 9 entries currently in `src/modules/index.js`.

### 5. Stale wrangler.toml AI-binding comment
- **Severity:** low
- **File:** `wrangler.toml:29-34`
- **Problem:** Comment claims `env.AI` is "used by semantle + doantu". `twentyq` also uses it. The Neuron/pricing numbers quoted are bge-m3 embedding numbers — twentyq uses Gemma which has different pricing.
- **Fix:** Change "semantle + doantu" → "semantle, doantu, and twentyq". Add a second line noting twentyq uses `@cf/google/gemma-4-26b-a4b-it` (separate pricing) or just drop the specific bge-m3 math and keep the pricing link.

### 6. Obsolete docs/todo.md — D1 already deployed
- **Severity:** medium
- **File:** `docs/todo.md` (entire file)
- **Problem:** File is the TODO for the D1+Cron infra rollout. `wrangler.toml:26` already has a real D1 UUID (`261b54e7-...`), so the "Pre-deploy" checklist is satisfied. The trading cron is live. The "first deploy verification" items are historical. The file survives as a reader-confusing artefact.
- **Fix:** Delete the three satisfied sections (Pre-deploy, First deploy verification, Post-deploy smoke tests), leaving only the "Nice-to-have" section. Or delete the whole file and fold the unclaimed items into `docs/development-roadmap.md`.

### 7. Stale stub-kv.js comment references nonexistent flag
- **Severity:** low
- **File:** `scripts/stub-kv.js:10`
- **Problem:** Doc-comment says future modules should "gate the write on a `process.env.REGISTER_DRYRUN` flag" — that flag is never read anywhere and has no consumer.
- **Fix:** Either plumb the flag through `register.js` (overkill — YAGNI) or replace the sentence with "If a future module writes inside init(), restructure that init to defer writes until the first handler call." Keep the `stubKv` / `stubAi` simple.

### 8. Confusing handleStats test — saves game, asserts stats
- **Severity:** low
- **File:** `tests/modules/twentyq/handlers.test.js:192-200`
- **Problem:** Test saves a game via `saveGame` then calls `handleStats` and asserts "no games" message. It passes (saving a game doesn't write stats), but the `saveGame` call is pure noise and misleads readers into thinking the render is expected even when a game is active.
- **Fix:** Delete the `await saveGame(db, 1, sampleGame())` line. Either keep the test as "renders empty summary when no stats" or add a second assertion that exercises the `played > 0` branch (the stats row is also currently not tested end-to-end — only `formatStats` is covered in render.test.js).

### 9. Inaccurate assertion in "fresh round + text" test
- **Severity:** low
- **File:** `tests/modules/twentyq/handlers.test.js:138-143`
- **Problem:** Minor — the test mocks `mockRoundStart(ai)` + `mockJudgement(ai, ...)` then asserts `ctx.reply` is called twice but never verifies `ai.run` was called twice. If the handler ever regressed to silently skipping one AI call, this would pass on reply count alone.
- **Fix:** Add `expect(ai.run).toHaveBeenCalledTimes(2);` after line 140. Same nit applies to the group-chat test at 161-170 — add `expect(ai.run).toHaveBeenCalledTimes(2);`.

### 10. Unused `recordResult` return value
- **Severity:** low
- **File:** `src/modules/twentyq/state.js:107`
- **Problem:** `recordResult` ends with `return s;` but no caller uses the returned stats. Mirrors doantu but unreferenced here.
- **Fix:** Either drop `return s;` and the implicit `Promise<TwentyqStats>` from the JSDoc (cleaner), or consume the return in handlers (e.g. post a one-line stat update after `giveup`/`solve`). Dropping is the YAGNI move.

### 11. Over-broad redact regex on one-letter targets
- **Severity:** low (defence-in-depth, not broken)
- **File:** `src/modules/twentyq/ai-client.js:125-131`
- **Problem:** `redactSecret` uses `\b<word>\b`. For single-letter or digit-heavy targets the regex still works, but the "entire-hint-became-redacted" fallback string at line 130 (`out.length > 0 ? ... : "the hint was redacted..."`) can never actually trigger because `hint.replace` with any input always yields a length > 0 (it replaces, not deletes). The safety branch is dead code.
- **Fix:** Simplify to `return out;` and drop the fallback branch + message. If you want to guard against a hint that IS the secret, compare `out === "(redacted)"` instead (that's the real "hint was just the secret" case).

### 12. .env.deploy.example default MODULES — requires manual sync with wrangler.toml
- **Severity:** low
- **File:** `.env.deploy.example:15` vs `wrangler.toml:8`
- **Problem:** Two places define the same comma-separated list. They happen to match today but drift is easy.
- **Fix:** Either (a) have `register.js` parse `wrangler.toml` directly, or (b) leave the duplication but add a one-line comment in both places: "KEEP IN SYNC WITH wrangler.toml [vars] MODULES" — currently the comment only exists in `.env.deploy.example`. YAGNI: just add the reciprocal comment to `wrangler.toml:5-6`.

### 13. Empty `D1 layer` test-coverage row
- **Severity:** low
- **File:** `docs/codebase-summary.md:79`
- **Problem:** Row says "DB layer (D1) | — | Fake D1 in-memory implementation...". The em-dash "tests" count is confusing; `tests/fakes/fake-d1.js` is exercised by trading tests. Either real count or drop.
- **Fix:** Delete the row or merge into the trading row.

### 14. validate-input.js open-ended regex is incomplete
- **Severity:** low (observation)
- **File:** `src/modules/twentyq/validate-input.js:13`
- **Problem:** Bars "what how why which who where when tell me describe explain". Misses some natural open-enders like "name", "list", "give me". Not worth flagging as a bug but note for when a user complains.
- **Fix:** No action unless users report — KISS. Document the short allow-list philosophy in the comment.

## Summary

- **Total findings:** 14 (3 high, 3 medium, 8 low)
- **Recommended apply order (easy wins → larger):**
  1. #1, #2 — stale twentyq function-calling claims in `README.md` + `index.js` header (5-min delete/rewrite each)
  2. #5, #7 — one-line comment fixes in `wrangler.toml` + `stub-kv.js`
  3. #11, #10 — tiny code deletes in `ai-client.js` + `state.js`
  4. #8, #9 — test cleanups
  5. #3, #4 — regenerate doc tables/trees in `docs/codebase-summary.md` + `docs/architecture.md`
  6. #6 — decide whether to delete `docs/todo.md` or trim it
  7. #12, #13, #14 — optional polish

## Unresolved Questions

- Should `docs/todo.md` be deleted outright or trimmed to the remaining "nice-to-have" items? (Style choice — leaning delete per YAGNI.)
- Is the `recordResult` return value kept on purpose for parity with doantu/semantle (which may use it)? Worth a 30-sec check in those modules before removing.
