# Design: Webhook Subscriptions for Corridor Transitions

**Status**: Proposed. Implementation is follow-up.

**Related**: [spike-alerting-semantics.md](spike-alerting-semantics.md), [#694](https://github.com/Wayfare-labs/wayfare/issues/694)

---

## 1. Problem Statement

The spike [spike-alerting-semantics.md](spike-alerting-semantics.md) defines *what* transitions deserve notification (C1–C3, O1–O3) and *what must never alert* (Rules 1–5). It also identifies the two audiences: **consumers** (wallets/PSPs) and **operators** (deployment owners). What it does not have is a *delivery channel*.

This design adds a webhook delivery layer that:
- Respects the non-custodial, no-accounts posture (no user management, no authentication of subscribers)
- Works within the two-dependency policy (stdlib + `github.com/shopspring/decimal`)
- Provides subscription storage, payload signing, retries, and abuse limits

---

## 2. Threat Model

| Threat | Mitigation |
|--------|------------|
| **Subscriber enumeration** | Subscription IDs are opaque (SHA-256 of target URL + secret); list endpoint requires admin token |
| **Replay attacks** | Payloads include `timestamp` and `event_id`; recipients must reject events older than 5 min or already seen |
| **Webhook flooding (abuse)** | Per-subscription rate limit (max 10 deliveries/min), exponential backoff, circuit breaker after 10 consecutive failures |
| **Payload tampering** | HMAC-SHA256 signature in `X-Wayfare-Signature` header; secret never logged |
| **SSRF via subscriber URLs** | Allowlist: only `https://` to public IPs; block private ranges, localhost, metadata endpoints |
| **Secret leakage** | Secrets generated once, shown only at creation; rotation via DELETE + POST |

---

## 3. Subscription Model

### 3.1 No Identity Required

Consistent with the project's no-accounts posture, **subscriptions carry no identity**. A subscription is:

```go
type WebhookSubscription struct {
    ID        string   `json:"id"`         // sha256(target_url + secret)[:16]
    TargetURL string   `json:"target_url"` // https:// only, validated at create
    Secret    string   `json:"-"`          // 32-byte random, base64; shown once at create
    Events    []string `json:"events"`     // e.g. ["integrity_transition", "verdict_change"]
    CreatedAt time.Time `json:"created_at"`
    Disabled  bool     `json:"disabled"`   // set true after 10 consecutive failures
}
```

- **No email, no user ID, no API key**. The secret *is* the credential.
- Subscription ID is deterministic from `target_url + secret` so a caller can recompute it, but the secret is never returned after creation.
- `Events` filters which transition types are delivered. Default: `["integrity_transition"]` (C1). Future: `["verdict_change", "recommendation_flip", "reference_degraded", "check_critical_fail", "sweep_gap"]` (C2, C3, O1, O2, O3).

### 3.2 Storage

**File-based, same directory as run store** (`data/webhooks.ndjson`). Each line is a JSON-encoded `WebhookSubscription` (secret omitted on read).

```go
// WebhookStore persists subscriptions.
type WebhookStore interface {
    Create(ctx context.Context, sub *WebhookSubscription) error
    Get(ctx context.Context, id string) (*WebhookSubscription, error)
    List(ctx context.Context) ([]*WebhookSubscription, error) // secret always empty
    Delete(ctx context.Context, id string) error
    Update(ctx context.Context, sub *WebhookSubscription) error // only Disabled, Events mutable
}
```

- Single file, append-on-create, rewrite-on-update/delete (store is small: ~100 subscriptions max).
- Verified at load (JSON parse + URL validation); no hash chain needed — subscriptions are not tamper-evident evidence.

### 3.3 Admin Token (Optional)

Listing subscriptions requires `X-Admin-Token` header matching `WAYFARE_ADMIN_TOKEN` env var. Without it, only `POST /webhooks` (create) and `DELETE /webhooks/{id}` (with secret in body) work. This keeps the common path keyless.

---

## 4. Payload Format

```json
{
  "event_id": "sha256:corridor:seq:transition_type",
  "event_type": "integrity_transition",
  "timestamp": "2026-11-21T08:43:27Z",
  "corridor": "USDC-NGNC",
  "transition": {
    "previous_integrity": "DIRECT",
    "current_integrity": "DERIVATIVE",
    "previous_run_at": "2026-11-21T02:00:00Z",
    "current_run_at": "2026-11-21T08:00:00Z",
    "previous_depends_on": [],
    "current_depends_on": ["NGNT"],
    "transition_type": "INTEGRITY_STATE_CHANGE"
  }
}
```

- `event_id` is deterministic: `sha256(corridor + "|" + current_run_seq + "|" + transition_type)[:32]` — enables recipient deduplication.
- `event_type` matches subscription filter.
- Payload is **exactly the wire types from `runstore.IntegrityTransition`** (no new shapes).

---

## 5. Signing

- **Algorithm**: HMAC-SHA256
- **Header**: `X-Wayfare-Signature: sha256=<hex>`
- **Signed data**: `timestamp + "\n" + event_id + "\n" + body` (canonical JSON, no whitespace)
- **Verification** (recipient):
  1. Check `timestamp` within 5 min of now
  2. Compute HMAC with stored secret
  3. Compare constant-time with header
  4. Check `event_id` not already processed (idempotency)

Secret is 32 random bytes, base64-encoded. Generated at subscription creation, shown once.

---

## 6. Delivery & Retries

### 6.1 Trigger

Delivery runs **after each successful sweep** in the scheduler (`monitor.Scheduler.RunOnce`). For each corridor measured:
1. Call `runstore.DetectLatestTransition`
2. If transition exists and not `UNKNOWN`, build payload
3. For each matching subscription (filter by `Events`), enqueue delivery

### 6.2 Retry Policy

| Attempt | Delay | Max Attempts |
|---------|-------|--------------|
| 1 | immediate | — |
| 2 | 30 s | — |
| 3 | 2 min | — |
| 4 | 10 min | — |
| 5 | 1 hour | 5 total |

- **Jitter**: ±10% per attempt
- **Timeout**: 10 s per attempt (configurable via `WEBHOOK_TIMEOUT`)
- **Circuit breaker**: After 10 consecutive failures (across all events), subscription `Disabled = true`. Admin must re-enable.

### 6.3 Abuse Limits

- **Per-subscription rate limit**: 10 deliveries/minute (token bucket, refill 1/6s)
- **Global rate limit**: 100 deliveries/minute across all subscriptions
- **Payload size cap**: 16 KB (current payload ~1 KB)
- **Target URL validation**: `https://` only; DNS resolves to public IP; rejects private ranges (10/8, 172.16/12, 192.168/16, 127/8, 169.254/16), metadata endpoints (169.254.169.254, [::ffff:169.254.169.254])

---

## 7. HTTP Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/api/v1/webhooks` | None | Create subscription. Returns `{id, secret}` once. |
| `GET` | `/api/v1/webhooks` | Admin token | List subscriptions (secrets omitted). |
| `GET` | `/api/v1/webhooks/{id}` | None* | Get subscription (secret omitted). *Requires secret in `X-Webhook-Secret` header. |
| `DELETE` | `/api/v1/webhooks/{id}` | Secret in body | Delete subscription. Body: `{"secret": "..."}`. |
| `PATCH` | `/api/v1/webhooks/{id}` | Secret in body | Update `events` or re-enable. Body: `{"secret": "...", "events": [...], "disabled": false}`. |

### 7.1 Create Request

```json
POST /api/v1/webhooks
{
  "target_url": "https://wallet.example.com/wayfare-webhook",
  "events": ["integrity_transition"]
}
```

### 7.2 Create Response (201)

```json
{
  "id": "a1b2c3d4e5f67890",
  "secret": "base64-encoded-32-bytes",
  "target_url": "https://wallet.example.com/wayfare-webhook",
  "events": ["integrity_transition"],
  "created_at": "2026-11-21T08:43:27Z",
  "disabled": false
}
```

**The `secret` is never returned again.** Caller must store it.

---

## 8. Two-Dependency Policy Compliance

| Dependency | Purpose | Status |
|------------|---------|--------|
| `github.com/shopspring/decimal` | Already in project | ✅ Allowed |
| `crypto/hmac`, `crypto/sha256`, `encoding/hex`, `encoding/base64`, `net/http`, `net/url`, `time`, `sync`, `context` | Stdlib | ✅ Allowed |

**No new external dependencies.**

---

## 9. Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| `WEBHOOK_STORE_DIR` | `./data` | Directory for `webhooks.ndjson` |
| `WEBHOOK_TIMEOUT` | `10s` | HTTP timeout per delivery attempt |
| `WEBHOOK_MAX_RETRIES` | `5` | Max delivery attempts per event |
| `WEBHOOK_RATE_LIMIT` | `10` | Max deliveries/min per subscription |
| `WEBHOOK_GLOBAL_RATE_LIMIT` | `100` | Max deliveries/min globally |
| `WAYFARE_ADMIN_TOKEN` | (empty) | If set, required for `GET /webhooks` |

---

## 10. Maintainer Decision

**Decision**: Approved for implementation.

**Rationale**:
- The spike's alert catalogue (C1–C3, O1–O3) is useless without delivery.
- Webhooks are the integration primitive wallets and anchors already use.
- The design respects no-accounts: subscription secret *is* the credential.
- File-based storage keeps operational complexity zero (same as run store).
- Two-dependency policy satisfied.

**Implementation Order**:
1. Storage + endpoints (create, list, get, delete, patch)
2. Signing + payload construction
3. Scheduler integration (enqueue after `reportTransition`)
4. Delivery worker with retries, circuit breaker, abuse limits
5. Tests (unit + integration with httptest)

---

## 11. Out of Scope

- Email, Slack, PagerDuty, or other channels (webhooks are the primitive; operators can forward)
- Escalation timeouts ("still UNUSABLE after N days") — separate capability
- Subscription authentication via OAuth/OIDC — violates no-accounts
- Persistent delivery queue (in-memory with retry is sufficient at this scale; a failed sweep re-detects transitions)

---

## 12. Related Documentation

- [spike-alerting-semantics.md](spike-alerting-semantics.md) — alert catalogue and rules
- [runstore/transition.go](../runstore/transition.go) — transition detector (C1 implemented)
- [monitor/monitor.go](../monitor/monitor.go) — scheduler that calls `reportTransition`
- [docs/deployment.md](deployment.md) — operational context