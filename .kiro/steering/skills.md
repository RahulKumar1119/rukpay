---
inclusion: always
---

# Required Skills

Both skills are already installed. Load the named skill before doing the work it covers. If a skill cannot be loaded, say so explicitly and do not substitute another skill.

## Go (`/backend`)
- Skill: `golang-patterns` (`affaan-m/ecc --skill golang-patterns`).
- Load it before writing, editing, or reviewing any Go code, including `*_test.go` files and `go.mod`.
- Do not mix in recommendations from other Go development skills. The repo-local `go-rapid-pbt` skill is allowed only for the property-test conventions it defines (rapid, 1000+ checks, trace comments).

## Frontend (`/frontend`)
- Skill: `web-design-guidelines`.
- Load it before writing, editing, or reviewing any React/TSX component, Tailwind styling, or UI copy layout, and when auditing UI for accessibility or UX.
- Its guidance applies alongside `anti-panic-ux.md`. Where they conflict, `anti-panic-ux.md` wins.
