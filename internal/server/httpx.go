package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
)

const maxBodyBytes = 1 << 20

type errorBody struct {
	Error   string   `json:"error"`
	Details []string `json:"details,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}

// isJSON 判断请求是否声明了 JSON 正文。跨站表单发不出这种请求，
// 所以要求写操作带上它，可以挡住借用 Cookie 的跨站请求。
func isJSON(r *http.Request) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mt == "application/json"
}

// decodeJSON 读取 JSON 正文，拒绝未知字段和超过 1 MB 的正文。失败时已写好响应，返回 false。
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if !isJSON(r) {
		writeError(w, http.StatusUnsupportedMediaType, "请求正文须为 application/json")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "请求正文过大")
		} else {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("请求正文不是合法的 JSON：%v", err))
		}
		return false
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		writeError(w, http.StatusBadRequest, "请求正文仅允许包含一个 JSON 对象")
		return false
	}
	return true
}

// clientIP 返回连接的来源 IP，不读取可伪造的转发头。
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
