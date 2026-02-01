NAME := global-logrotate
VERSION := 2.1.15
RELEASE := 1

# Detect native architecture
NATIVE_ARCH := $(shell uname -m)
NATIVE_GOARCH := $(shell go env GOARCH)

# Supported architectures
ARCHS := amd64 arm64

BINARY := $(NAME)
BUILDDIR := build
RPMDIR := $(BUILDDIR)/rpm
DEBDIR := $(BUILDDIR)/deb

.PHONY: all build build-all clean rpm deb rpm-all deb-all install man packages-all

all: build

# Build for native architecture
build:
	@echo "Building $(NAME) v$(VERSION) for $(NATIVE_GOARCH)..."
	CGO_ENABLED=0 GOOS=linux GOARCH=$(NATIVE_GOARCH) go build -ldflags="-s -w" -o $(BUILDDIR)/$(BINARY)-$(NATIVE_GOARCH) ./cmd/global-logrotate
	@ln -sf $(BINARY)-$(NATIVE_GOARCH) $(BUILDDIR)/$(BINARY)

# Build for all architectures
build-all:
	@echo "Building $(NAME) v$(VERSION) for all architectures..."
	@mkdir -p $(BUILDDIR)
	@for arch in $(ARCHS); do \
		echo "  Building for $$arch..."; \
		CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build -ldflags="-s -w" -o $(BUILDDIR)/$(BINARY)-$$arch ./cmd/global-logrotate; \
	done
	@ln -sf $(BINARY)-$(NATIVE_GOARCH) $(BUILDDIR)/$(BINARY)
	@echo "Binaries built:"
	@ls -lh $(BUILDDIR)/$(BINARY)-*

man:
	@echo "Installing man page..."
	@mkdir -p /usr/share/man/man1
	@cp man/$(NAME).1 /usr/share/man/man1/
	@gzip -f /usr/share/man/man1/$(NAME).1
	@mandb -q 2>/dev/null || true

install: build man
	@echo "Installing $(NAME)..."
	@install -Dm755 $(BUILDDIR)/$(BINARY) /usr/local/bin/$(BINARY)
	@mkdir -p /etc/global-sys-utils/global.conf.d
	@install -Dm644 config/global.conf /etc/global-sys-utils/global.conf
	@install -Dm644 config/global.conf.d/example.conf /etc/global-sys-utils/global.conf.d/example.conf
	@# Install bash completion
	@mkdir -p /usr/share/bash-completion/completions
	@install -Dm644 completions/global-logrotate.bash /usr/share/bash-completion/completions/$(BINARY)
	@# Install zsh completion
	@mkdir -p /usr/share/zsh/vendor-completions
	@install -Dm644 completions/_global-logrotate /usr/share/zsh/vendor-completions/_$(BINARY)
	@echo "Installed to /usr/local/bin/$(BINARY)"
	@echo "Config installed to /etc/global-sys-utils/"
	@echo "Shell completions installed for bash and zsh"

# RPM Package for specific architecture
# Usage: make rpm GOARCH=amd64 or make rpm GOARCH=arm64
rpm:
ifndef GOARCH
	$(eval GOARCH := $(NATIVE_GOARCH))
