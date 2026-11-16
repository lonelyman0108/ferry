package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/lonelyman0108/ferry/internal/sourcecheck"
)

func runSource(args []string) error {
	if len(args) == 0 {
		printSourceUsage(os.Stderr)
		return errors.New("缺少 source 子命令")
	}
	switch args[0] {
	case "check":
		return runSourceCheck(os.Stdout, os.Stderr, args[1:])
	case "-h", "--help", "help":
		printSourceUsage(os.Stdout)
		return nil
	default:
		printSourceUsage(os.Stderr)
		return fmt.Errorf("未知的 source 子命令：%s", args[0])
	}
}

func printSourceUsage(w io.Writer) {
	fmt.Fprintln(w, "用法：ferry source check [--hub] <文件或目录…>")
}

func runSourceCheck(stdout, stderr io.Writer, args []string) error {
	fs := flag.NewFlagSet("source check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	hubMode := fs.Bool("hub", false, "启用仓库检查模式（同时校验目录名、README.md 与 cases.json）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	targets := fs.Args()
	if len(targets) == 0 {
		printSourceUsage(stderr)
		return errors.New("请指定至少一个文件或目录")
	}

	failed := 0
	for _, target := range targets {
		results, err := sourcecheck.CheckTarget(target, *hubMode)
		if err != nil {
			fmt.Fprintf(stderr, "✗ %s：%v\n", target, err)
			failed++
			continue
		}
		for _, tr := range results {
			if tr.Err != nil {
				fmt.Fprintf(stderr, "✗ %s：%v\n", tr.Target, tr.Err)
				failed++
				continue
			}
			if !tr.OK() {
				fmt.Fprintf(stderr, "✗ %s（%s v%d）：检查未通过\n", tr.Target, tr.Key, tr.Version)
				for _, p := range tr.Problems {
					fmt.Fprintf(stderr, "  - 定义错误：%s\n", p.String())
				}
				for _, v := range tr.Report.Violations {
					fmt.Fprintf(stderr, "  - 规则违规：%s\n", v.String())
				}
				for _, w := range tr.Report.Warnings {
					fmt.Fprintf(stderr, "  - 警告：%s\n", w.String())
				}
				failed++
				continue
			}
			fmt.Fprintf(stdout, "✓ %s（%s v%d）：检查通过\n", tr.Target, tr.Key, tr.Version)
			for _, w := range tr.Report.Warnings {
				fmt.Fprintf(stdout, "  - 警告：%s\n", w.String())
			}
		}
	}

	if failed > 0 {
		return fmt.Errorf("共 %d 项未通过检查", failed)
	}
	return nil
}
