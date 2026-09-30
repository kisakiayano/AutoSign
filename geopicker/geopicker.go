// Package geopicker — 最轻量的地图选点: 纯标准库, 无 CGO、无第三方依赖。
//
//	loc, err := geopicker.Pick(geopicker.Options{StaticDir: "geopicker"})
//
// 页面(picker.html/picker.css/picker.js)与 Go 代码分离, 运行时从 StaticDir 提供。
package geopicker

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// Location 选择结果。
type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Address   string  `json:"address"`
}

// Options 配置, 全部字段可为零值。
type Options struct {
	Listen    string        // 监听地址, 空用 127.0.0.1:0
	StaticDir string        // 页面文件目录, 空用当前工作目录
	Timeout   time.Duration // 等待超时, 空用 10 分钟
	NoBrowser bool          // 不自动打开浏览器
	CenterLng float64       // 初始中心经度, 与 CenterLat 同时为 0 时用北京
	CenterLat float64
	Zoom      int    // 初始缩放, 空用 13
	AMapKey   string // 高德 Web 服务 key(逆地理编码用, 空用内置默认)
}

// Pick 启动本地选点页面并阻塞等待用户在地图上选点。
func Pick(opts Options) (Location, error) {
	if opts.Listen == "" {
		opts.Listen = "127.0.0.1:0"
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Minute
	}
	if opts.Zoom <= 0 {
		opts.Zoom = 13
	}
	if opts.CenterLng == 0 && opts.CenterLat == 0 {
		opts.CenterLng, opts.CenterLat = 116.397, 39.908
	}

	var assets fs.FS = embeddedAssets
	if opts.StaticDir != "" {
		if _, err := os.Stat(opts.StaticDir + "/picker.html"); err != nil {
			return Location{}, fmt.Errorf("geopicker: 在 %s 找不到 picker.html: %w", opts.StaticDir, err)
		}
		assets = os.DirFS(opts.StaticDir)
	}

	resultCh := make(chan Location, 1)
	cancelCh := make(chan struct{}, 1)

	ln, err := net.Listen("tcp", opts.Listen)
	if err != nil {
		return Location{}, err
	}
	srv := &http.Server{Handler: newMux(assets, resultCh, cancelCh)}
	go srv.Serve(ln)
	defer srv.Close()

	pageURL := fmt.Sprintf("http://%s/?lng=%v&lat=%v&zoom=%d&key=%s",
		ln.Addr(), opts.CenterLng, opts.CenterLat, opts.Zoom, url.QueryEscape(opts.AMapKey))
	fmt.Fprintln(os.Stderr, "[geo_picker] 地图选址页面:", pageURL)
	if !opts.NoBrowser {
		if err := openBrowser(pageURL); err != nil {
			fmt.Fprintln(os.Stderr, "[geo_picker] 打开浏览器失败, 请手动访问上面的地址:", err)
		}
	}

	select {
	case loc := <-resultCh:
		return loc, nil
	case <-cancelCh:
		return Location{}, errors.New("用户取消")
	case <-time.After(opts.Timeout):
		return Location{}, errors.New("等待超时")
	}
}

func openBrowser(u string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", u).Start()
	case "windows":
		return exec.Command("cmd", "/c", "start", "", u).Start()
	default:
		return exec.Command("xdg-open", u).Start()
	}
}