endif
	$(eval RPM_ARCH := $(if $(filter amd64,$(GOARCH)),x86_64,$(if $(filter arm64,$(GOARCH)),aarch64,$(GOARCH))))
	@echo "Building RPM package for $(RPM_ARCH) (Go arch: $(GOARCH))..."
	@if [ ! -f $(BUILDDIR)/$(BINARY)-$(GOARCH) ]; then \
		echo "  Building binary for $(GOARCH)..."; \
		CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -ldflags="-s -w" -o $(BUILDDIR)/$(BINARY)-$(GOARCH) ./cmd/global-logrotate; \
	fi
	@mkdir -p $(RPMDIR)/$(RPM_ARCH)/BUILD $(RPMDIR)/$(RPM_ARCH)/RPMS $(RPMDIR)/$(RPM_ARCH)/SOURCES $(RPMDIR)/$(RPM_ARCH)/SPECS $(RPMDIR)/$(RPM_ARCH)/SRPMS
	@cp $(BUILDDIR)/$(BINARY)-$(GOARCH) $(RPMDIR)/$(RPM_ARCH)/SOURCES/$(BINARY)
	@cp man/$(NAME).1 $(RPMDIR)/$(RPM_ARCH)/SOURCES/
	@gzip -f $(RPMDIR)/$(RPM_ARCH)/SOURCES/$(NAME).1 2>/dev/null || true
	@cp completions/global-logrotate.bash $(RPMDIR)/$(RPM_ARCH)/SOURCES/
	@cp completions/_global-logrotate $(RPMDIR)/$(RPM_ARCH)/SOURCES/
	@cp config/global.conf $(RPMDIR)/$(RPM_ARCH)/SOURCES/
	@cp config/global.conf.d/example.conf $(RPMDIR)/$(RPM_ARCH)/SOURCES/
	@cp packaging/rpm/$(NAME).spec $(RPMDIR)/$(RPM_ARCH)/SPECS/
	@rpmbuild --define "_topdir $(PWD)/$(RPMDIR)/$(RPM_ARCH)" \
		--define "_version $(VERSION)" \
		--define "_release $(RELEASE)" \
		--define "_rpmfilename %%{NAME}-%%{VERSION}-%%{RELEASE}.$(RPM_ARCH).rpm" \
		--define "__arch_install_post %{nil}" \
		--define "_binaries_in_noarch_packages_terminate_build 0" \
		--target noarch-linux \
		-bb $(RPMDIR)/$(RPM_ARCH)/SPECS/$(NAME).spec
	@mkdir -p $(RPMDIR)/$(RPM_ARCH)/RPMS/$(RPM_ARCH)
	@if [ -f $(RPMDIR)/$(RPM_ARCH)/RPMS/noarch/$(NAME)-$(VERSION)-$(RELEASE).$(RPM_ARCH).rpm ]; then \
		mv $(RPMDIR)/$(RPM_ARCH)/RPMS/noarch/$(NAME)-$(VERSION)-$(RELEASE).$(RPM_ARCH).rpm $(RPMDIR)/$(RPM_ARCH)/RPMS/$(RPM_ARCH)/; \
	elif [ -f $(RPMDIR)/$(RPM_ARCH)/RPMS/noarch/$(NAME)-$(VERSION)-$(RELEASE).noarch.rpm ]; then \
		mv $(RPMDIR)/$(RPM_ARCH)/RPMS/noarch/$(NAME)-$(VERSION)-$(RELEASE).noarch.rpm $(RPMDIR)/$(RPM_ARCH)/RPMS/$(RPM_ARCH)/$(NAME)-$(VERSION)-$(RELEASE).$(RPM_ARCH).rpm; \
	fi
	@echo "RPM package created: $(RPMDIR)/$(RPM_ARCH)/RPMS/$(RPM_ARCH)/$(NAME)-$(VERSION)-$(RELEASE).$(RPM_ARCH).rpm"

# Build RPM for all architectures
rpm-all: build-all
	@$(MAKE) rpm GOARCH=amd64
	@$(MAKE) rpm GOARCH=arm64

# DEB Package for specific architecture
# Usage: make deb GOARCH=amd64 or make deb GOARCH=arm64
deb:
ifndef GOARCH
	$(eval GOARCH := $(NATIVE_GOARCH))
