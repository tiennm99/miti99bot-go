# Code Review: Phase 04 — Firestore KVStore + KVProvider Abstraction

## Scope
- Files: `internal/storage/{kv_provider,firestore_client,firestore_kv,firestore_provider,firestore_kv_test}.go`, `internal/modules/{registry,registry_test}.go`, `internal/server/router_test.go`, `cmd/server/main.go`, `Makefile`, `go.mod`
- LOC: ~600 new
- Build: `go vet ./...` ✓, `go build ./...` ✓, `go test -race -count=1 ./...` ✓
- Verified intentional spec deviations (collection-per-module isolation, KVProvider abstraction, emulator-gated tests not in CI). Not flagged below.

## Overall Assessment
Solid. The FirestoreKVStore is the right shape, key validation is thorough, query construction (`firestore.DocumentID` constant + `*DocumentRef` value) is API-correct (verified against firestore SDK v1.22.0 source — `toProtoValue` handles `*DocumentRef` → `ReferenceValue`). The KVProvider abstraction is well-justified and contained: modules still receive only a `KVStore`, so isolation cannot leak through the module surface. One real config landmine in `buildProvider` and a couple of robustness gaps documented below.

---

## Critical Issues
None.

---

## High Priority

### H1. `buildProvider` selects Firestore but `NewFirestoreClient` requires `GOOGLE_CLOUD_PROJECT`
**Files:** `cmd/server/main.go:107-127`, `internal/storage/firestore_client.go:18-30`

`buildProvider` decides Firestore is wanted when *either* `GOOGLE_CLOUD_PROJECT` or `FIRESTORE_EMULATOR_HOST` is set. But `NewFirestoreClient` hard-fails when no project ID is supplied. Result: a developer setting only `FIRESTORE_EMULATOR_HOST=localhost:8085` and running `make run` to smoke-test against the emulator will get `storage: GOOGLE_CLOUD_PROJECT is required for Firestore` and the process exits.

Two reasonable fixes:
1. In `buildProvider`, if `cfg.GCPProject == ""` and emulator host is set, default the project to a placeholder (`"emulator-local"` or similar) before calling `NewFirestoreClient`.
2. In `NewFirestoreClient`, if `FIRESTORE_EMULATOR_HOST` is set, allow a default project (the emulator ignores it but the SDK requires *some* string).

The Makefile `test-emulator` target already exports both env vars, so tests are unaffected — but human local-dev workflow hits this.

### H2. `List(prefix)` does not validate prefix
**File:** `internal/storage/firestore_kv.go:154-177`

