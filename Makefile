.PHONY: dep
dep:
	@ go mod tidy && go mod verify

.PHONY: lint
lint:
	@ golangci-lint run --fix

.PHONY: build
build:
	@ go build -o ./bin/yamusic

.PHONY: run
run:
	./bin/yamusic -config config.yaml
