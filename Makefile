.PHONY: lint test test-long

LONG_DURATION ?= 3h
LONG_COUNT ?= 100
GO_TEST_FLAGS ?= -v -race
PKGS ?= ./...

lint:
	go vet $(PKGS)
	golangci-lint run

test: lint
	go test $(GO_TEST_FLAGS) $(PKGS)

test-long: lint
	@deadline=$$(($$(date +%s) + $$(echo $(LONG_DURATION) | awk '/h$$/{print $$0+0"*3600"} /m$$/{print $$0+0"*60"} /s$$/{print $$0+0} !/[hms]$$/{print $$0}' | bc))); \
	iter=0; \
	while [ $$(date +%s) -lt $$deadline ]; do \
		iter=$$((iter+1)); \
		echo "===== iteration $$iter (deadline in $$((deadline - $$(date +%s)))s) ====="; \
		go test $(GO_TEST_FLAGS) -count=$(LONG_COUNT) $(PKGS) || exit 1; \
	done
