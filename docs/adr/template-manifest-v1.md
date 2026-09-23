# ADR: Template Manifest v1

## Status
Accepted (2026-09-23)

## Context
Chat widget dan storefront membutuhkan kustomisasi visual aman tanpa arbitrary code upload.

## Decision
- Manifest JSON `schemaVersion: 1` dengan `kind`, `meta`, `tokens`
- Validator Go di `templates/manifest_validate.go`
- Built-in platform templates di-seed via `templates/seed.go`
- Install per tenant disimpan di `tenant_template_install` (system DB)

## Consequences
- Developer eksternal v1 hanya submit manifest + review queue
- Web frontend menerapkan token via `lib/template-engine` dengan sanitasi nilai
