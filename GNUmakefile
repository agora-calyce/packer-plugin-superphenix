NAME=superphenix
BINARY=packer-plugin-${NAME}

.PHONY: build test generate check-generate plugin-check

build:
	go build -o ${BINARY} .

test:
	go test -race -count=1 ./...

# Generates *.hcl2spec.go, docs-partials/ then .web-docs/ (to be committed).
generate:
	go generate ./...
	@if [ -d ".docs" ]; then rm -r ".docs"; fi
	packer-sdc renderdocs -src "docs" -partials docs-partials/ -dst ".docs/"
	@./.web-docs/scripts/compile-to-webdocs.sh "." ".docs" ".web-docs" "agora-calyce"
	@rm -r ".docs"

plugin-check: build
	packer-sdc plugin-check ${BINARY}
