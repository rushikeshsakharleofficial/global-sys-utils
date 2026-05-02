NAME := global-logrotate
VERSION := 2.1.15
RELEASE := 1

BUILDDIR := build
BINARY := $(NAME)

.PHONY: all build clean rpm deb

all: build

build:
	@echo "Building $(NAME) (C version)..."
	@mkdir -p $(BUILDDIR)
	gcc -O2 -o $(BUILDDIR)/$(BINARY) src/global-logrotate.c

install: build
	install -Dm755 $(BUILDDIR)/$(BINARY) /usr/local/bin/$(BINARY)

clean:
	rm -rf $(BUILDDIR)

rpm:
	@echo "Build RPM using existing spec (binary already built)"


deb:
	@echo "Build DEB using existing control (binary already built)"
