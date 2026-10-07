PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin

.PHONY: build install test vet clean docs docs-check

docs:
	go run ./internal/gendocs

docs-check:
	go run ./internal/gendocs --check

build:
	mkdir -p build
	go build -o build/tmix ./cmd/tmix

install: build
	@mkdir -p "$(DESTDIR)$(BINDIR)" && install -m 755 build/tmix "$(DESTDIR)$(BINDIR)/tmix" || { \
		echo 'Installation failed. Check directory permissions; if needed, run: sudo make install' >&2; \
		exit 1; \
	}
	@echo 'Installed tmix to $(DESTDIR)$(BINDIR)/tmix'
	@if [ -z "$(DESTDIR)" ]; then \
		case ":$$PATH:" in \
			*:"$(BINDIR)":*) ;; \
			*) echo 'Warning: $(BINDIR) is not on PATH. Use make install BINDIR=/an/existing/PATH/directory.' >&2 ;; \
		esac; \
	fi

test: docs-check
	go test -race ./...

vet:
	go vet ./...

clean:
	rm -rf build
