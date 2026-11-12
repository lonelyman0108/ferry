package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "good" {
			_, _ = w.Write([]byte(`{"code":401,"msg":"登录过期"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"userId":12345}}`))
	}))
	defer ts.Close()
	s := Spec{URL: ts.URL + "/status", Headers: map[string]string{"Authorization": "{{.authorization}}"}, Expect: "status == 200 && json.code == 0", Identity: "json.data.userId"}
	if err := s.Validate([]string{"authorization"}); err != nil {
		t.Fatal(err)
	}
	r := Run(context.Background(), nil, s, map[string]string{"authorization": "good"})
	if r.Err != nil || !r.OK || r.Identity != "12345" {
		t.Errorf("有效登录态 = %+v", r)
	}
	r = Run(context.Background(), nil, s, map[string]string{"authorization": "bad"})
	if r.Err != nil || r.OK || !strings.Contains(r.Detail, "未通过") {
		t.Errorf("无效登录态 = %+v", r)
	}
	r = Run(context.Background(), nil, Spec{URL: "http://127.0.0.1:1/x?token={{.authorization}}", Expect: "true", Timeout: 1}, map[string]string{"authorization": "login-state-secret"})
	if r.Err == nil || strings.Contains(r.Err.Error(), "login-state-secret") {
		t.Errorf("连不上时应返回 Err，且不带出渲染后的地址：%v", r.Err)
	}
}

func TestValidate(t *testing.T) {
	bad := []Spec{
		{URL: "ftp://x", Expect: "true"},
		{URL: "https://x/{{.missing}}", Expect: "true"},
		{URL: "https://x", Headers: map[string]string{"A": "{{.nope}}"}, Expect: "true"},
		{URL: "https://x"},
		{URL: "https://x", Expect: "status +"},
		{URL: "https://x", Expect: "status"},
		{URL: "https://x", Expect: "true", Method: "TRACE"},
		{URL: "https://x", Expect: "true", Timeout: 99},
	}
	for i, s := range bad {
		if s.Validate([]string{"a"}) == nil {
			t.Errorf("配置 %d 应当不合法：%+v", i, s)
		}
	}
}
