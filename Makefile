BINARY := yaga
TARGET_DIR ?= $(HOME)/dev/test-yaga

.PHONY: build copy styles

build:
	go build -o $(BINARY) ./cmd/yaga

copy: build
	mkdir -p $(TARGET_DIR)
	cp $(BINARY) $(TARGET_DIR)/

# Regenerate the embedded pre-built dashboard stylesheet
# (internal/generator/assets/styles.css) from the kitchen-sink fixture. Dev
# workflow only: the generated dashboard build itself never runs Tailwind.
styles:
	./scripts/build-styles.sh
