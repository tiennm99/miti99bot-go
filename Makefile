.PHONY: help test test-emulator test-dynamodb firestore-emulator dynamodb-local dynamodb-local-stop vet build build-lambda run sam-validate sam-build sam-deploy logs clean

# Lambda target architecture. Match Globals.Architectures in template.yaml.
LAMBDA_GOOS   ?= linux
LAMBDA_GOARCH ?= arm64
LAMBDA_OUT    := build/lambda/bootstrap

help: ## Show this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS=":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

# ---- Test ------------------------------------------------------------------

# Default: run unit tests that don't require any emulator.
test: ## Unit tests (no emulator required)
	go test -race -count=1 ./...

# Start a local Firestore emulator (separate terminal). Requires gcloud SDK
# with the cloud-firestore-emulator component installed:
#   gcloud components install cloud-firestore-emulator
firestore-emulator: ## Start Firestore emulator on :8085 (foreground)
	gcloud emulators firestore start --host-port=localhost:8085

# Run all tests including Firestore-emulator-gated ones. Expects the emulator
# to already be running (use `make firestore-emulator` in another shell).
test-emulator: ## Run tests with Firestore emulator (must be running)
	FIRESTORE_EMULATOR_HOST=localhost:8085 \
	GOOGLE_CLOUD_PROJECT=miti99bot-go-test \
	go test -race -count=1 ./...

# Run DynamoDB integration tests against DynamoDB Local.
# Override DDB_PORT if 8001 is taken on your host.
DDB_PORT ?= 8001
test-dynamodb: dynamodb-local ## Run DynamoDB tests against DynamoDB Local
	DYNAMODB_LOCAL_URL=http://localhost:$(DDB_PORT) LOG_LEVEL=error \
		go test -race -count=1 ./internal/storage/...

# ---- Lint / Vet -----------------------------------------------------------

vet: ## go vet
	go vet ./...

# ---- Build ----------------------------------------------------------------

build: ## Build the local server binary (host arch)
	CGO_ENABLED=0 go build -ldflags="-s -w" -o ./bin/server ./cmd/server

build-lambda: ## Cross-compile bootstrap for Lambda (linux/arm64)
	@mkdir -p $(dir $(LAMBDA_OUT))
	CGO_ENABLED=0 GOOS=$(LAMBDA_GOOS) GOARCH=$(LAMBDA_GOARCH) \
		go build -tags lambda.norpc -ldflags="-s -w" \
		-o $(LAMBDA_OUT) ./cmd/server
	@chmod +x $(LAMBDA_OUT)
	@ls -lh $(LAMBDA_OUT) | awk '{print "lambda binary:", $$5}'

# ---- Run ------------------------------------------------------------------

# Local dev run with an in-memory KV (no Firestore / DynamoDB needed).
run: ## Run locally (in-memory KV)
	go run ./cmd/server

# ---- DynamoDB Local for tests ---------------------------------------------

dynamodb-local: ## Start DynamoDB Local container on :$(DDB_PORT) (idempotent)
	@if ! docker ps --format '{{.Names}}' | grep -q '^miti99bot-ddb$$'; then \
		docker run -d --rm --name miti99bot-ddb -p $(DDB_PORT):8000 \
			amazon/dynamodb-local -jar DynamoDBLocal.jar -inMemory -sharedDb; \
		echo "DynamoDB Local started on :$(DDB_PORT)"; \
		sleep 1; \
	else \
		echo "DynamoDB Local already running"; \
	fi

dynamodb-local-stop: ## Stop DynamoDB Local
	-docker stop miti99bot-ddb

# ---- SAM (require AWS CLI + SAM CLI installed locally) -------------------

sam-validate: ## Validate template.yaml without contacting AWS
	sam validate --lint

sam-build: build-lambda ## sam build (after make build-lambda)
	sam build

sam-deploy: sam-build ## Deploy via SAM (uses samconfig.toml). Set ALERT_EMAIL=… optionally.
	@if [ -n "$$ALERT_EMAIL" ]; then \
		sam deploy --no-confirm-changeset --no-fail-on-empty-changeset \
			--parameter-overrides "AlertEmail=$$ALERT_EMAIL"; \
	else \
		sam deploy --no-confirm-changeset --no-fail-on-empty-changeset; \
	fi

logs: ## Tail Lambda logs (last 5m). Override with SINCE=10m.
	@sam logs --tail --stack-name miti99bot-aws-port --start-time $${SINCE:-5m}ago

# ---- Clean ----------------------------------------------------------------

clean: ## Remove local build artifacts
	rm -rf build/ bin/ .aws-sam/ cov.out
