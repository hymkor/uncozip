ifeq ($(OS),Windows_NT)
    SHELL=CMD.EXE
    SET=SET
    NUL=nul
else
    SET=export
    NUL=/dev/null
endif
NAME:=$(notdir $(CURDIR))
VERSION:=$(shell git describe --tags 2>$(NUL) || echo v0.0.0)
GOOPT=-ldflags "-s -w -X main.version=$(VERSION)"
EXE:=$(shell go env GOEXE)

all:
	go fmt
	$(SET) "CGO_ENABLED=0" && go build $(GOOPT)
	cd "cmd/uncozip" && go fmt && $(SET) "CGO_ENABLED=0" && go build -o ../../$(NAME)$(EXE) $(GOOPT)

_dist:
	$(MAKE) all
	zip $(NAME)-$(VERSION)-$(GOOS)-$(GOARCH).zip $(NAME)$(EXE)

dist:
	$(SET) "GOOS=linux"   && $(SET) "GOARCH=386"   && $(MAKE) _dist
	$(SET) "GOOS=linux"   && $(SET) "GOARCH=amd64" && $(MAKE) _dist
	$(SET) "GOOS=windows" && $(SET) "GOARCH=386"   && $(MAKE) _dist
	$(SET) "GOOS=windows" && $(SET) "GOARCH=amd64" && $(MAKE) _dist

release:
	gh release create -d --notes "" -t $(VERSION) $(VERSION) $(wildcard $(NAME)-$(VERSION)-*.zip)

manifest:
	make-scoop-manifest *-windows-*.zip > $(NAME).json

5GB.zip:
	fsutil.exe file createnew 5GB-1 5000000000
	fsutil.exe file createnew 5GB-2 5000000000
	zip -m 5GB.zip 5GB-1 5GB-2

.PHONY: manifest release dist _dist all
