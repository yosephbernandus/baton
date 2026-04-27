# Project Brief
# This file is prepended to every worker prompt by baton.
# Write once (by Opus during project setup), update occasionally.
# Covers universal project conventions that apply to every task.
# Place at: .baton/project-brief.md

Project: MiiTel Session Service
Language: Go 1.22
Framework: stdlib + sqlx (no ORM)
Database: PostgreSQL 15

## Conventions
- ID strategy: ULID via pkg/ulid (never UUID)
- Error handling: always return errors, never panic
- Naming: snake_case for DB columns, camelCase for Go structs/vars
- File naming: lowercase with underscores (user_session.go)
- Migrations: sequential numbering (001_, 002_, 003_), one file per migration

## Dependencies
- Test framework: testing + testify/assert
- HTTP routing: stdlib net/http with chi router
- Logging: slog with structured fields (no fmt.Println)
- Config: environment variables via envconfig

## Patterns
- All handlers return (http.ResponseWriter, *http.Request)
- All DB queries use sqlx named queries
- All errors wrapped with fmt.Errorf("context: %w", err)
- All new tables need a corresponding model struct in models/
