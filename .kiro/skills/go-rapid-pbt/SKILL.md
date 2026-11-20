---
name: go-rapid-pbt
description: Write Go property-based tests using pgregory.net/rapid with 1000+ checks and tier-boundary generators. Use when creating or editing *_test.go property tests.
---

# Go Property-Based Testing (`pgregory.net/rapid`) Rules

- Import `"pgregory.net/rapid"` and write standard `func TestPropName(t *testing.T)` wrappers calling `rapid.Check(t, func(t *rapid.T) { ... })`.
- Always include the exact trace comment above each test: `// Feature: <spec-name>, Property <N>: <Title>`
- Use `rapid.OneOf` to mix exact boundary constants (`1999, 2000, 9999, 10000, 24999, 25000` for INR; `23, 24, 71, 72` for hours; `0, 1, 2, 3` for retries) with `rapid.Int64Range` / `rapid.IntRange`.
- Never import `testing/quick`, SQLite, or logging packages.
- Run locally with: `go test -race -v ./... -rapid.checks=1000`
