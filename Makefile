# normMP3 — compilation et paquets de distribution.
#
#   make              liste les cibles
#   make run          lance l'application
#   make check        gofmt + go vet + tests
#   make dist         construit tous les paquets possibles dans dist/
#
# L'application est 100% Go pur : Windows et Linux se compilent sans cgo
# depuis n'importe quelle machine. macOS aussi (shirei passe par purego) ;
# les paquets macOS (bundle .app, lipo universel, codesign) utilisent des
# outils Apple et se construisent donc sur un Mac.

APP      := NormMP3
BIN      := normmp3
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GO       ?= go

BUILD_DIR := build
DIST_DIR  := dist
STAGE_DIR := $(BUILD_DIR)/stage
DIST_ABS  := $(abspath $(DIST_DIR))
LDFLAGS   := -X main.version=$(VERSION)
PKG       := ./cmd/normmp3
HOST_OS   := $(shell $(GO) env GOOS)

# Le traitement audio utilise un ffmpeg statique embarqué dans le binaire ;
# les archives sont téléchargées une fois par machine (voir le script).
FFMPEG_BINS := $(wildcard internal/ffmpeg/bin/*.xz)

ffmpeg-bins: ## Télécharge les ffmpeg statiques à embarquer (si absent)
	@scripts/fetch-ffmpeg.sh all

.DEFAULT_GOAL := help

.PHONY: help run build test vet fmt check clean distclean dist ffmpeg-bins \
	dist-macos dist-macos-intel dist-macos-silicon dist-macos-universal \
	dist-windows dist-windows-amd64 dist-windows-arm64 \
	dist-linux dist-linux-amd64 dist-linux-arm64 \
	checksums version

help: ## Affiche cette aide
	@echo "$(APP) $(VERSION)"
	@echo
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk -F':.*?## ' '{printf "  %-24s %s\n", $$1, $$2}'
	@echo
	@echo "  Paquets produits dans $(DIST_DIR)/ :"
	@echo "    $(APP)-$(VERSION)-macos-{intel,apple-silicon,universal}.zip"
	@echo "    $(APP)-$(VERSION)-windows-{amd64,arm64}.zip"
	@echo "    $(APP)-$(VERSION)-linux-{amd64,arm64}.tar.gz"

version: ## Affiche la version qui sera compilée
	@echo $(VERSION)

# ----------------------------------------------------------- développement

run: ffmpeg-bins ## Lance l'application
	$(GO) run -ldflags "$(LDFLAGS)" $(PKG)

build: $(BUILD_DIR)/$(BIN) ## Compile pour cette machine (build/)

$(BUILD_DIR)/$(BIN): $(shell find . -name '*.go') go.mod go.sum $(FFMPEG_BINS)
	@mkdir -p $(BUILD_DIR)
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $@ $(PKG)

test: ## Exécute les tests
	$(GO) test ./...

vet: ## Analyse statique
	$(GO) vet ./...

fmt: ## Reformate le code
	gofmt -w .

check: ffmpeg-bins ## Vérifie le format, l'analyse statique et les tests
	@if [ -n "$$(gofmt -l .)" ]; then \
		echo "gofmt : fichiers à reformater :"; gofmt -l .; exit 1; \
	fi
	$(GO) vet ./...
	$(GO) test ./...

clean: ## Supprime les fichiers de compilation
	rm -rf $(BUILD_DIR)

distclean: clean ## Supprime aussi les paquets
	rm -rf $(DIST_DIR)

# ------------------------------------------------------------------ macOS
#
# Bundle .app minimal (Info.plist + binaire), signé « ad hoc » : sans
# signature posée après assemblage, lipo invalide de toute façon celle du
# linker, et un binaire arm64 non signé est refusé par macOS.

# $(1) = GOARCH
define macos_binary
	@mkdir -p $(BUILD_DIR)/macos
	CGO_ENABLED=0 GOOS=darwin GOARCH=$(1) $(GO) build -trimpath \
		-ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/macos/$(BIN)-$(1) $(PKG)
endef

# $(1) = étiquette du paquet, $(2) = binaire à embarquer
define macos_package
	rm -rf $(STAGE_DIR)/macos-$(1)
	mkdir -p $(STAGE_DIR)/macos-$(1)/$(APP).app/Contents/MacOS
	cp $(2) $(STAGE_DIR)/macos-$(1)/$(APP).app/Contents/MacOS/$(BIN)
	sed -e 's/@VERSION@/$(VERSION)/g' -e 's/@BIN@/$(BIN)/g' packaging/Info.plist.in \
		> $(STAGE_DIR)/macos-$(1)/$(APP).app/Contents/Info.plist
	cp packaging/INSTALLATION-macos.txt $(STAGE_DIR)/macos-$(1)/INSTALLATION.txt
	xattr -cr $(STAGE_DIR)/macos-$(1)/$(APP).app
	codesign --force --sign - --timestamp=none $(STAGE_DIR)/macos-$(1)/$(APP).app
	@mkdir -p $(DIST_DIR)
	rm -f $(DIST_ABS)/$(APP)-$(VERSION)-macos-$(1).zip
	ditto -c -k --norsrc --noextattr $(STAGE_DIR)/macos-$(1) \
		$(DIST_ABS)/$(APP)-$(VERSION)-macos-$(1).zip
	@echo "  → $(DIST_DIR)/$(APP)-$(VERSION)-macos-$(1).zip"
endef

dist-macos: dist-macos-intel dist-macos-silicon dist-macos-universal ffmpeg-bins ## Paquets macOS (Intel, Apple Silicon, universel)

dist-macos-intel: ffmpeg-bins ## Paquet macOS Intel (.zip)
	$(call macos_binary,amd64)
	$(call macos_package,intel,$(BUILD_DIR)/macos/$(BIN)-amd64)

dist-macos-silicon: ffmpeg-bins ## Paquet macOS Apple Silicon (.zip)
	$(call macos_binary,arm64)
	$(call macos_package,apple-silicon,$(BUILD_DIR)/macos/$(BIN)-arm64)

dist-macos-universal: ffmpeg-bins ## Paquet macOS universel Intel + Apple Silicon (.zip)
	$(call macos_binary,amd64)
	$(call macos_binary,arm64)
	lipo -create -output $(BUILD_DIR)/macos/$(BIN)-universal \
		$(BUILD_DIR)/macos/$(BIN)-amd64 $(BUILD_DIR)/macos/$(BIN)-arm64
	$(call macos_package,universal,$(BUILD_DIR)/macos/$(BIN)-universal)

# ---------------------------------------------------------------- Windows
#
# -H=windowsgui : application graphique, sans fenêtre de console derrière.

# $(1) = GOARCH
define windows_package
	rm -rf $(STAGE_DIR)/windows-$(1)
	mkdir -p $(STAGE_DIR)/windows-$(1)/$(APP)
	CGO_ENABLED=0 GOOS=windows GOARCH=$(1) $(GO) build -trimpath \
		-ldflags "-H=windowsgui $(LDFLAGS)" \
		-o $(STAGE_DIR)/windows-$(1)/$(APP)/$(BIN).exe $(PKG)
	cp packaging/INSTALLATION-windows.txt $(STAGE_DIR)/windows-$(1)/$(APP)/INSTALLATION.txt
	@mkdir -p $(DIST_DIR)
	rm -f $(DIST_ABS)/$(APP)-$(VERSION)-windows-$(1).zip
	cd $(STAGE_DIR)/windows-$(1) && zip -qr $(DIST_ABS)/$(APP)-$(VERSION)-windows-$(1).zip $(APP)
	@echo "  → $(DIST_DIR)/$(APP)-$(VERSION)-windows-$(1).zip"
endef

dist-windows: ffmpeg-bins dist-windows-amd64 dist-windows-arm64 ## Paquets Windows (x86-64 et ARM64)

dist-windows-amd64: ffmpeg-bins ## Paquet Windows x86-64 (.zip)
	$(call windows_package,amd64)

dist-windows-arm64: ffmpeg-bins ## Paquet Windows ARM64 (.zip)
	$(call windows_package,arm64)

# ------------------------------------------------------------------ Linux

# $(1) = GOARCH
define linux_package
	rm -rf $(STAGE_DIR)/linux-$(1)
	mkdir -p $(STAGE_DIR)/linux-$(1)/$(APP)
	CGO_ENABLED=0 GOOS=linux GOARCH=$(1) $(GO) build -trimpath \
		-ldflags "$(LDFLAGS)" -o $(STAGE_DIR)/linux-$(1)/$(APP)/$(BIN) $(PKG)
	cp packaging/INSTALLATION-linux.txt $(STAGE_DIR)/linux-$(1)/$(APP)/INSTALLATION.txt
	@mkdir -p $(DIST_DIR)
	tar -czf $(DIST_ABS)/$(APP)-$(VERSION)-linux-$(1).tar.gz \
		-C $(STAGE_DIR)/linux-$(1) $(APP)
	@echo "  → $(DIST_DIR)/$(APP)-$(VERSION)-linux-$(1).tar.gz"
endef

dist-linux: ffmpeg-bins dist-linux-amd64 dist-linux-arm64 ## Paquets Linux (x86-64 et ARM64)

dist-linux-amd64: ffmpeg-bins ## Paquet Linux x86-64 (.tar.gz)
	$(call linux_package,amd64)

dist-linux-arm64: ffmpeg-bins ## Paquet Linux ARM64 (.tar.gz)
	$(call linux_package,arm64)

# ------------------------------------------------------------------- tout

dist: dist-windows dist-linux ffmpeg-bins ## Construit tous les paquets possibles sur cette machine
ifeq ($(HOST_OS),darwin)
dist: dist-macos
endif

checksums: ## Écrit dist/SHA256SUMS.txt
	@cd $(DIST_DIR) && \
		( command -v shasum >/dev/null && shasum -a 256 $(APP)-* > SHA256SUMS.txt \
		  || sha256sum $(APP)-* > SHA256SUMS.txt )
	@echo "  → $(DIST_DIR)/SHA256SUMS.txt"
