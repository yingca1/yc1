# ASCII Architecture Examples

## 1. HTTP Request Flow

**Input**: User login — client sends credentials, API gateway rate-limits and forwards to AuthService, which verifies password against DB, issues JWT, and logs the event.

**Output**:

```
  Client
    POST /auth/login { email, password }
      ↓ HTTPS
  API Gateway (nginx/APISIX)
      │   ✓ Rate limit check (10 req/min per IP)
      │   ✓ Strip internal headers
      ↓ proxy_pass
  AuthService
      │   ✓ Validate email format
      │   ✓ Find user by email
      ↓ SQL
  PostgreSQL
      │   SELECT id, password_hash FROM users WHERE email = :email
      ↓ result
  AuthService
      │   ✓ bcrypt.compare(password, password_hash)
      │   ✗ mismatch → 401 { error: "Invalid credentials" }
      │   ✓ Generate JWT { sub: userId, exp: 1h }
      │   ✓ Set refresh token in HttpOnly cookie
      ↓ async (fire-and-forget)
  AuditLog
      │   INSERT INTO audit_logs (user_id, action, ip, timestamp)
      ↓
  Client ← 200 { access_token, expires_in: 3600 }
```

---

## 2. Event-Driven Pipeline (Kafka)

**Input**: Order placed — OrderService publishes event, PaymentService processes payment, NotificationService sends confirmation email, AnalyticsService updates dashboard.

**Output**:

```
  OrderService
      │   ✓ Validate order items & stock
      │   ✓ Calculate total price
      │   ✓ Create order (status: PENDING)
      │   ✓ Publish event
      ↓ Kafka Producer
  Kafka (topic: order.placed, 6 partitions)
      │   key: orderId, value: { orderId, userId, items, total }
      ├─ partition 0-2 ──────────────────────────────┐
      └─ partition 3-5 ──────────────┐               │
      ↓ Consumer Group: payment      ↓               ↓
  PaymentService                NotificationSvc   AnalyticsSvc
      │   ✓ Idempotency check        │               │
      │   ✓ Charge payment method     │ Send email    │ Update
      │   ✗ failed → publish          │ confirmation  │ dashboard
      │     order.payment.failed      │               │ metrics
      │   ✓ success → publish         │               │
      │     order.payment.completed   │               │
      ↓ Kafka Producer               ↓               ↓
  Kafka (topic: order.payment.completed)
      ↓ Consumer Group: fulfillment
  FulfillmentService
      │   ✓ Reserve inventory
      │   ✓ Create shipping label
      │   ✓ Update order (status: SHIPPED)
      └─ Publish order.shipped
```

---

## 3. State Machine / Lifecycle

**Input**: Order status lifecycle — from creation through payment, fulfillment, delivery, with cancellation and refund paths.

**Output**:

```
  Order Lifecycle

  user places order       payment received        warehouse ships
       │                       │                       │
       ▼                       ▼                       ▼
    CREATED ──────────► PAYMENT_PENDING ──────► PROCESSING ──────► SHIPPED
       │                       │                    │                  │
       │ user cancels          │ payment fails      │ out of stock     │ delivered
       │                       │                    │                  │
       ▼                       ▼                    ▼                  ▼
    CANCELLED           PAYMENT_FAILED         BACKORDERED        DELIVERED
       │                       │                    │                  │
       │                       │ retry succeeds     │ restocked        │ user requests
       │                       └──► PAYMENT_PENDING │                  │ return
       │                                            └──► PROCESSING   │
       │                                                               ▼
       │                                                           RETURN_PENDING
       │                                                               │
       │                                                               │ inspected
       │                                                               ▼
       └───────────────────────────────────────────────────────► REFUNDED
                                                                (terminal)

  Allowed Transitions:
  ┌──────────────────┬──────────────────────┬────────────────────┐
  │ From             │ To                   │ Trigger            │
  ├──────────────────┼──────────────────────┼────────────────────┤
  │ CREATED          │ PAYMENT_PENDING      │ checkout initiated │
  │ CREATED          │ CANCELLED            │ user cancels       │
  │ PAYMENT_PENDING  │ PROCESSING           │ payment success    │
  │ PAYMENT_PENDING  │ PAYMENT_FAILED       │ payment fails      │
  │ PAYMENT_FAILED   │ PAYMENT_PENDING      │ retry payment      │
  │ PROCESSING       │ SHIPPED              │ warehouse ships    │
  │ PROCESSING       │ BACKORDERED          │ out of stock       │
  │ BACKORDERED      │ PROCESSING           │ restocked          │
  │ SHIPPED          │ DELIVERED            │ carrier confirms   │
  │ DELIVERED        │ RETURN_PENDING       │ return requested   │
  │ RETURN_PENDING   │ REFUNDED             │ return inspected   │
  │ CANCELLED        │ REFUNDED             │ if payment taken   │
  └──────────────────┴──────────────────────┴────────────────────┘
```

---

## 4. Version Comparison Table

**Input**: Compare v2 vs v3 of the LLM completion pipeline — changes in creation timing, credit handling, error handling, and model rewriting.

**Output**:

```
  LLM Completion Pipeline: v2 → v3

  | Change                  | v2                              | v3                                         |
  |-------------------------|---------------------------------|--------------------------------------------|
  | Completion creation     | Kafka async (started msg)       | Gateway pre-create (after reserve, pre-proxy) |
  | Kafka createLLMCompletion | Direct create                 | Upsert: skip if exists, update providerResponseId |
  | Status enum             | STARTED, COMPLETED              | STARTED, COMPLETED, **FAILED**             |
  | Proxy failure handling  | releaseCredit only, no terminal state | releaseCredit + failCompletion (FAILED + VOID) |
  | Credit reserve amount   | Fixed $0.001 (Kafka phase)      | Estimated from llm_model_prices (Gateway)  |
  | Response model field    | Passthrough `openrouter/google/...` | Rewrite to `google/...`                  |
  | x-sunra-token decode    | Extract orgId (sub) only        | Extract {sub, client_key_id}               |
  | providerResponseId      | Required at create              | Optional at create (''), filled via Kafka upsert |

  Key Improvement:
  ┌─────────────────────────────────────────────────────────────────┐
  │ v2: proxy fails → credit released, but Completion stuck as     │
  │     STARTED forever (no terminal state, ghost records)         │
  │                                                                │
  │ v3: proxy fails → credit released + Completion marked FAILED   │
  │     with invoice VOID (clean terminal state, auditable)        │
  └─────────────────────────────────────────────────────────────────┘
```
