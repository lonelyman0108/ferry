// Ferry 个人登录态同步工具。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lonelyman0108/ferry/internal/buildinfo"
	"github.com/lonelyman0108/ferry/internal/config"
	"github.com/lonelyman0108/ferry/internal/server"
	"github.com/lonelyman0108/ferry/internal/store"
)

const usage = `用法：
  ferry serve [-config ferry.yaml] [-addr 地址]   启动服务
  ferry version               输出构建信息`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		if err := serve(os.Args[2:]); err != nil {
			slog.Error("服务退出", "err", err)
			os.Exit(1)
		}
	case "version":
		fmt.Println(buildinfo.String())
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	cfgPath := fs.String("config", envOr("FERRY_CONFIG", "ferry.yaml"), "配置文件路径，也可用 FERRY_CONFIG 指定")
	addr := fs.String("addr", "", "监听地址，覆盖配置里的 listen")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*cfgPath)
	var st *store.Store
	switch {
	case errors.Is(err, config.ErrNotInitialized):
	case err != nil:
		return err
	default:
		if st, err = store.Open(cfg.Database); err != nil {
			return err
		}
		defer st.Close()
		if err := st.Migrate(); err != nil {
			return fmt.Errorf("迁移数据库：%w", err)
		}
		slog.Info("数据库就绪", "driver", cfg.Database.Driver)
	}
	if *addr == "" {
		*addr = cfg.Listen
	}

	app, err := server.New(server.Options{ConfigPath: *cfgPath, Config: cfg, Store: st})
	if err != nil {
		return err
	}
	defer app.Close()
	if token := app.SetupToken(); token != "" {
		slog.Warn("尚未初始化：打开管理界面完成初始化向导，要用到下面的初始化令牌（每次启动都会变）",
			"config", *cfgPath, "setup_token", token)
	}

	srv := &http.Server{Addr: *addr, Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go app.RunWorkers(ctx)

	errc := make(chan error, 1)
	go func() {
		slog.Info("开始监听", "addr", *addr, "version", buildinfo.Version)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
