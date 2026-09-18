# Build des project-router: erst die WebUI, dann das Binary mit eingebetteter WebUI.

BINARY ?= router
GOFLAGS ?=

.PHONY: all build web deps test vet clean run

all: build

## build: WebUI bauen und das Binary erzeugen
build: web
	go build $(GOFLAGS) -o $(BINARY) ./cmd/router

## web: React-Oberfläche nach web/dist bauen
web: web/node_modules
	cd web && npm run build

web/node_modules: web/package.json web/package-lock.json
	cd web && npm ci
	@touch web/node_modules

## test: kompletter Testlauf
test:
	go test ./...

## vet: statische Prüfung
vet:
	go vet ./...

## run: lokal starten (Roots über ROUTER_ROOTS oder -config setzen)
run: build
	./$(BINARY)

clean:
	rm -f $(BINARY)
	rm -rf web/dist/assets
