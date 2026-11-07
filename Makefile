API_DIR := apps/api
WEB_DIR := apps/web
BIN := bin/api

.PHONY: setup dev dev-api dev-web build build-web build-api build-linux-arm64 run test vet sqlc clean smoke

setup: ## Download Go modules and install npm deps
	cd $(API_DIR) && go mod download
	cd $(WEB_DIR) && npm install

dev-api: ## Run API with hot reload via go run (http://localhost:3000)
	cd $(API_DIR) && go run ./cmd/api

dev-web: ## Run Vite dev server with /api proxy (http://localhost:5173)
	cd $(WEB_DIR) && npm run dev

dev: ## Run API + web together (Ctrl-C stops both)
	@trap 'kill 0' INT TERM; \
	(cd $(API_DIR) && go run ./cmd/api) & \
	(cd $(WEB_DIR) && npm run dev) & \
	wait

build-web: ## Build SPA and copy bundle into the API embed dir
	cd $(WEB_DIR) && npm run build
	rm -rf $(API_DIR)/web/dist/assets $(API_DIR)/web/dist/index.html
	cp $(WEB_DIR)/dist/index.html $(API_DIR)/web/dist/index.html
	cp -r $(WEB_DIR)/dist/assets $(API_DIR)/web/dist/assets

build-api: ## Build single binary (serves API + embedded SPA)
	cd $(API_DIR) && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o ../../$(BIN) ./cmd/api

build-linux-arm64: build-web ## Cross build for linux/arm64 -> ./bin/api-linux-arm64
	cd $(API_DIR) && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o ../../bin/api-linux-arm64 ./cmd/api

build: build-web build-api ## Full production build -> ./bin/api

run: build ## Build and run the single binary
	./$(BIN)

test: ## Run backend tests
	cd $(API_DIR) && go test ./...

vet: ## Format check + go vet + frontend typecheck
	test -z "$$(cd $(API_DIR) && gofmt -l .)"
	cd $(API_DIR) && go vet ./...
	cd $(WEB_DIR) && npm run typecheck

sqlc: ## Regenerate sqlc queries (needs sqlc binary)
	cd $(API_DIR) && sqlc generate

clean: ## Remove build outputs and local sqlite data
	rm -rf bin $(API_DIR)/data $(API_DIR)/web/dist/assets $(API_DIR)/web/dist/index.html

smoke: ## Probe a running server on :3000 (CSRF cookie + CSP header)
	@echo "== GET /api/auth/csrf =="
	curl -s -c /tmp/smoke.jar -D - -o /dev/null http://localhost:3000/api/auth/csrf | grep -iE '^HTTP|content-security-policy:|set-cookie:' | head -5
	@echo "== POST /api/auth/login without CSRF (expect 403) =="
	curl -s -o /dev/null -w '%{http_code}\n' -X POST http://localhost:3000/api/auth/login -H 'Content-Type: application/json' -d '{"username":"admin","password":"admin123"}'
