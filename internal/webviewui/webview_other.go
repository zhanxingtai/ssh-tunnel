//go:build !windows && webview

package webviewui

import webview "github.com/jchv/go-webview-selector"

// showWebview does nothing outside Windows: the other toolkits show the
// webview together with the window.
func showWebview(w webview.WebView) {}
