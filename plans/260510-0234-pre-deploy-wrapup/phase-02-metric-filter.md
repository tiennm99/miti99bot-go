---
phase: 2
title: "Cold-start metric filter"
status: pending
priority: P3
effort: "30m"
dependencies: []
---

# Phase 02: Cold-start metric filter

## Overview
Add a CloudWatch Logs metric filter that extracts Lambda's `Init Duration` from the auto-emitted `REPORT` line so the AWS-port plan's "P95 < 1.5s" abort criterion is measurable from day one.

## Requirements
- **Functional:** A custom metric `miti99bot/ColdStartInitDuration` exists; samples appear in CloudWatch Metrics within 5 minutes of cold-start.
- **Non-functional:** Stays inside CloudWatch's always-free 10 custom metrics. No additional ingest cost (filter operates on existing log stream).

## Architecture
Lambda emits a synthetic `REPORT` line at the end of every invocation. On a cold start, that line includes `Init Duration: <ms>`. A `AWS::Logs::MetricFilter` parses the line and emits a custom metric value into `miti99bot/ColdStartInitDuration` namespace.

```
Lambda invocation → CloudWatch log stream →
   filter pattern matches "REPORT ... Init Duration: <n>" →
     publish metric (Namespace: miti99bot, Name: ColdStartInitDuration, Value: <n>)
```

## Related Code Files
- Modify: `template.yaml` — add `ColdStartMetricFilter` resource

## Implementation Steps
1. Append to `template.yaml` after `BotFunctionLogGroup`:
   ```yaml
   ColdStartMetricFilter:
     Type: AWS::Logs::MetricFilter
     Properties:
       LogGroupName: !Ref BotFunctionLogGroup
       FilterPattern: '[report="REPORT", reqid_label="RequestId:", reqid, dur_label="Duration:", dur, dur_unit="ms", bill_label="Billed", bill_dur_label, bill_dur, bill_unit, mem_label, mem_size_label, mem_size, mem_unit, max_label="Max", max_used_label="Memory", max_used_label2="Used:", max_used, max_used_unit, init_label="Init", init_dur_label="Duration:", init_dur, init_unit="ms"]'
       MetricTransformations:
         - MetricName: ColdStartInitDuration
           MetricNamespace: miti99bot
           MetricValue: $init_dur
           Unit: Milliseconds
   ```
2. Validate locally: `make sam-validate` (offline lint). Should pass.
3. After AWS-port Phase 01 manual deploy completes, verify:
   - `aws logs describe-metric-filters --log-group-name /aws/lambda/miti99bot-aws-port-bot`
   - Trigger a cold start (`aws lambda update-function-configuration --environment ... ` flip).
   - `aws cloudwatch get-metric-statistics --namespace miti99bot --metric-name ColdStartInitDuration --statistics Average,Maximum --start-time ... --end-time ... --period 300`

## Success Criteria
- [ ] `template.yaml` has `ColdStartMetricFilter` resource
- [ ] `sam validate` passes
- [ ] Post-deploy: metric filter visible in AWS console, samples flow within 5 min of a cold start

## Risk Assessment
- **Filter pattern brittleness** — Lambda's REPORT format is stable but unofficial. Mitigation: pattern uses positional + label parsing, tolerant of whitespace; if AWS changes format, filter just stops matching (no error, just zero data).
- **Warm-only invocations don't include `Init Duration`** — pattern won't match those, which is correct; we only want cold-start samples.

## Open questions
1. Capture `Duration:` (warm + cold) too as a separate metric? YAGNI — request latency is already in CloudWatch's built-in `Duration` metric for the function.
2. Per-region dashboard? Skip — solo dev uses Insights queries.
