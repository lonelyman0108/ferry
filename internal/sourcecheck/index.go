package sourcecheck

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lonelyman0108/ferry/internal/source"
)

// HubIndex 是仓库根目录 index.json 的结构。
type HubIndex struct {
	Format  int            `json:"format"`
	Sources []HubIndexItem `json:"sources"`
}

// HubIndexItem 是 index.json 中单个来源的摘要条目。
type HubIndexItem struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Icon        string   `json:"icon,omitempty"`
	Version     int      `json:"version"`
	Author      string   `json:"author,omitempty"`
	Homepage    string   `json:"homepage,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Path        string   `json:"path"`
	SHA256      string   `json:"sha256"`
}

// FindHubIconFile 查找并校验来源目录下的 icon.* 图标文件，返回文件名（如 icon.svg）与校验后的 data URI。
func FindHubIconFile(dir string) (string, string, []Issue) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", "", nil
	}
	var iconFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		lower := strings.ToLower(name)
		if lower == "icon" || strings.HasPrefix(lower, "icon.") {
			iconFiles = append(iconFiles, name)
		}
	}
	sort.Strings(iconFiles)
	if len(iconFiles) == 0 {
		return "", "", nil
	}
	if len(iconFiles) > 1 {
		return "", "", []Issue{{
			Code:    CodeInvalidIconFile,
			Path:    strings.Join(iconFiles, ", "),
			Message: "来源目录下存在多个 icon.* 文件，请仅保留一个图标文件",
		}}
	}

	name := iconFiles[0]
	mime, ok := source.IconMIMEByExt(filepath.Ext(name))
	if !ok {
		return name, "", []Issue{{
			Code:    CodeInvalidIconFile,
			Path:    name,
			Message: "图标文件仅支持 icon.svg、icon.png、icon.webp、icon.jpg 或 icon.jpeg",
		}}
	}
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return name, "", []Issue{{
			Code:    CodeInvalidIconFile,
			Path:    name,
			Message: fmt.Sprintf("读取图标文件失败：%v", err),
		}}
	}
	dataURI, err := source.EncodeIconBytes(mime, raw)
	if err != nil {
		return name, "", []Issue{{
			Code:    CodeInvalidIconFile,
			Path:    name,
			Message: fmt.Sprintf("图标文件无效：%v", err),
		}}
	}
	return name, dataURI, nil
}

// BuildHubIndex 扫描仓库根目录下的 sources/* 子目录，校验每个来源并生成 index.json 内容。
func BuildHubIndex(rootDir string) (HubIndex, []byte, error) {
	sourcesDir := filepath.Join(rootDir, "sources")
	entries, err := os.ReadDir(sourcesDir)
	if err != nil {
		return HubIndex{}, nil, fmt.Errorf("读取 %s 失败：%v", sourcesDir, err)
	}

	var dirNames []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dirNames = append(dirNames, e.Name())
	}
	sort.Strings(dirNames)

	idx := HubIndex{
		Format:  source.EnvelopeFormat,
		Sources: make([]HubIndexItem, 0, len(dirNames)),
	}

	for _, name := range dirNames {
		subDir := filepath.Join(sourcesDir, name)
		results, err := CheckTarget(subDir, true)
		if err != nil {
			return HubIndex{}, nil, err
		}
		for _, r := range results {
			if !r.OK() {
				if r.Err != nil {
					return HubIndex{}, nil, fmt.Errorf("%s 校验未通过：%v", r.Target, r.Err)
				}
				if len(r.Problems) > 0 {
					return HubIndex{}, nil, fmt.Errorf("%s 定义有误：%s", r.Target, r.Problems[0].String())
				}
				if len(r.Report.Violations) > 0 {
					return HubIndex{}, nil, fmt.Errorf("%s 违反规则：%s", r.Target, r.Report.Violations[0].String())
				}
			}
		}

		sourceFile := filepath.Join(subDir, "source.json")
		raw, err := os.ReadFile(sourceFile)
		if err != nil {
			return HubIndex{}, nil, fmt.Errorf("读取 %s 失败：%v", sourceFile, err)
		}
		env, err := source.DecodeSingleEnvelope(raw)
		if err != nil {
			return HubIndex{}, nil, fmt.Errorf("解析 %s 失败：%v", sourceFile, err)
		}
		env.Source.Normalize()
		sum := sha256.Sum256(raw)

		iconRel := ""
		if iconName, _, _ := FindHubIconFile(subDir); iconName != "" {
			iconRel = filepath.ToSlash(filepath.Join("sources", name, iconName))
		}

		idx.Sources = append(idx.Sources, HubIndexItem{
			Key:         env.Source.Key,
			Name:        env.Source.Name,
			Description: env.Source.Description,
			Icon:        iconRel,
			Version:     env.Meta.Version,
			Author:      env.Meta.Author,
			Homepage:    env.Meta.Homepage,
			Tags:        env.Meta.Tags,
			Path:        filepath.ToSlash(filepath.Join("sources", name, "source.json")),
			SHA256:      hex.EncodeToString(sum[:]),
		})
	}

	out, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return HubIndex{}, nil, err
	}
	out = append(out, '\n')
	return idx, out, nil
}
