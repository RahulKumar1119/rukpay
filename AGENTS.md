# Go Development Skill Policy

## Mandatory Skill

- Use `golang-patterns` from `affaan-m/ecc` as the primary Go development skill.
- Activate `/golang-patterns` for all Go-related development tasks.
- Read and follow its complete `SKILL.md` instructions before implementing Go changes.
- Do not substitute other Go development skills.
- Do not mix conflicting recommendations from other Go skills.

## Go Version Requirements

- Target Go 1.21.1.
- Do not introduce Go 1.22+ language features or APIs (for example `ServeMux` method or `{id}` patterns, `Request.PathValue`, range-over-int).
- Maintain compatibility with existing dependencies and project architecture.

## Implementation Requirements

- Follow idiomatic Go patterns.
- Use proper error handling and wrapping.
- Follow clean package and interface design.
- Handle concurrency safely.
- Write maintainable, testable code.
- Run gofmt, go vet and go test ./... before completing changes.

## Execution Rule

If golang-patterns is unavailable, explicitly report that it could not be loaded. Do not silently substitute another skill.

# Frontend Design Skill Policy

## Mandatory Skill

- Use `web-design-guidelines` as the UI review and design skill for all work under `frontend/`.
- Load it before creating, editing, or reviewing React/TSX components, Tailwind styles, and UI layout.
- Apply it together with `.kiro/steering/anti-panic-ux.md`; if they conflict, the steering document wins.
- If `web-design-guidelines` is unavailable, explicitly report that it could not be loaded. Do not silently substitute another skill.
