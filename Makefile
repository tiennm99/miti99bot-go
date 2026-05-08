.PHONY: test test-emulator firestore-emulator vet build run

# Default: run unit tests that don't require the Firestore emulator.
test:
	go test -race -count=1 ./...

# Start a local Firestore emulator (separate terminal). Requires gcloud SDK
# with the cloud-firestore-emulator component installed:
#   gcloud components install cloud-firestore-emulator
firestore-emulator:
	gcloud emulators firestore start --host-port=localhost:8085

# Run all tests including emulator-gated ones. Expects the emulator to be
# already running (use `make firestore-emulator` in another shell).
test-emulator:
	FIRESTORE_EMULATOR_HOST=localhost:8085 \
	GOOGLE_CLOUD_PROJECT=miti99bot-go-test \
	go test -race -count=1 ./...

vet:
	go vet ./...

build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o ./bin/server ./cmd/server

# Local dev run with an in-memory KV (no Firestore needed).
run:
	go run ./cmd/server