`Get/Put/Delete` validate the key, but `List` accepts any prefix. A caller passing a prefix containing `/` (which a module might do reflexively if mirroring the JS bot's nested keys) will hand `col.Doc("a/b")` to the SDK, which interprets that as a nested collection path and either errors out or returns nothing — confusing because Put on the same key would have errored cleanly.

Add a `validatePrefix` (or call `validateKey` allowing empty) at the top of `List`.

### H3. `validateKey` length is bytes, but the call uses `len(string)` — *correct*, but worth a doc note
**File:** `internal/storage/firestore_kv.go:55`

`len(key)` on a Go string returns bytes, which matches Firestore's documented 1500-byte limit for the document name. The current code is correct; the worry was rune-vs-byte. Recommend adding a one-line comment to make the intent explicit so a future "fix" doesn't switch it to `utf8.RuneCountInString`.

---

## Medium Priority

### M1. `prefixSuccessor` all-`0xFF` falls back silently to `prefix unchanged` → empty range
**File:** `internal/storage/firestore_kv.go:186-197`

The function returns `prefix` itself when the prefix is entirely `0xFF`, and the test `TestPrefixSuccessor` asserts this (`"\xff": "\xff"`). The code comment claims this "degenerates to empty" — which is true: `Where(>= "\xff", < "\xff")` selects no docs. So a `List("\xff")` against a collection that has a `"\xff"` key would silently return `[]`. For ASCII module keys this is unreachable; for safety on a bytestring backend it's a footgun. Either:
- Drop the `Where(< end)` clause when successor == prefix (degenerates to a one-sided scan from the prefix on, then in-memory filter) — small change.
- Or document explicitly that all-`0xFF` prefixes are unsupported and rely on the validateKey-equivalent on prefix (H2).

### M2. `firestore.Query(col.Query)` cast is a no-op
**File:** `internal/storage/firestore_kv.go:156`

`col.Query` is already `firestore.Query` (it's the embedded type on `CollectionRef`). The explicit conversion is decorative. Replace with `q := col.Query`. Cosmetic only.

### M3. Get accepts `string` value field via type-switch — defensive but not asserted
**File:** `internal/storage/firestore_kv.go:90-99`

Comment says "Firestore may decode small payloads as string." Actually with `Put` always sending `[]byte` (line 70 of `to_value.go` encodes as `BytesValue`), a string return is only possible if a doc was written by another tool. Behavior is correct; consider tightening the comment to "covers docs written by external tooling" so a future reader doesn't conclude the SDK is flaky.

### M4. `MemoryProvider.Base()` is a deliberate test escape hatch — keep, but lock it down
**File:** `internal/storage/kv_provider.go:35`

Method returns the unprefixed `*MemoryKVStore`. It is documented as test-only and only used by `registry_test.go`. Modules never see the `*MemoryProvider`, only a `KVStore` interface — so the leak is not reachable from module code today. Acceptable. Consider moving `Base()` to a `_test.go` build-tagged file (or to a `MemoryProviderForTest` type in a `storagetest` package) in a future tidy pass; not a Phase 04 blocker.

### M5. Shutdown ordering is correct but worth asserting in a comment
**File:** `cmd/server/main.go:42-100`

The sequence `defer stop(); ... defer closeProvider(); ... srv.Shutdown(ctx); return` means the Firestore client closes *after* `srv.Shutdown` returns, which happens after in-flight handlers complete OR the 15s shutdown budget expires. If shutdown times out, in-flight handlers may still be writing to Firestore when `client.Close()` runs — they'll see `grpc: the client connection is closing`. This is acceptable (we're already abandoning), but a one-line comment near `defer closeProvider()` saying "client closes after server.Shutdown returns; in-flight handlers past the 15s budget will see Closed errors" would save someone future debugging.

---

## Low Priority

### L1. `firestoreInitTimeout = 10s` is generous for Cloud Run
**File:** `cmd/server/main.go:31`

10s exceeds typical Cloud Run cold-start budgets (~500ms target). `firestore.NewClient` for a project in the same region usually returns in <100ms; if it stalls, faster failure → faster Cloud Run reschedule. Consider 3-5s. Trade-off: emulator on slow laptops sometimes takes a beat to come up. Not worth changing now.

### L2. Test cleanup uses `Documents(ctx).GetAll()` — fine for emulator, no action needed
**File:** `internal/storage/firestore_kv_test.go:53`

`drainCollection` loads every doc to delete one-by-one. Costs O(N) reads + O(N) deletes. Acceptable for emulator (no quota) and small test fixtures. Just noting.

### L3. `kv_provider.go` has no dedicated test file
**Status:** Memory provider behavior is implicitly covered by `registry_test.go::TestBuild_PerModulePrefixedKV`. A direct unit test of `NewMemoryProvider().For(...)` would catch a regression earlier but isn't critical.

### L4. `cfg.ModuleEnv` exposes `GOOGLE_CLOUD_PROJECT` and `FIRESTORE_EMULATOR_HOST` to modules
**File:** `cmd/server/main.go:163-172`

`secretEnvKeys` strips the three secret tokens but not infrastructure env. Modules will receive `GOOGLE_CLOUD_PROJECT` etc. in `Deps.Env`. Not a security issue (these aren't secrets), but a contract leak — modules might come to depend on these keys, making future refactors awkward. Consider a positive allow-list (`MODULE_*` prefix) when the env-passing contract solidifies, or extend `secretEnvKeys` to include infra keys explicitly. Not Phase 04 work.

---

## Edge Cases (Scout)

1. **GetJSON on a value originally Put as raw bytes (round-trip):** `Put` writes raw bytes. `GetJSON` reads bytes then `json.Unmarshal` — works iff the original raw bytes were valid JSON. If a module mixes `Put([]byte("not-json"))` and `GetJSON(...)`, the latter fails with "invalid character 'n'". This is the correct contract — same as memory_kv.go — but worth a one-line comment on `GetJSON` saying "value must have been JSON-encoded (typically by PutJSON)".

2. **Put then immediate List visibility:** Firestore single-region query reads on `__name__` are strongly consistent (per docs). The emulator matches this. Code is safe; no eventual-consistency window to worry about.

3. **Key length `bytes vs runes`:** `len(string)` is bytes, matches Firestore's 1500-byte cap. Verified — see H3 for note.

4. **Nil `[]byte` in Put:** Encoded as nil-bytes `BytesValue`. Get returns `[]byte(nil)`. Round-trips. Safe.

5. **Concurrent Build calls:** `Build` constructs maps and sequentially populates them. Not called from multiple goroutines in current code (called once in main), so no race. Tests run in parallel but each constructs its own provider. Safe.

6. **`MemoryProvider.For()` called twice with same module name** returns two different `*prefixedStore` wrappers over the same base. Both write to identical underlying keys. Idempotent — fine.

7. **Cron handler Firestore call after shutdown begin:** `srv.Shutdown` waits for handler completion; handlers receive `r.Context()` which is canceled when `Shutdown` returns. A long-running cron will see ctx-canceled and Firestore call returns `context canceled`. Correct behavior.

---

## Positive Observations

- `validateKey` covers Firestore's documented constraints (`/`, `.`, `..`, `__*__`, length, empty) — more thorough than the spec required.
- `Delete` is documented as idempotent, with a test (`TestFirestoreKV_DeleteIdempotent`) verifying the contract.
- `requireEmulator` skip + `t.Cleanup(c.Close)` + per-test `uniqueCollection` is clean — emulator state can't leak between tests.
- `KVProvider` interface is one method (`For`). Easy to mock, hard to misuse.
- `Build` failure modes (nil provider, unknown module, duplicate module, command/cron conflict, validation errors) all error with module name in the message — operationally friendly.
- `splitCSV` handles whitespace and empty entries correctly.
- `buildProvider` returns a non-nil closer in all paths (defensive, matches comment claim).

---

## Recommended Actions

1. **Fix H1** before any developer hits the emulator-only-no-project trap. Smallest patch: in `buildProvider`, when `cfg.GCPProject == ""` and `cfg.FirestoreEmulatorHost != ""`, set project to `"miti99bot-emulator"` before calling `NewFirestoreClient`.
2. **Fix H2:** add prefix validation (or accept-empty key validation) at top of `List`.
3. **Add comment** for H3 (length is bytes, intentional).
4. **Decide on M1:** either the all-`0xFF` test asserts current degenerate behavior (current state, fine if documented in code) or fix the function to fall back to a single-sided range. Pick one and document.
5. The other items are housekeeping — fold into next pass or skip.

---

## Metrics

- Type Coverage: 100% (Go).
- Test Coverage: emulator-gated suite passes; no-emulator suite passes via skip. Memory + Prefixed + Build all directly tested. `kv_provider.go` covered transitively.
- Lint: `go vet` clean.

---

## Unresolved Questions

- **Q1:** Phase 04 plan step 4 says reject empty / `/` / >1500 bytes; the implementation also rejects `.`, `..`, `__*__`. Was the broader rejection intended (matches Firestore docs) or scope creep? Recommend: keep — it's correct.
- **Q2:** Should `List` cap result count to prevent a runaway scan billing the free-tier quota? Plan claims caching is the answer, but List itself has no `Limit`. Phase 5+ concern, but worth surfacing now so module authors don't List-without-bound.
- **Q3:** Is `MemoryProvider.Base()` going to be needed once real Firestore is in CI? If not, mark it for removal in Phase 09 cleanup.

---

**Status:** DONE_WITH_CONCERNS
**Summary:** Implementation is API-correct against firestore SDK v1.22.0; one real config landmine (H1) plus a List-prefix validation gap (H2) deserve a fix before any module ships against this layer.
