package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSourceCheck(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runSourceCheck(&stdout, &stderr, []string{filepath.Join("..", "..", "internal", "source", "examples", "ninebot.json")})
	if err != nil {
		t.Fatalf("合法文件检查失败：%v (stderr=%s)", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "检查通过") {
		t.Errorf("stdout = %q", stdout.String())
	}

	// 违反探测域名规则的文件应当返回非 nil 错误。
	badFile := filepath.Join(t.TempDir(), "bad.json")
	badContent := `{
  "format": 1,
  "meta": { "version": 1 },
  "source": {
    "key": "bad",
    "name": "违规来源",
    "probe": {
      "url": "https://other.example.org/check",
      "expect": "status == 200"
    },
    "matchers": [
      { "name": "api", "host": "api.example.com" }
    ],
    "fields": [
      { "key": "token", "in": "header", "path": "Authorization" }
    ]
  }
}`
	if err := os.WriteFile(badFile, []byte(badContent), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if err := runSourceCheck(&stdout, &stderr, []string{badFile}); err == nil {
		t.Fatalf("违规文件应当报错，stderr=%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "规则违规") {
		t.Errorf("stderr = %q", stderr.String())
	}
}
