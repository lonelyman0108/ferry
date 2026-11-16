package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/lonelyman0108/ferry/internal/sourcecheck"
)

func runHub(args []string) error {
	if len(args) == 0 {
		printHubUsage(os.Stderr)
		return errors.New("缺少 hub 子命令")
	}
	switch args[0] {
	case "index":
		return runHubIndex(os.Stdout, os.Stderr, args[1:])
	case "-h", "--help", "help":
		printHubUsage(os.Stdout)
		return nil
	default:
		printHubUsage(os.Stderr)
		return fmt.Errorf("未知的 hub 子命令：%s", args[0])
	}
}

func printHubUsage(w io.Writer) {
	fmt.Fprintln(w, "用法：ferry hub index [--check] [仓库目录]")
}

func runHubIndex(stdout, stderr io.Writer, args []string) error {
	fs := flag.NewFlagSet("hub index", flag.ContinueOnError)
	fs.SetOutput(stderr)
	checkOnly := fs.Bool("check", false, "仅检查 index.json 是否与 sources/ 一致，不改写文件")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rootDir := "."
	if fs.NArg() > 0 {
		rootDir = fs.Arg(0)
	}

	idx, expected, err := sourcecheck.BuildHubIndex(rootDir)
	if err != nil {
		fmt.Fprintf(stderr, "✗ 生成索引失败：%v\n", err)
		return err
	}

	indexFile := filepath.Join(rootDir, "index.json")
	if *checkOnly {
		existing, err := os.ReadFile(indexFile)
		if err != nil {
			errMsg := fmt.Errorf("读取 %s 失败（请运行 ferry hub index 生成并一并提交）：%w", indexFile, err)
			fmt.Fprintf(stderr, "✗ %v\n", errMsg)
			return errMsg
		}
		if !bytes.Equal(bytes.TrimSpace(existing), bytes.TrimSpace(expected)) {
			errMsg := errors.New("index.json 与 sources/ 内容不一致，请运行 ferry hub index 更新后一并提交")
			fmt.Fprintf(stderr, "✗ %v\n", errMsg)
			return errMsg
		}
		fmt.Fprintf(stdout, "✓ index.json 校验通过（共 %d 个来源）\n", len(idx.Sources))
		return nil
	}

	if err := os.WriteFile(indexFile, expected, 0o644); err != nil {
		fmt.Fprintf(stderr, "✗ 写入 %s 失败：%v\n", indexFile, err)
		return err
	}
	fmt.Fprintf(stdout, "✓ 已生成 %s（共 %d 个来源）\n", indexFile, len(idx.Sources))
	return nil
}
