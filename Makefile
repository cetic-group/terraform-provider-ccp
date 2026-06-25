HOSTNAME=registry.terraform.io
NAMESPACE=cetic-group
NAME=ccp
VERSION=5.4.0
OS_ARCH=$(shell go env GOOS)_$(shell go env GOARCH)
BIN=terraform-provider-$(NAME)
INSTALL_DIR=$$HOME/.terraform.d/plugins/$(HOSTNAME)/$(NAMESPACE)/$(NAME)/$(VERSION)/$(OS_ARCH)

.PHONY: build install fmt vet tidy clean

build:
	go build -ldflags "-X main.version=v$(VERSION)" -o $(BIN)

install: build
	mkdir -p $(INSTALL_DIR)
	mv $(BIN) $(INSTALL_DIR)/

fmt:
	gofmt -w .

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -f $(BIN)
	rm -rf $(INSTALL_DIR)
