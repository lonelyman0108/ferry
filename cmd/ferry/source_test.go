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
	err := runSourceCheck(&stdout, &stderr, []string{filepath.Join("..", "..", "internal", "source", "testdata", "ninebot.json")})
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

func TestRunHubIndex(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "sources", "demo")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	srcJSON := `{
  "format": 1,
  "meta": { "version": 1, "author": "lonelyman0108", "tags": ["测试"] },
  "source": {
    "key": "demo",
    "name": "示例来源",
    "matchers": [
      { "name": "api", "host": "api.example.com" }
    ],
    "fields": [
      { "key": "token", "in": "header", "path": "Authorization" }
    ]
  }
}
`
	casesJSON := `{
  "cases": [
    {
      "name": "示例请求",
      "request": {
        "method": "GET",
        "url": "https://api.example.com/v1/user",
        "headers": { "Authorization": "Bearer sample_token" }
      },
      "expected": { "token": "Bearer sample_token" }
    }
  ]
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "source.json"), []byte(srcJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "README.md"), []byte("# 示例\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "cases.json"), []byte(casesJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := runHubIndex(&stdout, &stderr, []string{"--check", root}); err == nil {
		t.Fatalf("尚未生成 index.json 时 --check 应当失败")
	}

	stdout.Reset()
	stderr.Reset()
	if err := runHubIndex(&stdout, &stderr, []string{root}); err != nil {
		t.Fatalf("生成 index.json 失败：%v (stderr=%s)", err, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if err := runHubIndex(&stdout, &stderr, []string{"--check", root}); err != nil {
		t.Fatalf("生成后 --check 应当通过：%v (stderr=%s)", err, stderr.String())
	}
}
