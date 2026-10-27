ENV_FILE ?= .env

ifneq (,$(wildcard $(ENV_FILE)))
    include $(ENV_FILE)
    export
endif

DB_STRING = postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_NAME)?sslmode=disable

MIGRATIONS_DIR ?= database/migrations

FUNCTIONS_DIR ?= database/functions
FUNCTIONS_TABLE ?= goose_db_functions

SEEDS_DIR ?= database/seeds
SEEDS_TABLE ?= goose_db_seeds

.PHONY: air \
    migrate-up migrate-down migrate-reset migrate-status \
    functions-up functions-down functions-reset functions-status \
    seeds-up seeds-down seeds-reset seeds-status \
    db-reset db-up db-clean prod-nuke

air:
	air | jq --unbuffered -R 'fromjson? // .'

migrate-up:
	goose -dir $(MIGRATIONS_DIR) postgres "$(DB_STRING)" up

migrate-down:
	goose -dir $(MIGRATIONS_DIR) postgres "$(DB_STRING)" down

migrate-reset:
	goose -dir $(MIGRATIONS_DIR) postgres "$(DB_STRING)" reset

migrate-status:
	goose -dir $(MIGRATIONS_DIR) postgres "$(DB_STRING)" status

functions-up:
	goose -dir $(FUNCTIONS_DIR) -table $(FUNCTIONS_TABLE) postgres "$(DB_STRING)" up

functions-down:
	goose -dir $(FUNCTIONS_DIR) -table $(FUNCTIONS_TABLE) postgres "$(DB_STRING)" down

functions-reset:
	goose -dir $(FUNCTIONS_DIR) -table $(FUNCTIONS_TABLE) postgres "$(DB_STRING)" reset

functions-status:
	goose -dir $(FUNCTIONS_DIR) -table $(FUNCTIONS_TABLE) postgres "$(DB_STRING)" status

seeds-up:
	goose -dir $(SEEDS_DIR) -table $(SEEDS_TABLE) postgres "$(DB_STRING)" up

seeds-down:
	goose -dir $(SEEDS_DIR) -table $(SEEDS_TABLE) postgres "$(DB_STRING)" down

seeds-reset:
	goose -dir $(SEEDS_DIR) -table $(SEEDS_TABLE) postgres "$(DB_STRING)" reset

seeds-status:
	goose -dir $(SEEDS_DIR) -table $(SEEDS_TABLE) postgres "$(DB_STRING)" status

db-reset: seeds-reset functions-reset migrate-reset

db-up: migrate-up functions-up seeds-up

db-clean: db-reset db-up

prod-nuke:
	$(MAKE) db-clean ENV_FILE=.env.prod
