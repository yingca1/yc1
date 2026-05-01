---
name: ascii-arch
description: Convert system architecture descriptions into ASCII flow diagrams. Triggered by requests for architecture diagrams, flow charts, ASCII diagrams, system flows, state machines, or version comparison tables.
---

# ASCII Architecture Diagram Generator

## Symbol Reference

| Symbol | Meaning |
|--------|---------|
| `↓` | Synchronous call / flow direction (top → down) |
| `→` | Data flow / transformation (left → right) |
| `├─` | Branch / parallel path (non-terminal) |
| `└─` | Branch / parallel path (terminal / last item) |
| `│` | Continuation line |
| `✓` | Success path / check passed |
| `✗` | Failure path / check failed |
| `▼` | State transition |

## Layout Rules

1. **Service names** flush left, bold-style with description
2. **Operations** indented 6 spaces + `│` prefix for continuation
3. **Success/failure** marked with `✓` / `✗`
4. **Branches** use `├─` (non-last) and `└─` (last)
5. **Sync calls** connected by `↓` between service blocks
6. **Tables** for comparisons, summaries, and version diffs
7. **State machines** use `→` for transitions with trigger labels
8. **Max line width** ~80 chars; wrap long descriptions

## Workflow

When the user asks for an architecture diagram:

1. **Clarify scope**: Identify which pattern fits:
   - **Request Flow**: HTTP/RPC call chain across services
   - **Event Pipeline**: Async messaging (Kafka, queues, webhooks)
   - **State Machine**: Entity lifecycle with transitions
   - **Comparison Table**: Version diff, feature matrix

2. **Gather inputs**: Ask for (if not provided):
   - Services / components involved
   - Key operations at each step
   - Success and failure paths
   - Any version or comparison dimension

3. **Generate diagram** following the layout rules and symbol reference above

4. **Output** the diagram in a fenced code block (no language tag or use `text`)

## Inline Example: Request Flow

Input: "API Gateway verifies token, forwards to UserService which queries DB, returns user profile"

Output:

```
  Client
    POST /api/users/:id (Authorization: Bearer xxx)
      ↓ HTTPS
  API Gateway
      │   ✓ Verify JWT token
      │   ✓ Extract userId from path
      ↓ internal RPC
  UserService
      │   ✓ Validate userId format
      │   ✓ Query user by ID
      ↓ SQL
  PostgreSQL
      │   SELECT * FROM users WHERE id = :userId
      ↓ result
  UserService
      ├─ found    → 200 { id, name, email, avatar }
      └─ notFound → 404 { error: "User not found" }
```

## Reference Examples

For additional patterns (event pipelines, state machines, comparison tables), load:

```
@references/examples.md
```
