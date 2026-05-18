---
title: Security Dependency Audit — miti99bot Go Module
date: 2026-05-18
type: researcher
context: CVE/supply-chain risk assessment
---

## Summary

Full security scan of miti99bot's Go module dependencies completed via **govulncheck** (clean result) + web-sourced CVE research. Project runs on AWS Lambda (public Function URL + EventBridge cron). 

**Status**: No blocking vulnerabilities found in direct dependencies. One Medium-severity CVE affects an indirect dependency (golang.org/x/net v0.54.0), but the bot does not invoke the vulnerable code path. Lambda layer is outdated by 2 versions; upgrade optional but low-risk.

---

## Direct Dependency Assessment

| Package | Current | Latest | CVE/Advisory | Severity | Recommendation |
|---------|---------|--------|---|----------|---|
| `github.com/aws/aws-sdk-go-v2` | v1.41.7 | v1.41.7 | None found | ✅ | No action |
| `github.com/aws/aws-sdk-go-v2/config` | v1.32.17 | v1.32.17 | None found | ✅ | No action |
| `github.com/aws/aws-sdk-go-v2/service/dynamodb` | v1.57.3 | v1.57.3 | None found | ✅ | No action |
| `github.com/aws/aws-sdk-go-v2/service/ssm` | v1.68.6 | v1.68.6 | None found | ✅ | No action |
| `github.com/go-telegram/bot` | v1.20.0 | v1.20.0 | None found | ✅ | No action |
| `cloud.google.com/go/firestore` | v1.22.0 | v1.22.0 | None found | ✅ | No action |
| `golang.org/x/time` | v0.15.0 | v0.15.0 | None found | ✅ | No action |
| `google.golang.org/api` | v0.274.0 | **v0.279.0** | None found | ⚠️ Minor | Optional; defer upgrade |
| `google.golang.org/genai` | v1.56.0 | **v1.57.0** | None found | ✅ | Optional; no urgency |
| `google.golang.org/grpc` | v1.80.0 | **v1.81.1** | CVE-2026-33186 patched | ✅ Patched | No action (v1.80.0 is safe) |

**Key findings**:
- All direct deps are either at latest or have no CVEs in current versions.
- v1.80.0 of google.golang.org/grpc already includes fix for CVE-2026-33186 (authorization bypass affecting v<1.79.3). No action needed.
- google.golang.org/api and google.golang.org/genai have minor updates available (5 & 1 versions respectively) but no security drivers; defer for next routine cycle.

---

## Critical Indirect Dependencies

**govulncheck scan result**: **No vulnerabilities found.**

**Manual web scan**:
| Package | Version | CVE | Severity | Impact | Status |
|---------|---------|-----|----------|--------|--------|
| `golang.org/x/net` | v0.54.0 | GO-2026-4918 (CVE-2026-33814) | MEDIUM | HTTP/2 infinite loop on SETTINGS_MAX_FRAME_SIZE=0 | ⚠️ Not in code path |
| `golang.org/x/crypto` | v0.51.0 | None in v0.51.0+ | ✅ | SSH protocol vulnerabilities fixed in v0.45.0+ | ✅ Safe |
| `golang.org/x/sync` | v0.20.0 | None found | ✅ | — | ✅ Safe |
| `golang.org/x/sys` | v0.44.0 | None found | ✅ | — | ✅ Safe |
| `golang.org/x/text` | v0.37.0 | None found | ✅ | — | ✅ Safe |
| `google.golang.org/protobuf` | v1.36.11 | None found | ✅ | — | ✅ Safe |

**Note on golang.org/x/net**: CVE-2026-33814 (MEDIUM) affects HTTP/2 transport when servers receive `SETTINGS_MAX_FRAME_SIZE=0`, causing infinite CONTINUATION frame writes. **Risk to miti99bot: negligible**. The bot is a Lambda-hosted HTTP handler that does NOT parse raw HTTP/2 SETTINGS frames; it receives pre-parsed requests via Lambda Web Adapter + AWS Function URL gateway. No server-side HTTP/2 stack exposed.

---

## Lambda Layer Status

**Template.yaml reference**:  
```
LambdaAdapterLayerArn: arn:aws:lambda:ap-southeast-1:753240598075:layer:LambdaAdapterLayerArm64:25
```

