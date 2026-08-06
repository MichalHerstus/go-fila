BINARY := go-fila
TARGET_DIR ?= $(HOME)/dev/pokus-fila

.PHONY: build copy

build:
	go build -o $(BINARY) ./cmd/go-fila

copy: build
	mkdir -p $(TARGET_DIR)
	cp $(BINARY) $(TARGET_DIR)/
