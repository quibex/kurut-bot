SERVICE_NAME := kurut-bot
SERVICE_PORT ?= 8080

export GOBIN := $(PWD)/bin
export PATH := $(GOBIN):$(PATH)

PROTOC_VERSION := 26.1
GOOSE_VERSION := 3.24.3
GRPCUI_VERSION := 1.4.1
OGEN_VERSION ?= v1.14.0
GOLANG_CI_LINT_VERSION ?= v1.64.8
MOCKGEN_VERSION ?= v1.6.0
GOWRAP_VERSION ?= v1.4.0

UNAME_S := $(shell uname -s)
UNAME_P := $(shell uname -p)

ifeq ($(UNAME_S),Linux)
	OSFLAG = linux
endif

ifeq ($(UNAME_S),Darwin)
	OSFLAG = osx
  ifeq ($(UNAME_P),arm)
    # protobuf team doesn't create releases
    # for Apple M1 (arm) chipset
	  UNAME_P = x86_64
  endif
  ifeq ($(UNAME_P),i386)
    # for Rosetta 2 emulator
	  UNAME_P = x86_64
  endif
endif

HOST_ARCH := "$(OSFLAG)-$(UNAME_P)"
PROTOC_ZIP := protoc-$(PROTOC_VERSION)-$(HOST_ARCH).zip

.PHONY: all
all: test lint build

.PHONY: build
build:
	go build -o bin/kurut-bot cmd/bot/main.go

.PHONY: run
run: build
	./bin/kurut-bot

.PHONY: clean
clean:
	rm -rf bin/

.PHONY: lint
lint: ./bin/golangci-lint
	./bin/golangci-lint run --timeout 5m -v ./...

.PHONY: fix-lint
fix-lint: ./bin/golangci-lint
	./bin/golangci-lint run --fix --timeout=5m ./...

.PHONY: test
test:
	GOGC=off go test -short ./...

.PHONY: integration-test
test-all:
	go test -tags=all,dbtest ./...

.PHONY: cover
test-cover:
	GOGC=off go test -tags=all -coverprofile=cover.tmp.out ./...
	grep -v -E "stubs_test.go|stubs.go|mocks.go|generated.go|pb.go|gen.go|main.go" cover.tmp.out > coverage.out
	rm cover.tmp.out
	go tool cover -html=coverage.out

.PHONY: vendor
vendor:
	go mod vendor

./bin:
	mkdir -p ./bin

.PHONY: ./bin/golangci-lint
./bin/golangci-lint: | ./bin
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANG_CI_LINT_VERSION)

.PHONY: ./bin/ogen
./bin/ogen: | ./bin
	go install github.com/ogen-go/ogen/cmd/ogen@$(OGEN_VERSION)

.PHONY: ./bin/mockgen
./bin/mockgen: | ./bin
	go install github.com/golang/mock/mockgen@$(MOCKGEN_VERSION)

.PHONY: ./bin/gowrap
./bin/gowrap: | ./bin
	go install github.com/hexdigest/gowrap/cmd/gowrap@latest

.PHONY: install
install: | ./bin ./bin/golangci-lint ./bin/ogen ./bin/mockgen ./bin/gowrap

.PHONY: docker
docker-build: vendor
	docker build -t $(SERVICE_NAME) .

docker-run:
	docker run -p $(SERVICE_PORT):8080 $(SERVICE_NAME)

.PHONY:
docker-up:
	@docker-compose up -d --build

.PHONY:
docker-down:
	@docker-compose down -v

.PHONY:  gen
gen: install
	go generate ./...

.PHONY: pre
pre:
	chmod -R +x .pre-commit-checks
	pre-commit run --all-files

./bin/protoc.zip: | ./bin
	curl -L https://github.com/google/protobuf/releases/download/v$(PROTOC_VERSION)/$(PROTOC_ZIP) -o ./bin/protoc.zip

./bin/protoc: ./bin/protoc.zip
	unzip -o ./bin/protoc.zip -d ./ bin/protoc

./proto/include: ./bin/protoc.zip
	unzip -o ./bin/protoc.zip -d ./api/proto include/*

./bin/protoc-gen-go: | ./bin
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest

./bin/gomock: | ./bin
	go install go.uber.org/mock/mockgen@latest

./bin/goose: | ./bin
	go install github.com/pressly/goose/v3/cmd/goose@v$(GOOSE_VERSION)

PROTOS = $(wildcard ./api/proto/*.proto ./api/proto/events/*.proto, ./api/proto/commands/*.proto ./api/proto/types/*.proto)

.PHONY: ./pkg
./pkg: PROTOC_OPT ?= module=gitlab.services.mts.ru/media/projects/puma/$(PROJECT_NAME)/pkg:./pkg/api
./pkg: ./bin/protoc ./proto/include ./bin/protoc-gen-go $(PROTOS)
	mkdir -p $@
	protoc \
	-I ./api/proto \
    -I ./api/proto/include \
    --go_out=$(PROTOC_OPT) \
    --descriptor_set_out=./api/proto/$(SERVICE_NAME).protoset  \
    --include_imports \
    ./api/proto/replicationproto/*.proto

# example usage:  make create-migration name=create_subtasks_tables
.PHONY: create migration
create-migration: | ./migrations ./bin/goose
	goose -dir migrations create $(name) sql

DATABASE_PATH := ./data/kurut.db

.PHONY: migrate-up
migrate-up: | ./bin/goose
	goose -dir migrations sqlite3 $(DATABASE_PATH) up

.PHONY: migrate-down
migrate-down: | ./bin/goose
	goose -dir migrations sqlite3 $(DATABASE_PATH) down

.PHONY: migrate-status
migrate-status: | ./bin/goose
	goose -dir migrations sqlite3 $(DATABASE_PATH) status