BINARY := ssh-tunnel
PKG    := ./cmd/ssh-tunnel
BIN    := bin

GO ?= go

# Windows 自带原生窗口（纯 Go 的 WebView2，无需 cgo）。
# Linux/macOS 的原生窗口需要 webview/webview_go（cgo + gtk3 + webkit2gtk-4.0），
# 用 build-webview 目标单独构建；默认构建走系统浏览器。
#
# -H windowsgui 让 Windows 以 GUI 子系统启动，程序不分配控制台，
# 免得开出一个黑窗口。链接器只在 PE 文件上认这个参数，其它平台会忽略它。
.PHONY: all build build-webview build-windows build-darwin build-all test vet fmt check clean install icons

all: build

# 重新生成图标：矢量母版、PNG、ICO，以及 Windows 可执行文件用的资源对象。
# 改完图形后要跑一次，并把生成的文件一起提交。
icons:
	$(GO) run scripts/genicon/main.go
	$(GO) run scripts/gensyso/main.go

build:
	$(GO) build -ldflags "-H windowsgui" -o $(BIN)/$(BINARY) $(PKG)

build-webview:
	$(GO) build -tags webview -ldflags "-H windowsgui" -o $(BIN)/$(BINARY) $(PKG)

build-windows:
	GOOS=windows GOARCH=amd64 $(GO) build -ldflags "-H windowsgui" -o $(BIN)/$(BINARY).exe $(PKG)

build-darwin:
	GOOS=darwin GOARCH=arm64 $(GO) build -o $(BIN)/$(BINARY)-darwin-arm64 $(PKG)

build-all: build build-windows build-darwin

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

check: fmt vet test

install: build
	install -m 0755 $(BIN)/$(BINARY) $(DESTDIR)/usr/local/bin/$(BINARY)

clean:
	rm -rf $(BIN)
