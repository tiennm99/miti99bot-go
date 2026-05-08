---
phase: 4
title: "Firestore KVStore + per-module prefixing"
status: done
priority: P1
effort: "4h"
dependencies: [3]
---

# Phase 04: Firestore KVStore + per-module prefixing

## Overview
Implement `FirestoreKVStore` against the `KVStore` interface from Phase 03. One Firestore collection per module (`<module>`), each KV entry one document. Test against the local Firestore emulator. Provide an in-memory fake for module-level unit tests so they don't need the emulator.

## Requirements
- Functional: `Get/GetJSON/Put/PutJSON/Delete/List` work against Firestore. Per-module isolation via collection name. JSON values stored as `value` field on the document.
- Non-functional: P50 read ≤80ms warm, ≤500ms cold. Connection reused across requests via package-level `*firestore.Client`. Free-tier-aware: avoid `Query.GetAll` on hot paths.

## Architecture

```
internal/storage/
├── firestore_kv.go      ← FirestoreKVStore impl
├── firestore_client.go  ← package-level client (lazy init, project ID from env)
└── firestore_kv_test.go ← runs against emulator if FIRESTORE_EMULATOR_HOST set
```

Firestore document shape:
```
collection: <moduleName>
document id: <key>     ← URL-safe key (rejects `/` per Firestore rules)
fields:
  value: bytes | string | map  ← raw bytes for Put, JSON-marshaled struct for PutJSON
  updatedAt: timestamp
```

`List(prefix)` uses `collection.Where(firestore.DocumentID(), ">=", prefix).Where(firestore.DocumentID(), "<", prefixSuccessor(prefix))`.

## Related Code Files
- Create: `internal/storage/firestore_kv.go`, `firestore_client.go`, `firestore_kv_test.go`
- Modify: `cmd/server/main.go` to initialize Firestore client, pass to module Deps
- Modify: `internal/modules/dispatcher.go` Deps construction
- Create: `Makefile` target `test-emulator` (start emulator, run tests)

## Implementation Steps
1. Add dep: `go get cloud.google.com/go/firestore`.
2. `firestore_client.go`: singleton `func Client(ctx) (*firestore.Client, error)` reading `GOOGLE_CLOUD_PROJECT` from env. Reuse across requests.
3. `firestore_kv.go`:
   - Struct `FirestoreKVStore { c *firestore.Client; collection string }`.
   - `Get(ctx, key)`: `c.Collection(collection).Doc(key).Get(ctx)`. Map `codes.NotFound` → `ErrNotFound`. Return `value` field as bytes.
   - `Put(ctx, key, val)`: `Doc(key).Set(ctx, map{"value": val, "updatedAt": time.Now()})`.
   - `GetJSON/PutJSON`: marshal/unmarshal via `encoding/json`.
   - `Delete`: `Doc(key).Delete(ctx)`.
   - `List(prefix)`: `Where(DocumentID >= prefix).Where(DocumentID < successor)`. Iterator → slice of doc IDs.
4. Key validation: reject `/`, empty string, length >1500 bytes (Firestore limit).
5. `firestore_kv_test.go`: skip if `FIRESTORE_EMULATOR_HOST` not set. Round-trip Put/Get/Delete/List/PutJSON/GetJSON/NotFound.
6. Update `internal/storage/memory_kv.go` to support `List(prefix)` symmetrically (iterate map keys).
7. Update `cmd/server/main.go`:
   - Init Firestore client at startup.
   - For each module, pass `Prefixed(NewFirestoreKVStore(client, module.Name), module.Name)` (collection name = module name = prefix; equivalent to single-collection prefixing).
   - Actually: drop the `Prefixed` wrapper for Firestore — collection itself isolates. `Prefixed` only used with `MemoryKV` for tests.
8. Add `Makefile`: `firestore-emulator: gcloud emulators firestore start --host-port=localhost:8085` and `test: FIRESTORE_EMULATOR_HOST=localhost:8085 go test ./...`.

## Success Criteria
- [x] All KV ops round-trip against emulator (`firestore_kv_test.go`, runs via `make test-emulator`)
- [x] In-memory fake matches Firestore semantics for List ordering + ErrNotFound
- [x] Two modules writing to same key name → no collision (`TestBuild_PerModulePrefixedKV` for memory backend; collection-per-module IS the isolation for Firestore — test exists in registry layer)
- [x] `go test -race -count=1 ./...` green (Firestore tests skip cleanly without emulator)

## Implementation deviations
- Spec step 3 (wrap Firestore in `Prefixed`) contradicts step 7 (drop the wrapper). We followed step 7 — collection-per-module IS isolation. Memory backend keeps `Prefixed` because all modules share one in-process store.
- Introduced `KVProvider` interface (not in spec): `MemoryProvider` wraps base+Prefixed, `FirestoreProvider` returns one collection per module. `modules.Build` now takes `KVProvider`+env map instead of base `Deps`. Cleaner: modules never see the backend choice.
- Backend selection in `cmd/server/main.go`: Firestore when `GOOGLE_CLOUD_PROJECT` or `FIRESTORE_EMULATOR_HOST` is set (latter supplies a placeholder project ID for the SDK); otherwise in-memory.
- `validateKey` rejects more than spec required: empty, `/`, `.`, `..`, `__namespace__`, > 1500 bytes. `validatePrefix` runs the same check on `List` arguments.
- Binary size 6.4 MB → 17 MB after Firestore SDK + gRPC. Within Phase 02's ≤20 MiB target. Distroless image ≈19 MB.

## Code review
[Phase 04 review](reports/code-reviewer-260508-2333-phase04-firestore-kv.md) — 0 critical, 3 high (H1 emulator-only-no-project trap, H2 List-prefix unvalidated, H3 bytes-vs-runes doc) all addressed in same session; M1 prefixSuccessor all-0xFF degeneracy documented; remaining mediums deferred.

## Risk Assessment
- **Risk**: Firestore document IDs reject `/` but JS keys may contain them (e.g. nested loldle state). **Mitigation**: encode `/` → `_` in Put, decode on Get. Document the mapping. Or use base64 for arbitrary keys.
- **Risk**: 50k reads/day hard cap. Listing leaderboards on every request hits this fast. **Mitigation**: cache hot reads in process memory with 5-minute TTL — free for warm instance, costs 0 reads.
- **Risk**: Emulator behavior diverges from prod (e.g. timestamp resolution, indexes). **Mitigation**: smoke a 50-key Put/List against real Firestore at end of phase.

## Rollback
Drop the Firestore client init, revert to `MemoryKV` for all modules. Modules continue working but lose persistence.
