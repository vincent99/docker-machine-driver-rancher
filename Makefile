.PHONY : build dist dist-clean fmt release tag-release deps vet test

NAME=docker-machine-driver-rancher
VERSION := $(shell cat VERSION)

ifneq ($(CIRCLE_BUILD_NUM),)
	BUILD:=$(VERSION)-$(CIRCLE_BUILD_NUM)
else
	BUILD:=$(VERSION)
endif

LDFLAGS:=-X main.Version=$(VERSION)

all: dist

build:
	mkdir -p build
	go build -a -ldflags "$(LDFLAGS)" -o build/$(NAME)

dist-clean:
	rm -rf dist
	rm -rf release

dist: dist-clean dist-darwin dist-amd64 dist-armhf

dist-darwin:
	mkdir -p release dist/darwin/amd64
	GOOS=darwin GOARCH=amd64 go build -a -ldflags "$(LDFLAGS)" -o dist/darwin/amd64/$(NAME)
	tar -cvzf release/$(NAME)-$(VERSION)-darwin-amd64.tar.gz -C dist/darwin/amd64 $(NAME)
	cd $(shell pwd)/release && md5 $(NAME)-$(VERSION)-darwin-amd64.tar.gz > $(NAME)-$(VERSION)-darwin-amd64.tar.gz.md5

dist-amd64:
	mkdir -p release dist/linux/amd64
	GOOS=linux GOARCH=amd64 go build -a -ldflags "$(LDFLAGS)" -o dist/linux/amd64/$(NAME)
	tar -cvzf release/$(NAME)-$(VERSION)-linux-amd64.tar.gz -C dist/linux/amd64 $(NAME)
	cd $(shell pwd)/release && md5 $(NAME)-$(VERSION)-linux-amd64.tar.gz > $(NAME)-$(VERSION)-linux-amd64.tar.gz.md5

dist-armhf:
	mkdir -p release dist/linux/armhf
	GOOS=linux GOARCH=arm GOARM=6 go build -a -ldflags "$(LDFLAGS)" -o dist/linux/armhf/$(NAME)
	tar -cvzf release/$(NAME)-$(VERSION)-linux-armhf.tar.gz -C dist/linux/armhf $(NAME)
	cd $(shell pwd)/release && md5 $(NAME)-$(VERSION)-linux-armhf.tar.gz > $(NAME)-$(VERSION)-linux-armhf.tar.gz.md5

#dist-windows:
#	mkdir -p dist/windows/amd64 && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -a -ldflags "$(LDFLAGS)" -o dist/windows/amd64/$(NAME).exe ./bin
#	tar -cvzf release/$(NAME)-$(VERSION)-windows-amd64.tar.gz -C dist/windows/amd64 $(NAME).exe
#	cd $(shell pwd)/release && md5 $(NAME)-$(VERSION)-windows-amd64.tar.gz > $(NAME)-$(VERSION)-windows-amd64.tar.gz.md5
#

release: dist
	ghr -u vincent99 -r docker-machine-driver-rancher --replace $(VERSION) release/

tag-release:
	git tag -f `cat VERSION`
	git push -f origin master --tags

deps:
	go get -u github.com/tcnksm/ghr

vet:
	@if [ -n "$(shell gofmt -l .)" ]; then \
		echo 1>&2 'The following files need to be formatted:'; \
		gofmt -l .; \
		exit 1; \
	fi
	godep go vet .

test:
	godep go test -race ./...
