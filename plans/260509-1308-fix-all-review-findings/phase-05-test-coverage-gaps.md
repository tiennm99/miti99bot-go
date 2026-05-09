---
phase: 5
title: "Test coverage gaps"
status: pending
priority: P2
effort: "6-8h"
dependencies: [3]
---

# Phase 5: Test coverage gaps

## Overview
Coverage at 44.7% with handler-layer at 0% in 5 modules and Firestore ops skipped on CI. Implement handler integration tests + Firestore emulator in CI to reach ≥60% coverage and gain confidence in the dispatch path that currently has no test exercising it end-to-end.

## Requirements
- Functional: every handler reachable via `bot.ProcessUpdate` exercised in tests with realistic `*models.Update` fixtures.
- Non-functional: tests run in <30s on CI; emulator setup adds <60s startup; no flakes.

## Architecture

### Handler test pattern
- New `testutil/update.go` package: builders for `NewPrivateMessage(userID, text)`, `NewGroupMessage(chatID, userID, text)`, `NewChannelMessage(chatID, text)`.
- Bot mock: capture sent messages via a `recordingBot` that stores `SendMessageParams` instead of calling Telegram. (`*bot.Bot` has unexported fields — alternative: spin httptest server that replies to `sendMessage` API and use `bot.WithServerURL`.)
- Per-module `handlers_test.go` exercises each handler with a real in-memory KV provider + recording bot.

### Firestore emulator on CI
Add GitHub Actions service or Docker step:
```yaml
- name: Start Firestore emulator
  run: |
    gcloud --quiet components install beta cloud-firestore-emulator
    gcloud beta emulators firestore start --host-port=localhost:8080 &
    until nc -z localhost 8080; do sleep 1; done
- name: Run tests
  env:
    FIRESTORE_EMULATOR_HOST: localhost:8080
    GOOGLE_CLOUD_PROJECT: test-project
  run: go test -race ./internal/storage/...
```

Or use `firestore-emulator` Docker image with service container.

## Related Code Files
- Create: `internal/testutil/update.go` — Update fixture builders
- Create: `internal/testutil/recordbot.go` — recording bot helper (httptest-based)
- Create: `internal/modules/wordle/handlers_test.go`
- Create: `internal/modules/loldle/handlers_test.go`
- Create: `internal/modules/loldleemoji/handlers_test.go`
- Create: `internal/modules/util/handlers_test.go` (info/help/stickerid)
- Create: `internal/modules/misc/handlers_test.go`
- Modify: `.github/workflows/ci.yml` — emulator setup + env

## Implementation Steps

1. **Build `internal/testutil`**
   - `NewPrivateMessage`, `NewGroupMessage`, `NewChannelMessage` builders.
   - `NewRecordingBot()` returns `*bot.Bot` wired to httptest server that records `SendMessage`/`SendSticker` requests; expose `Sent() []SendMessageParams`.
   - Tests for the test util itself.

2. **Wordle handler tests** (~25% coverage gain)
   - `TestHandleWordle_Win` — guess equals target → win path, sticker, stats.
   - `TestHandleWordle_Loss` — exhaust max guesses → loss path.
   - `TestHandleWordle_InvalidWord` — non-dictionary word → reject.
   - `TestHandleNew` — abandon active round, autoGiveup recorded.
   - `TestHandleGiveup` — reveal, idempotency on finished.
   - `TestHandleStats` — win rate calc with wins/losses.
   - `TestHandleWordle_NilMessage` — nil-guard path.

3. **Loldle handler tests** (~10% gain) — same pattern.

4. **Loldleemoji handler tests** (~20% gain) — same pattern.

5. **Util handler tests** (~15% gain)
   - `TestInfoCommand_*` — chat-id/sender-id echo.
   - `TestHelpCommand_*` — registry render.
   - `TestStickerIDCommand_*` — sticker echo, no-sticker case.

6. **Misc handler tests** (~10% gain)
   - `TestPingCommand_*` — KV write best-effort, reply.
   - `TestMstatsCommand_*` — GetJSON missing → fresh state, formatting.
   - `TestFortytwoCommand_*` — easter egg reply.

7. **Firestore emulator on CI**
   - Add gcloud emulator service to GitHub Actions workflow.
   - Set `FIRESTORE_EMULATOR_HOST` for storage package tests.
   - Verify all 5 currently-skipped tests run on CI.

8. **Coverage gate**
   - Add `-coverprofile=cov.out` to CI test command.
   - Optional: gate at ≥60% (start with warn, escalate to fail when stable).

## Success Criteria
- [ ] Coverage ≥60% (target 65-70%)
- [ ] Every handler in wordle/loldle/loldleemoji/util/misc has at least one happy-path + one error-path test
- [ ] All 5 Firestore emulator tests run on CI
- [ ] `go test -race -count=1 ./...` clean
- [ ] CI runtime under 3 minutes total
- [ ] No flaky tests (run x10 locally clean)

## Risk Assessment
- **Risk:** Recording bot via httptest is brittle if `go-telegram/bot` changes serialization → pin bot library version; add integration smoke test.
- **Risk:** Firestore emulator startup adds 30-60s to CI → acceptable; this is industry-standard.
- **Risk:** Tests over-mock and miss real bugs → use real in-memory KVStore (already standard); recording bot only stubs the network.
- **Risk:** Handler tests duplicate state-layer tests → keep handler tests focused on dispatch + reply text + side effects, not game logic.

## Security Considerations
- Test fixtures use synthetic IDs (no real Telegram user IDs).
- Emulator runs in-CI, not exposed externally.

## Next Steps
- Coverage trend tracked in CI; future modules require ≥60% to merge.
- Phase 06 cleanup (file splits) easier with comprehensive tests.
