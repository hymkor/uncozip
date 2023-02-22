NAME=$(lastword $(subst /, ,$(abspath .)))
VERSION=$(shell git.exe describe --tags 2>nul || echo v0.0.0)
GOOPT=-ldflags "-s -w -X main.version=$(VERSION)"
EXT=$(shell go env GOEXE)

ifeq ($(OS),Windows_NT)
    SHELL=CMD.EXE
    SET=SET
else
    SET=export
endif

all:
	go fmt
	$(SET) "CGO_ENABLED=0" && go build $(GOOPT)
	cd cmd/uncozip && go fmt && $(SET) "CGO_ENABLED=0" && go build -o ../../$(NAME)$(EXT) $(GOOPT)

_package:
	$(MAKE) all
	zip $(NAME)-$(VERSION)-$(GOOS)-$(GOARCH).zip $(NAME)$(EXT)

package:
	$(SET) "GOOS=linux"   && $(SET) "GOARCH=386"   && $(MAKE) _package
	$(SET) "GOOS=linux"   && $(SET) "GOARCH=amd64" && $(MAKE) _package
	$(SET) "GOOS=windows" && $(SET) "GOARCH=386"   && $(MAKE) _package
	$(SET) "GOOS=windows" && $(SET) "GOARCH=amd64" && $(MAKE) _package

release:
	gh release create -d -t $(VERSION) $(VERSION) $(wildcard $(NAME)-$(VERSION)-*.zip)
manifest:
	make-scoop-manifest *-windows-*.zip > $(NAME).json

5GB.zip:
	fsutil.exe file createnew 5GB-1 5000000000
	fsutil.exe file createnew 5GB-2 5000000000
	zip -m 5GB.zip 5GB-1 5GB-2
