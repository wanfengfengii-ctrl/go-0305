---
name: retry-logical-time-gate
description: Instrument retry must be gated by logical time vs next_retry_at
metadata:
  type: project
---

Instrument-call retries are gated by logical time: `RetryInstrument` rejects when `req.LogicalTime < call.NextRetryAt`, so a retry cannot succeed before its scheduled time.

**Why:** Without the gate, a retry issued immediately after the first `timeout` (which returned `next_retry_at=1010`) would run the next script step and turn `success` right away, letting evidence reference a call that succeeded before its planned retry time. The deterministic retry schedule is meaningless if the gate is missing.

**How to apply:** The retry endpoint `POST /api/v1/instrument-calls/{call}/retry` takes a JSON body `{"logical_time": N}`. `RetryInstrument(ctx, callID, RetryInstrumentRequest{LogicalTime})` checks `req.LogicalTime < existing.NextRetryAt` → `INVALID_REQUEST`. The retry-schedule formula itself (`LogicalTime + Attempt*retryInterval`) is unchanged — only the gate was missing.
