package geopicker

import (
	"encoding/json"
	"io/fs"
	"net/http"
)

// newMux 只做三件事: 提供页面文件、接收选点结果、接收取消。
// 逆地理编码由页面直接请求高德 REST 接口, 不经服务端。
func newMux(assets fs.FS, resultCh chan Location, cancelCh chan struct{}) http.Handler {
	files := http.FileServer(http.FS(assets))
	mux := http.NewServeMux()

	mux.HandleFunc("/pick", func(w http.ResponseWriter, r *http.Request) {
		var loc Location
		if err := json.NewDecoder(r.Body).Decode(&loc); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write([]byte(`{"ok":true}`))
		resultCh <- loc
	})

	mux.HandleFunc("/cancel", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
		select {
		case cancelCh <- struct{}{}:
		default:
		}
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			// 从嵌入文件系统读 picker.html
			data, err := fs.ReadFile(assets, "picker.html")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(data)
			return
		}
		files.ServeHTTP(w, r)
	})

	return mux
}
