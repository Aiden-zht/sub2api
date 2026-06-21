# XunhuPay Parity Design

Date: 2026-06-21
Status: approved-for-implementation
Scope: sub2api payment subsystem

## Goal

Bring `xunhupay` to parity with existing platform payment providers by combining:

- frontend behavior aligned with EasyPay-style aggregate checkout (`qrcode` and `popup` flows)
- backend safety guarantees aligned with official providers (snapshot identity, strict validation, auditable fulfillment, refund lifecycle)

This work must avoid money-loss scenarios caused by false-success parsing, merchant drift, misrouted callbacks, or capability switches that expose unsupported operations.

## Current Problems

1. XunhuPay checkout is live, but its order snapshot does not freeze merchant identity the same way official providers do.
2. `errcode` parsing is too permissive; malformed upstream responses can be treated like success or harmless pending states.
3. Refund UI/configuration can be exposed before the provider has full refund support and response normalization.
4. XunhuPay behavior is implemented as a provider-specific exception instead of being clearly aligned with the platform's existing payment safety shell.

## Design

### 1. Provider responsibility

`backend/internal/payment/provider/xunhupay.go` remains the XunhuPay protocol adapter and is responsible for:

- create payment
- query order
- verify notification signature and parse callback payload
- request refund
- normalize refund/query/provider statuses into the platform contract

The provider does not decide final fulfillment. It only returns verified, normalized upstream facts.

### 2. Platform safety shell

Existing fulfillment and reconciliation flows remain the source of truth for whether an order becomes paid/refunded. XunhuPay must pass through the same safety checks used by official providers:

- provider instance binding
- provider key consistency
- merchant identity snapshot validation
- amount validation
- auditable status transitions

Webhook handling, active reconciliation, and refund execution must all use the historical order binding rather than the latest mutable provider config.

### 3. Historical order snapshot

When a XunhuPay order is created, the order snapshot must persist:

- `provider_instance_id`
- `provider_key`
- `payment_mode`
- `merchant_app_id` = XunhuPay `appId`

This snapshot is used later by webhook verification, reconciliation, and refund flows so that changing a provider instance after checkout does not silently rebind old orders to a new merchant.

### 4. Strict response validation

XunhuPay create/query/refund code must be fail-closed:

- missing or malformed `errcode` is an error
- missing required fields for a claimed success response is an error
- unknown refund/query status values must not be treated as success
- notification payloads with invalid signature, mismatched `appid`, invalid amount, or mismatched order data are rejected and audited

The platform should prefer explicit failure over optimistic continuation when money state is ambiguous.

### 5. Refund parity

XunhuPay must expose the same backend operational capability shape as other providers once implemented:

- admin refund initiation
- standardized provider refund status mapping: `success`, `pending`, `failed`
- order audit logging on success/failure/pending transitions
- use the original order binding and merchant snapshot before issuing refund

Refund request strategy:

- prefer merchant `trade_order_id` (`out_trade_no` in platform terms) as the stable refund reference
- allow fallback to `open_order_id` only if needed by future real-world behavior, but do not broaden automatically in this change without evidence
- preserve returned `transaction_id`, `out_refund_no`, `refund_fee`, and `refund_status` in normalized handling as far as the current service contract allows

### 6. Capability gating

Provider capability exposure should match real implementation:

- XunhuPay supports create/query/webhook immediately
- refund capability is exposed only when refund implementation and tests are in place
- frontend provider settings should derive refund toggles from real provider support instead of assuming every provider can refund

### 7. Frontend behavior

Frontend keeps the existing EasyPay-like checkout UX:

- XunhuPay supports `qrcode` and `popup`
- explicit QR mode wins even on mobile
- hosted QR image URLs render directly as images instead of being re-encoded
- webhook helper/default notify URL still targets backend origin in dev environments

Additional admin behavior:

- refund toggles only appear when XunhuPay refund support is available in the platform capability model
- settings copy and provider cards continue to present XunhuPay as a first-class provider

## Implementation Plan

### Backend

1. Extend order snapshot creation for XunhuPay merchant identity.
2. Extend snapshot metadata validation to enforce XunhuPay `appid` matching.
3. Tighten XunhuPay `errcode` parsing and required-success-field validation.
4. Implement XunhuPay refund protocol support against `/payment/refund.html`.
5. Normalize refund statuses from XunhuPay (`CD`, `RD`, `UD`, etc.) into platform refund states.
6. Gate refund enablement through provider capability checks so unsupported providers cannot be configured as refundable.

### Frontend

1. Reflect provider refund capability in payment provider admin surfaces.
2. Keep existing XunhuPay checkout/mode UX intact.
3. Add or adjust tests for refund toggle visibility/serialization.

## Test Plan

### Backend unit tests

- snapshot includes XunhuPay merchant identity
- metadata validation rejects mismatched or missing XunhuPay `appid` where snapshot requires it
- create/query reject missing `errcode`
- create/query reject malformed success payloads
- refund request builds correct signed payload
- refund response maps `CD` => success, `RD` => pending, `UD` => failed
- refund config cannot be enabled unless provider refund support is real

### Frontend tests

- XunhuPay refund controls are hidden/disabled until capability is supported
- provider dialog serializes refund settings only when capability is available
- existing QR/popup tests continue to pass

### Regression validation

- `go test ./internal/payment/provider ./internal/service`
- targeted frontend vitest for payment provider config/dialog/settings/QR flow
- manual end-to-end verification in remote dev environment:
  - at least one XunhuPay WeChat payment
  - at least one XunhuPay Alipay payment if merchant account supports it
  - one missed-callback reconciliation case
  - one admin refund case

## Risks and Guardrails

- Never weaken signature verification to improve compatibility.
- Never infer merchant identity from current mutable config when an order snapshot exists.
- Do not mark ambiguous upstream responses as paid or refunded.
- Prefer audit logs and explicit operator-visible failures over silent fallback in money-moving paths.
