# WABantu API (Encore.go)

Full rewrite of the NestJS API using [Encore.go](https://encore.dev) framework.
The original NestJS code is preserved in `../api/` for reference.

## Prerequisites

- **Go 1.24+**: `brew install go`
- **Encore CLI**: `brew install encoredev/tap/encore`
- **Docker**: running (Encore provisions local Postgres + Redis)

## Quick start

```bash
cd api-go
encore run
```

Encore starts the app, provisions databases, and opens the local dev dashboard
with tracing, API explorer, and architecture diagrams.

## Project structure

```
api-go/
  encore.app            # Encore app config
  shared/               # Non-service packages (crypto, types, errors)
  auth/                 # Authentication (register, login, JWT, sessions)
  tenant/               # Multi-tenant infra (system DB, schema provisioning)
  business/             # Business profile CRUD, website import
  whatsapp/             # Meta Cloud API provider
  webhook/              # WhatsApp webhook ingest
  inbox/                # Conversations, messages, contacts
  ai/                   # AI auto-reply pipeline, safety, prompts, memory
  kb/                   # Knowledge base FAQ CRUD
  billing/              # Subscription plans, invoices
  payment/              # Midtrans QRIS integration
  order/                # Lightweight conversational commerce
  shipping/             # RajaOngkir shipping rates
  analytics/            # Dashboard statistics
  usage/                # Usage metering, quotas, cost protection
  audit/                # Audit logging
  admin/                # Super admin, impersonation
  flag/                 # Feature flags
  importcsv/            # CSV/XLSX file import
```

## Multi-tenant architecture

Each tenant has its own Postgres **schema**. The `tenant` service manages schema
provisioning. Other services use `sqldb.Named("tenantdata")` with
`SET search_path TO "tenant_schema"` per request.

System tables (accounts, tenants, flags, audit) live in the `tenant` service DB.

## Key dependencies

| Package | Purpose |
|---------|---------|
| `encore.dev` | Framework (API, DB, Pub/Sub, Cron, Auth) |
| `anthropic-sdk-go` | Claude AI for auto-reply |
| `midtrans-go` | QRIS payment |
| `go-redis/v9` | Redis sessions + AI cache |
| `golang-jwt/jwt/v5` | JWT tokens |
| `excelize/v2` | XLSX import |
| `sentry-go` | Error tracking |

## Environment / Secrets

Encore secrets (set via `encore secret set`):

- `JwtAccessSecret`, `JwtRefreshSecret`
- `DataEncryptionKey`
- `RedisURL`
- `AnthropicAPIKey`
- `AIInternalToken`
- `MidtransServerKey`, `MidtransClientKey`, `MidtransIsProduction`
- `RajaOngkirAPIKey`, `RajaOngkirAccountType`
- `SentryDSN`

## Building for production

```bash
encore build docker wabantu
```

This produces a single optimised Docker image.
