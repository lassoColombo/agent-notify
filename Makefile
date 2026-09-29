# Two commands set a machine up: `make install` builds every module into
# $(GOBIN), then `agent-notify install` sets up every integration it finds on
# PATH (D-85).

MODULES := agent-notify $(wildcard agent-integrations/*) $(wildcard tool-integrations/*)

# GOROOT exported by an outer shell overrides the toolchain .tool-versions
# pins; unset it so that the pin decides.
GO := env -u GOROOT go

# Where the binaries go, and it has to be a directory on your PATH: finding
# `agent-notify-<name>` there by name is the whole of what core knows about an
# integration. `go install` reads GOBIN, so setting it here is enough:
#
#   make install GOBIN=/opt/homebrew/bin
#
# Left alone it is whatever `go env GOBIN` says, and Go's own default when that
# is empty, which is $(go env GOPATH)/bin.
GOBIN ?= $(shell $(GO) env GOBIN)
ifeq ($(strip $(GOBIN)),)
GOBIN := $(shell $(GO) env GOPATH)/bin
endif
export GOBIN

.PHONY: install build test vet

install:
	@for module in $(MODULES); do echo "== $$module"; (cd $$module && $(GO) install .) || exit 1; done
	@echo
	@echo "installed into $(GOBIN)"
	@case ":$$PATH:" in \
	  *":$(GOBIN):"*) echo "next: agent-notify install" ;; \
	  *) echo "$(GOBIN) is NOT on your PATH, and nothing will find these by name."; \
	     echo "Put it there, or build again with: make install GOBIN=<a directory on your PATH>" ;; \
	esac

# Workspace mode does not make `./...` span modules, so each verb walks them.
build test vet:
	@for module in $(MODULES); do echo "== $$module"; (cd $$module && $(GO) $@ ./...) || exit 1; done
