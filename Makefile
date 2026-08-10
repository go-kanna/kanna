BUILD_DIR = bin

# Every generator has a command under cmd/kanna-<name> and one example module
# under examples/<name>, which the `build` and `examples` targets walk.
GENERATORS = di fixture mapper

.PHONY: test
test:
	go test ./... -race

.PHONY: lint
lint:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint is not installed"; \
		exit 1; \
	}
	golangci-lint run

.PHONY: lint-fix
lint-fix:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint is not installed"; \
		exit 1; \
	}
	golangci-lint run --fix

.PHONY: build
build:
	mkdir -p $(BUILD_DIR)
	@for g in $(GENERATORS); do \
		echo "go build -o $(BUILD_DIR)/kanna-$$g ./cmd/kanna-$$g"; \
		go build -o $(BUILD_DIR)/kanna-$$g ./cmd/kanna-$$g || exit 1; \
	done

.PHONY: clean
clean:
	rm -rf $(BUILD_DIR)

# Regenerate every example and confirm the result still builds and runs. CI runs
# this and then checks that the working tree is unchanged, which turns the
# examples into an end-to-end regression test for the generators.
#
# Generation goes through `go generate`, so what runs here is exactly the
# //go:generate line an example shows its readers. The go.work at the repository
# root is what lets the tool directive resolve to this checkout.
.PHONY: examples
examples:
	@for ex in $(GENERATORS); do \
		echo ">>> regenerate examples/$$ex"; \
		(cd examples/$$ex && go generate ./...) || exit 1; \
		(cd examples/$$ex && go vet ./... && go build ./... && go run .) || exit 1; \
	done
