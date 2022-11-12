ROOT_DIR:=$(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))


.PHONY: help gomod-tidy install

default: help 

#GO BUILD SDK
gomod-tidy:
	@go mod tidy

build:
	@$(eval VERSION=$(shell git describe --tags --dirty --always))
	CGO_ENABLED=1 go build -x -v -tags bn256 -ldflags "-X github.com/0chain/s3migration/cmd.VersionStr=$(VERSION)" -o s3mgrt main.go


help:
	@echo "Environment: "
	@echo "\tGOPATH=$(GOPATH)"
	@echo "\tGOROOT=$(GOROOT)"
	@echo ""
	@echo "Supported commands:"
	@echo "\tmake help              - display environment and make targets"
	@echo "\tmake gomod-tidy        - download the go modules"
	@echo "\tmake install           - build s3mgrt cli"
