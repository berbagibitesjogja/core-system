# Core System BBJ - Backend

## Backend Stack (Prerequisites)
1. Programming Language: Go 1.26.8 or more
2. Framework: net/http
3. Database: PostgreSQL 18
4. File Storage: RustFS 1.0.0
5. Database Migration: Goose v3.27.2
6. Container Runtime: Podman 5.8.4, Docker (not tested)

## To Start Dev
1. `cp .env.example .env`
2. `podman compose up -d`
3. `make air`
4. Develop!

## Makefile
1. `make migrate-up` - Run migration up
2. `make migrate-down` - Run migration down
3. `make migrate-reset` - Run migration reset
4. `make migrate-status` - Show migration status
5. `make functions-up` - Run function up
6. `make functions-down` - Run function down
7. `make functions-reset` - Run function reset
8. `make functions-status` - Show function status
9. `make seeds-up` - Run seeds up
10. `make seeds-down` - Run seeds down
11. `make seeds-reset` - Run seeds reset
12. `make seeds-status` - Show seeds status
13. `make db-up` - Run database up
14. `make db-down` - Run database down
15. `make db-reset` - Run database reset
16. `make db-status` - Show database status
17. `make nuke-prod` - Nuke production database (use with caution)

### 2026 BERBAGI BITES JOGJA (BBJ).