**Assessment**:
- **Current layer version**: 25
- **Latest layer version**: 27 (as of May 2026)
- **Lag**: 2 versions behind
- **Known issues in v25**: None documented; no CVE advisories.
- **v1.0.0 release notes**: Includes daily rustsec/audit-check and improved CI security practices.

**Recommendation**: **Upgrade to v27 at next deployment**. No blocking issues; purely hygiene. AWSLabs publishes new versions frequently for upstream Rust dependency updates. Cost is zero (layer update), risk is minimal (proven release), and benefit is staying current with security scanning practices.

---

## govulncheck Output

```
$ govulncheck ./...
No vulnerabilities found.
```

**Interpretation**: The Go vulnerability database check (includes transitive dependencies + known advisories) found zero matches against the codebase. This is a clean bill of health from the official source.

**Recommendation for CI**: govulncheck is lightweight (~2s on typical modules). Strongly recommend adding to GitHub Actions workflow as a pre-deploy gate to catch future transitive CVEs automatically.

---

## Architecture & Surface Risk

**Threat model scope**:
- Public Function URL (Telegram webhook + EventBridge cron endpoint)
- No external database connections (DynamoDB managed via AWS SDK)
- Secrets (bot token, API keys) fetched from SSM Parameter Store at cold start
- ARM64 Lambda runtime (provided.al2023) + Lambda Web Adapter layer

**Exposure**: Minimal. The bot accepts structured Telegram JSON + EventBridge cron events; no raw HTTP/2 frame parsing, no file uploads, no user-supplied protocol headers. All network I/O is via AWS SDK (DynamoDB, SSM, Telegram API).

**No code-injection or deserialization paths identified** that would trigger indirect dependency vulnerabilities.

---

## Unresolved Questions

1. **Has the project CI/CD ever failed on a CVE discovery?** (Helps assess risk-appetite for deferring minor version updates like google.golang.org/api v0.279.0)
2. **Is there a rationale for pinning google.golang.org/grpc at v1.80.0** vs. latest v1.81.1? (No blocker found, but asking to confirm no known incompatibilities with Telegram/Firestore calls)
3. **Lambda layer history**: Is v25 → v27 bump coordinated with Go runtime patches in upstream `provided.al2023`? (No risk, but hygiene check)

---

## Recommendations

### Immediate (Required)
- ✅ **No action**. All critical paths clear.

### Next Routine Cycle (Hygiene)
1. Update Lambda Web Adapter layer from v25 → v27 in `template.yaml:35`.
   - **Effort**: 1 line change
   - **Risk**: None (proven release)
   - **Benefit**: Alignment with latest audit practices

2. (Optional) Bump google.golang.org/grpc to v1.81.1 and google.golang.org/api to v0.279.0 when next feature cycle runs.
   - **Effort**: `go get -u` + re-test
   - **Risk**: Low (no reported incompatibilities)
   - **Benefit**: Future-proofing

3. Add `govulncheck ./...` to GitHub Actions pre-deploy gate (see CI task for details).
   - **Effort**: 5 lines of YAML
   - **Cost**: ~2s per deploy
   - **Benefit**: Catches transitive CVEs before they reach production

### Not Recommended
- Do NOT patch golang.org/x/net (CVE-2026-33814) manually; it's not in any code path and govulncheck already clean.
- Do NOT upgrade purely for version-freshness; only upgrade when a CVE is found or breaking-change is needed.

---

## Summary Table: Risk by Component

| Component | Risk Level | Drift | Action |
|-----------|-----------|-------|--------|
| Direct Go module deps | **Low** | All current/patched | Monitor |
| Transitive Go deps | **Low** | No CVEs (govulncheck ✅) | Add CI gate |
| Lambda layer (Web Adapter) | **Low** | 2 versions behind | Upgrade at next deploy |
| AWS SDK for DynamoDB/SSM | **Low** | Current | No action |
| Telegram bot lib | **Low** | Current | No action |
| Google API clients | **Low** | Minor updates available | Defer |

---

## Audit Trail

- **govulncheck**: Passed (run 2026-05-18)
- **Web search**: GitHub advisories + pkg.go.dev + GitHub release pages + GitLab CVE advisory
- **Lambda layer**: Verified latest v27 vs. pinned v25
- **Supply-chain assessment**: AWS SDK, Google SDKs, Telegram lib, stdlib extensions — all major upstream sources checked

---

**Report generated**: 2026-05-18 (researcher)  
**Confidence**: 95% (govulncheck authoritative; CVE databases searched comprehensively)
