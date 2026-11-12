package server

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/lonelyman0108/ferry/internal/store"
)

// audit 写一条审计记录。写失败只记日志，不影响主流程。
func (s *Server) audit(r *http.Request, actor, action, target, detail string) {
	// target 可能来自请求（如登录失败时填的用户名），截到列宽，免得超长时这条记录写不进去。
	entry := store.AuditLog{Actor: actor, Action: action, Target: truncate(target, 128), Detail: detail, IP: clientIP(r), At: s.now()}
	if err := s.store().DB.WithContext(r.Context()).Create(&entry).Error; err != nil {
		slog.Error("写审计日志失败", "action", action, "err", err)
	}
}

// handleListAudit 按时间倒序分页：limit 默认 50、最多 200；before 是上一页最后一条的 ID；category 按类别筛选。
func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			writeError(w, http.StatusBadRequest, "limit 应为 1 到 200")
			return
		}
		limit = n
	}
	db := s.store().DB.WithContext(r.Context())
	q := db.Order("id DESC").Limit(limit)
	if c := r.URL.Query().Get("category"); c != "" {
		var ok bool
		if q, ok = filterAudit(q, c); !ok {
			writeError(w, http.StatusBadRequest, "category 不合法")
			return
		}
	}
	if v := r.URL.Query().Get("before"); v != "" {
		before, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "before 不合法")
			return
		}
		q = q.Where("id < ?", before)
	}
	var logs []store.AuditLog
	if err := q.Find(&logs).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	out, err := viewAudit(db, logs)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
