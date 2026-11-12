package source

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// maxYAMLBytes 是导入 YAML 的大小上限。
const maxYAMLBytes = 256 << 10

// MarshalYAML 导出来源定义，只有规则，不含任何取到的值。
func MarshalYAML(d Doc) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("# Ferry 来源定义，只含匹配和取值规则，不含任何登录态。\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(d); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// UnmarshalYAML 解析来源定义，拒绝未知字段，并完成 Normalize 和 Validate。
func UnmarshalYAML(data []byte) (Doc, error) {
	var d Doc
	if len(data) > maxYAMLBytes {
		return d, fmt.Errorf("YAML 不能超过 %d KB", maxYAMLBytes>>10)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&d); err != nil {
		return d, fmt.Errorf("YAML 解析失败：%w", err)
	}
	d.Normalize()
	return d, d.Validate()
}

//go:embed examples/*.yaml
var exampleFS embed.FS

// Example 是内置的来源示例。
type Example struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Doc         Doc    `json:"-"`
}

// Examples 返回全部内置示例，按 key 排序。示例文件不合法时直接 panic，由测试保证。
func Examples() []Example {
	entries, err := fs.ReadDir(exampleFS, "examples")
	if err != nil {
		panic(err)
	}
	var out []Example
	for _, e := range entries {
		data, err := exampleFS.ReadFile(path.Join("examples", e.Name()))
		if err != nil {
			panic(err)
		}
		d, err := UnmarshalYAML(data)
		if err != nil {
			panic(fmt.Sprintf("内置示例 %s 不合法：%v", e.Name(), err))
		}
		out = append(out, Example{Key: d.Key, Name: d.Name, Description: strings.TrimSpace(d.Description), Doc: d})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
