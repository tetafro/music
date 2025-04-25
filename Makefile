.PHONY: dep
dep:
	@ go mod tidy && go mod verify

.PHONY: lint
lint:
	@ golangci-lint run --fix

.PHONY: build
build:
	@ go build -o ./bin/music

.PHONY: run
run:
	./bin/music -config config.yaml
