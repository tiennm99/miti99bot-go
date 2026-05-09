---
phase: 3
title: "DynamoDB KV provider"
status: pending
priority: P1
effort: "4h"
dependencies: [1]
---

# Phase 03: DynamoDB KV provider

## Overview
Add `DynamoDBKVStore` + `DynamoDBProvider` as a sibling to the existing Firestore impl, satisfying the same `KVStore` / `KVProvider` interface. Selectable via `KV_PROVIDER=dynamodb|firestore|memory` env. Default in production: `dynamodb`. Firestore impl preserved for parity tests.

## Requirements
- **Functional:** All existing modules' KV ops (Get/Put/Delete/List + JSON convenience methods) work against DynamoDB with byte-for-byte parity to Firestore where observable.
- **Non-functional:** Single-table design. On-demand billing. P99 < 50ms for Get/Put. List() with prefix uses `Query` (not `Scan`) — must be cheap.

## Architecture
**Single-table schema (composite key):**
```
TableName: miti99bot-data
PK (pk):   string  = moduleName              (e.g. "wordle")
SK (sk):   string  = caller-provided key     (e.g. "user:42:state")
attrs:
  value:     binary  raw bytes
  updatedAt: number  epoch nanos (parity with Firestore impl)
```

Composite key is the canonical DynamoDB shape for prefix-scan workloads: `Query` supports `begins_with(sk, :prefix)` on the **sort key**, but only `=` on the partition key — so the sort key holds the user-supplied key and the partition key holds the module name (which gives free isolation by partition).

**Operations:**
- `Get(key)` → `GetItem(pk=module, sk=key)` with `ConsistentRead: true` (parity with Firestore strong read)
- `Put(key, val)` → `PutItem(pk=module, sk=key, value=val, updatedAt=now)`
- `Delete(key)` → `DeleteItem(pk=module, sk=key)`
- `List(prefix)` → `Query(pk=module AND begins_with(sk, prefix))` paginated

**Provider isolation:** `For(moduleName)` returns a `DynamoDBKVStore` bound to that module name; the partition key is the isolation boundary. No prefix wrapping needed at this layer.

**Reserved word handling:** `value` is reserved in DynamoDB expressions; resolved via `ExpressionAttributeNames` (`#v` → `value`).

## Related Code Files
- Create: `internal/storage/dynamodb_client.go` — AWS SDK v2 client init, region from env
- Create: `internal/storage/dynamodb_kv.go` — `DynamoDBKVStore` (Get/Put/Delete/List + JSON helpers)
- Create: `internal/storage/dynamodb_provider.go` — `DynamoDBProvider`, `For()` returns module-bound store
- Create: `internal/storage/dynamodb_kv_test.go` — uses `localstack` or DynamoDB Local via `testcontainers-go`
- Create: `internal/storage/dynamodb_provider_test.go` — cross-module isolation
- Create: `internal/storage/parity_test.go` (optional) — runs the same op sequence against Memory + Firestore + DynamoDB and asserts identical observables
- Modify: `cmd/server/main.go` — read `KV_PROVIDER`, branch on value; default `dynamodb` when running on Lambda (detect via `AWS_LAMBDA_FUNCTION_NAME` env)
- Modify: `go.mod` — add `github.com/aws/aws-sdk-go-v2`, `…/config`, `…/service/dynamodb`, `…/feature/dynamodb/attributevalue`, `…/feature/dynamodb/expression`

## Implementation Steps
1. Add SDK deps. `go mod tidy`.
2. Implement `DynamoDBKVStore` with the same method set as `FirestoreKVStore`. Key composition: `pk = moduleName + "#" + key`.
3. `List(prefix)` implementation — `Query` with `KeyConditionExpression` on PK begins-with semantics (use range trick or `BEGINS_WITH` on PK; AWS docs: `BEGINS_WITH` works on sort key only, so use the start/end range trick on PK directly).
4. JSON helpers (`GetJSON`, `PutJSON`) — mirror Firestore impl exactly: marshal/unmarshal with `encoding/json`, store as binary, `ErrNotFound` semantics preserved.
5. Tests with DynamoDB Local (Docker image `amazon/dynamodb-local`). Add `make dynamodb-local` target. Skip if `DYNAMODB_LOCAL_URL` env unset (so CI without Docker can still build).
6. Cross-module isolation test: Put `wordle#k=A`, `loldle#k=B`, assert `wordleStore.Get("k") == A`, `loldleStore.Get("k") == B`, `loldleStore.List("") returns ["k"]` (not `[wordle#k, loldle#k]`).
7. Wire provider selection in `main.go`:
   ```go
   switch os.Getenv("KV_PROVIDER") {
   case "dynamodb": kv = storage.NewDynamoDBProvider(...)
   case "firestore": kv = storage.NewFirestoreProvider(...)
   default: kv = storage.NewMemoryProvider()
   }
   ```
8. Manual smoke against deployed Lambda: send `/start`, then verify `aws dynamodb scan --table-name miti99bot --max-items 5` shows expected keys.

## Success Criteria
- [ ] `dynamodb_kv_test.go` passes against DynamoDB Local
- [ ] `dynamodb_provider_test.go` passes (cross-module isolation)
- [ ] `parity_test.go` (if added) passes — Memory ≡ Firestore ≡ DynamoDB on observables
- [ ] `KV_PROVIDER=dynamodb` works in deployed Lambda end-to-end
- [ ] One full game session of `/wordle` → state persists across invocations (cold-start safe)
- [ ] List() with prefix returns expected keys, no `Scan` calls in CloudWatch metrics

## Risk Assessment
- **`List` performance** if a module accumulates >1k keys — DynamoDB Query handles this fine via paginated results. Confirm caller iterates pages (or all calls fit in one page).
- **Item size limit (400 KB)** — modules generally store small JSON; document the cap and add a `len(val) > 380*1024 → error` guard.
- **Eventual consistency** — DynamoDB defaults to eventually consistent reads. Use `ConsistentRead: true` in `Get` to match Firestore's strong default. Costs 2× the RCU but on-demand absorbs it.
- **Reserved word `value`** — DynamoDB reserves many names; use `ExpressionAttributeNames` `#v = "value"` to avoid the conflict.
- **AWS SDK v2 cold-start tax** (~80ms) — acceptable; cache client instance globally in `init()` or pkg-level var.

## Open questions
1. TTL attribute for ephemeral keys (e.g. wordle daily state)? Add optional `ttl` Number attr; modules opt in via a new method or skip for v1.
2. Use single PK or PK+SK? Sticking with single PK for KISS — no current module needs sort-key queries.
3. Encryption — DynamoDB uses AWS-owned KMS by default (free); switch to AWS-managed only if compliance demands it.
4. Backup strategy — point-in-time recovery is paid; for free-tier hobby use, accept "no backup" and document.
