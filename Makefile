PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin

.PHONY: build install test vet clean

build:
	mkdir -p build
	go build -o build/tmx ./cmd/tmx

install: build
	@mkdir -p "$(DESTDIR)$(BINDIR)" && install -m 755 build/tmx "$(DESTDIR)$(BINDIR)/tmx" || { \
		echo 'Installation failed. Check directory permissions; if needed, run: sudo make install' >&2; \
		exit 1; \
	}
	@echo 'Installed tmx to $(DESTDIR)$(BINDIR)/tmx'
	@if [ -z "$(DESTDIR)" ]; then \
		case ":$$PATH:" in \
			*:"$(BINDIR)":*) ;; \
			*) echo 'Warning: $(BINDIR) is not on PATH. Use make install BINDIR=/an/existing/PATH/directory.' >&2 ;; \
		esac; \
	fi

test:
	go test -race ./...

vet:
	go vet ./...

clean:
	rm -rf build
