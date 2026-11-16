package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/lonelyman0108/ferry/internal/ghnet"
)

type networkSettingsView struct {
	ghnet.Config
	Presets []ghnet.MirrorPreset `json:"presets"`
}

type networkTestResult struct {
	OK        bool   `json:"ok"`
	LatencyMs int64  `json:"latency_ms"`
	Target    string `json:"target"`
	Error     string `json:"error,omitempty"`
}

func (s *Server) githubClient(ctx context.Context, timeout time.Duration) (*http.Client, error) {
	box, _ := s.box()
	cfg, err := ghnet.LoadConfig(ctx, s.store().DB, box)
	if err != nil {
		cfg = ghnet.Config{Mode: ghnet.ModeDirect}
	}
	var baseRT http.RoundTripper
	if s.HTTP != nil {
		baseRT = s.HTTP.Transport
	}
	return ghnet.NewHTTPClient(cfg, timeout, baseRT)
}

func (s *Server) handleGetNetworkSettings(w http.ResponseWriter, r *http.Request) {
	box, err := s.box()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	cfg, err := ghnet.LoadConfig(r.Context(), s.store().DB, box)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, networkSettingsView{
		Config:  cfg.Masked(),
		Presets: ghnet.Presets,
	})
}

func (s *Server) handleUpdateNetworkSettings(w http.ResponseWriter, r *http.Request) {
	var in ghnet.Config
	if !decodeJSON(w, r, &in) {
		return
	}
	box, err := s.box()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	stored, err := ghnet.LoadConfig(r.Context(), s.store().DB, box)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	in.ProxyURL = ghnet.ResolveProxyURL(in.ProxyURL, stored.ProxyURL)
	if err := ghnet.SaveConfig(r.Context(), s.store().DB, box, in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.updateMu.Lock()
	s.update = nil
	s.updateMu.Unlock()

	s.audit(r, identityFrom(r.Context()).actor(), "settings.network_update", "settings:GitHub 网络", in.Mode)
	writeJSON(w, http.StatusOK, networkSettingsView{
		Config:  in.Masked(),
		Presets: ghnet.Presets,
	})
}

func (s *Server) handleTestNetworkSettings(w http.ResponseWriter, r *http.Request) {
	var in ghnet.Config
	if !decodeJSON(w, r, &in) {
		return
	}
	box, err := s.box()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	stored, err := ghnet.LoadConfig(r.Context(), s.store().DB, box)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	in.ProxyURL = ghnet.ResolveProxyURL(in.ProxyURL, stored.ProxyURL)
	if err := in.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var baseRT http.RoundTripper
	if s.HTTP != nil {
		baseRT = s.HTTP.Transport
	}
	client, err := ghnet.NewHTTPClient(in, 8*time.Second, baseRT)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	target := s.networkTestURL
	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	res := networkTestResult{
		LatencyMs: latency,
		Target:    target,
	}
	if err != nil {
		res.Error = fmt.Sprintf("测试连接失败：%v", err)
		writeJSON(w, http.StatusOK, res)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		res.Error = fmt.Sprintf("测试连接失败：HTTP %d", resp.StatusCode)
		writeJSON(w, http.StatusOK, res)
		return
	}
	res.OK = true
	writeJSON(w, http.StatusOK, res)
}
