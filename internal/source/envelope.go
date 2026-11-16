package source

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// JSON 外壳常量与导入上限。
const (
	SchemaURL      = "https://raw.githubusercontent.com/lonelyman0108/ferry/main/schema/source.v1.json"
	EnvelopeFormat = 1
	MaxImportBytes = 1 << 20
	MaxImportItems = 50
)

// Meta 是外壳的元信息（规则版本号、作者、主页、标签），不进入来源本身的运行定义。
type Meta struct {
	Version  int      `json:"version"`
	Author   string   `json:"author,omitempty"`
	Homepage string   `json:"homepage,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

// Normalize 去掉首尾空白与空标签。
func (m *Meta) Normalize() {
	m.Author = strings.TrimSpace(m.Author)
	m.Homepage = strings.TrimSpace(m.Homepage)
	if len(m.Tags) > 0 {
		tags := make([]string, 0, len(m.Tags))
		for _, t := range m.Tags {
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, t)
			}
		}
		m.Tags = tags
	}
}

// Validate 检查外壳元信息。
func (m *Meta) Validate() error {
	if m.Version < 1 {
		return fmt.Errorf("meta.version 须为正整数")
	}
	if len(m.Author) > 64 {
		return fmt.Errorf("meta.author 不超过 64 个字节")
	}
	if m.Homepage != "" {
		u, err := url.Parse(m.Homepage)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(m.Homepage) > 512 {
			return fmt.Errorf("meta.homepage 须为 http 或 https 地址")
		}
	}
	if len(m.Tags) > 10 {
		return fmt.Errorf("meta.tags 最多 10 个")
	}
	for _, t := range m.Tags {
		if len(t) > 32 {
			return fmt.Errorf("meta.tags 单项不超过 32 个字节")
		}
	}
	return nil
}

// Envelope 是来源分享与导出的 JSON 外壳。
type Envelope struct {
	Schema string `json:"$schema,omitempty"`
	Format int    `json:"format"`
	Meta   Meta   `json:"meta"`
	Source Doc    `json:"source"`
}

// MarshalEnvelope 校验来源定义与元信息后，序列化为带缩进的 JSON 外壳。
func MarshalEnvelope(d Doc, meta Meta) ([]byte, error) {
	d.Normalize()
	if err := d.Validate(); err != nil {
		return nil, err
	}
	meta.Normalize()
	if meta.Version < 1 {
		meta.Version = 1
	}
	if err := meta.Validate(); err != nil {
		return nil, err
	}
	env := Envelope{
		Schema: SchemaURL,
		Format: EnvelopeFormat,
		Meta:   meta,
		Source: d,
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(env); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeSingleEnvelope 解析单个 JSON 外壳并校验外壳结构与元信息，不调用 Source.Validate。
func DecodeSingleEnvelope(data []byte) (Envelope, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return Envelope{}, fmt.Errorf("JSON 内容为空")
	}
	if len(trimmed) > MaxImportBytes {
		return Envelope{}, fmt.Errorf("JSON 内容超过 %d KB", MaxImportBytes>>10)
	}
	if trimmed[0] == '[' {
		return Envelope{}, fmt.Errorf("此处仅接受单个来源对象，不接受数组")
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	var env Envelope
	if err := dec.Decode(&env); err != nil {
		return Envelope{}, formatJSONDecodeError(err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return Envelope{}, fmt.Errorf("JSON 末尾存在多余内容")
	}
	if env.Format != EnvelopeFormat {
		return Envelope{}, fmt.Errorf("不支持的格式版本（format 须为 %d）", EnvelopeFormat)
	}
	env.Meta.Normalize()
	if err := env.Meta.Validate(); err != nil {
		return Envelope{}, err
	}
	env.Source.Normalize()
	return env, nil
}

// UnmarshalEnvelope 解析单个 JSON 外壳并完整校验来源定义。
func UnmarshalEnvelope(data []byte) (Envelope, error) {
	env, err := DecodeSingleEnvelope(data)
	if err != nil {
		return Envelope{}, err
	}
	if err := env.Source.Validate(); err != nil {
		return Envelope{}, err
	}
	return env, nil
}

// DecodeEnvelopes 解析单个外壳或外壳数组，校验外壳结构与元信息，不调用 Source.Validate。
func DecodeEnvelopes(data []byte) ([]Envelope, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("JSON 内容为空")
	}
	if len(trimmed) > MaxImportBytes {
		return nil, fmt.Errorf("JSON 内容超过 %d KB", MaxImportBytes>>10)
	}
	if trimmed[0] != '[' {
		env, err := DecodeSingleEnvelope(trimmed)
		if err != nil {
			return nil, err
		}
		return []Envelope{env}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	var raws []json.RawMessage
	if err := dec.Decode(&raws); err != nil {
		return nil, formatJSONDecodeError(err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("JSON 末尾存在多余内容")
	}
	if len(raws) == 0 {
		return nil, fmt.Errorf("导入列表为空")
	}
	if len(raws) > MaxImportItems {
		return nil, fmt.Errorf("单次最多导入 %d 个来源", MaxImportItems)
	}
	out := make([]Envelope, 0, len(raws))
	for i, raw := range raws {
		env, err := DecodeSingleEnvelope(raw)
		if err != nil {
			return nil, fmt.Errorf("第 %d 项：%v", i+1, err)
		}
		out = append(out, env)
	}
	return out, nil
}

// UnmarshalEnvelopes 解析单个外壳或外壳数组，并完整校验每一项来源定义。
func UnmarshalEnvelopes(data []byte) ([]Envelope, error) {
	envs, err := DecodeEnvelopes(data)
	if err != nil {
		return nil, err
	}
	for i := range envs {
		if err := envs[i].Source.Validate(); err != nil {
			if len(envs) == 1 {
				return nil, err
			}
			return nil, fmt.Errorf("第 %d 项（%s）：%w", i+1, envs[i].Source.Key, err)
		}
	}
	return envs, nil
}

func formatJSONDecodeError(err error) error {
	msg := err.Error()
	if rest, ok := strings.CutPrefix(msg, "json: unknown field "); ok {
		return fmt.Errorf("存在未知字段 %s", rest)
	}
	return fmt.Errorf("JSON 解析失败：%v", err)
}

// HashDoc 计算归一化后的来源定义摘要（不含启用开关），用于比对本地定义与导入内容是否一致。
func HashDoc(d Doc) string {
	d.Normalize()
	d.Enabled = nil
	if len(d.Outputs) == 0 {
		d.Outputs = nil
	}
	data, _ := json.Marshal(d)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
