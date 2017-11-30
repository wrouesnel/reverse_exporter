
# COVERDIR is just a temporary working directory for coverage files
COVERDIR = $(shell pwd)/.coverage
# TOOLDIR is the path to where our vendored build tooling lives
TOOLDIR = $(shell pwd)/tools
# CMD_DIR is the presumed location of golang binaries to build i.e. cmd/myprogram
CMD_DIR := cmd

# GO_SRC is used to track source code changes for builds
GO_SRC := $(shell find . -name '*.go' ! -path '*/vendor/*' ! -path 'tools/*' )
# GO_DIRS is used to pass package lists to gometalinter
GO_DIRS := $(shell find . -path './vendor/*' -o -path './tools/*' -o -name '*.go' -printf "%h\n" | uniq | tr -s '\n' ' ')
# GO_PKGS is used to run tests.
GO_PKGS := $(shell go list ./... | grep -v '/vendor/')
# GO_CMDS is used to build command binaries (by convention assume to be anything under cmd/)
GO_CMDS := $(shell find $(CMD_DIR) -mindepth 1 -type d -printf "%f ")

# VERSION is calculated from git tags and inserted into built binaries.
VERSION ?= $(shell git describe --dirty)

# When using CI systems, you want to override these - especially in container
# infrastructures.
CONCURRENT_LINTERS ?= $(shell cat /proc/cpuinfo | grep processor | wc -l)
LINTER_DEADLINE ?= 30s

export PATH := $(TOOLDIR)/bin:$(PATH)
SHELL := env PATH=$(PATH) /bin/bash

all: style lint test

style: tools
	gometalinter --disable-all --enable=gofmt $(GO_DIRS)

lint: tools
	@echo Using $(CONCURRENT_LINTERS) processes
	gometalinter -j $(CONCURRENT_LINTERS) --deadline=$(LINTER_DEADLINE) --disable=gotype $(GO_DIRS)

fmt: tools
	gofmt -s -w $(GO_SRC)

test: tools
	@mkdir -p $(COVERDIR)
	@rm -f $(COVERDIR)/*
	for pkg in $(GO_PKGS) ; do \
		go test -v -covermode count -coverprofile=$(COVERDIR)/$$(echo $$pkg | tr '/' '-').out $$pkg || exit 1 ; \
	done
	gocovmerge $(shell find $(COVERDIR) -name '*.out') > cover.out

tools:
	$(MAKE) -C $(TOOLDIR)

autogen:
	@echo "Installing git hooks in local repository..."
	ln -sf $(TOOLDIR)/pre-commit .git/hooks/pre-commit


.PHONY: tools autogen style fmt test binary all