endif
	$(eval DEB_ARCH := $(GOARCH))
	@echo "Building DEB package for $(DEB_ARCH)..."
	@if [ ! -f $(BUILDDIR)/$(BINARY)-$(GOARCH) ]; then \
		echo "  Building binary for $(GOARCH)..."; \
		CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -ldflags="-s -w" -o $(BUILDDIR)/$(BINARY)-$(GOARCH) ./cmd/global-logrotate; \
	fi
	@mkdir -p $(DEBDIR)/$(NAME)_$(VERSION)-$(RELEASE)_$(DEB_ARCH)/DEBIAN
	@mkdir -p $(DEBDIR)/$(NAME)_$(VERSION)-$(RELEASE)_$(DEB_ARCH)/usr/bin
	@mkdir -p $(DEBDIR)/$(NAME)_$(VERSION)-$(RELEASE)_$(DEB_ARCH)/usr/share/man/man1
	@mkdir -p $(DEBDIR)/$(NAME)_$(VERSION)-$(RELEASE)_$(DEB_ARCH)/usr/share/bash-completion/completions
	@mkdir -p $(DEBDIR)/$(NAME)_$(VERSION)-$(RELEASE)_$(DEB_ARCH)/usr/share/zsh/vendor-completions
	@mkdir -p $(DEBDIR)/$(NAME)_$(VERSION)-$(RELEASE)_$(DEB_ARCH)/etc/global-sys-utils/global.conf.d
	@cp $(BUILDDIR)/$(BINARY)-$(GOARCH) $(DEBDIR)/$(NAME)_$(VERSION)-$(RELEASE)_$(DEB_ARCH)/usr/bin/$(BINARY)
	@cp man/$(NAME).1 $(DEBDIR)/$(NAME)_$(VERSION)-$(RELEASE)_$(DEB_ARCH)/usr/share/man/man1/
	@gzip -f $(DEBDIR)/$(NAME)_$(VERSION)-$(RELEASE)_$(DEB_ARCH)/usr/share/man/man1/$(NAME).1 2>/dev/null || true
	@cp completions/global-logrotate.bash $(DEBDIR)/$(NAME)_$(VERSION)-$(RELEASE)_$(DEB_ARCH)/usr/share/bash-completion/completions/$(BINARY)
	@cp completions/_global-logrotate $(DEBDIR)/$(NAME)_$(VERSION)-$(RELEASE)_$(DEB_ARCH)/usr/share/zsh/vendor-completions/_$(BINARY)
	@cp config/global.conf $(DEBDIR)/$(NAME)_$(VERSION)-$(RELEASE)_$(DEB_ARCH)/etc/global-sys-utils/
	@cp config/global.conf.d/example.conf $(DEBDIR)/$(NAME)_$(VERSION)-$(RELEASE)_$(DEB_ARCH)/etc/global-sys-utils/global.conf.d/
	@sed -e "s/{{VERSION}}/$(VERSION)/g" \
		-e "s/{{ARCH}}/$(DEB_ARCH)/g" \
		packaging/deb/control > $(DEBDIR)/$(NAME)_$(VERSION)-$(RELEASE)_$(DEB_ARCH)/DEBIAN/control
	@cp packaging/deb/postinst $(DEBDIR)/$(NAME)_$(VERSION)-$(RELEASE)_$(DEB_ARCH)/DEBIAN/
	@cp packaging/deb/conffiles $(DEBDIR)/$(NAME)_$(VERSION)-$(RELEASE)_$(DEB_ARCH)/DEBIAN/ 2>/dev/null || true
	@chmod 755 $(DEBDIR)/$(NAME)_$(VERSION)-$(RELEASE)_$(DEB_ARCH)/DEBIAN/postinst
	@dpkg-deb --build $(DEBDIR)/$(NAME)_$(VERSION)-$(RELEASE)_$(DEB_ARCH)
	@echo "DEB package created: $(DEBDIR)/$(NAME)_$(VERSION)-$(RELEASE)_$(DEB_ARCH).deb"

# Build DEB for all architectures
deb-all: build-all
	@$(MAKE) deb GOARCH=amd64
	@$(MAKE) deb GOARCH=arm64

# Build all packages for all architectures
packages-all: build-all deb-all rpm-all
	@echo ""
	@echo "==============================================="
	@echo "All packages built successfully!"
	@echo "==============================================="
	@echo ""
	@echo "Binaries:"
	@ls -lh $(BUILDDIR)/$(BINARY)-* 2>/dev/null || true
	@echo ""
	@echo "DEB packages:"
	@ls -lh $(DEBDIR)/*.deb 2>/dev/null || true
	@echo ""
	@echo "RPM packages:"
	@find $(RPMDIR) -name "*.rpm" -exec ls -lh {} \; 2>/dev/null || true

clean:
	@rm -rf $(BUILDDIR)
	@echo "Cleaned build directory"
