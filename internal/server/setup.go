package server

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/lonelyman0108/ferry/internal/auth"
	"github.com/lonelyman0108/ferry/internal/config"
	"github.com/lonelyman0108/ferry/internal/secret"
	"github.com/lonelyman0108/ferry/internal/store"
)

// setupTokenHeader 是初始化向导令牌所在的请求头。
const setupTokenHeader = "X-Ferry-Setup-Token"

type setupState struct {
	// NeedsConfig 表示还没有数据库和主密钥，要走完整向导。
	NeedsConfig bool `json:"needs_config"`
	// NeedsAdmin 表示还没有管理员；配置由环境变量给出时只需要这一步。
	NeedsAdmin bool `json:"needs_admin"`
}

func (s *Server) setupState(ctx context.Context) (setupState, error) {
	st := s.store()
	if st == nil {
		return setupState{NeedsConfig: true, NeedsAdmin: true}, nil
	}
	var n int64
	if err := st.DB.WithContext(ctx).Model(&store.User{}).Count(&n).Error; err != nil {
		return setupState{}, err
	}
	return setupState{NeedsAdmin: n == 0}, nil
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.setupState(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// requireSetupToken 只在需要初始化时放行，并要求带上启动日志里打印的向导令牌。
func (s *Server) requireSetupToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := s.SetupToken()
		if token == "" {
			writeError(w, http.StatusConflict, "已经初始化，不能再次进入向导")
			return
		}
		if !auth.Equal(r.Header.Get(setupTokenHeader), token) {
			writeError(w, http.StatusForbidden, "初始化令牌无效，见服务启动日志")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type databaseInput struct {
	Driver string `json:"driver"`
	DSN    string `json:"dsn"`
}

func (d databaseInput) config() config.Database {
	return config.Database{Driver: strings.TrimSpace(d.Driver), DSN: strings.TrimSpace(d.DSN)}
}

// openAndMigrate 连接数据库、测连通并建表；失败时关闭连接。
func openAndMigrate(db config.Database) (*store.Store, error) {
	st, err := store.Open(db)
	if err != nil {
		return nil, err
	}
	if err := st.Ping(); err != nil {
		_ = st.Close()
		return nil, err
	}
	if err := st.Migrate(); err != nil {
		_ = st.Close()
		return nil, err
	}
	return st, nil
}

func (s *Server) handleSetupTestDatabase(w http.ResponseWriter, r *http.Request) {
	var in databaseInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if s.store() != nil {
		writeError(w, http.StatusConflict, "数据库已配置")
		return
	}
	st, err := store.Open(in.config())
	if err == nil {
		err = st.Ping()
		_ = st.Close()
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "连不上数据库："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type setupInput struct {
	Database *databaseInput `json:"database"`
	// Endpoint 是可选的第一个访问地址，订阅里手机用它访问平台；以后也可以在设置里加。
	Endpoint *endpointInput `json:"endpoint"`
	Admin    struct {
		Username string `json:"username"`
		Password string `json:"password"`
	} `json:"admin"`
}

type setupResult struct {
	// MasterKey 只在这次响应里出现一次，丢失后敏感字段无法解密。
	MasterKey  string `json:"master_key,omitempty"`
	ConfigPath string `json:"config_path,omitempty"`
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	var in setupInput
	if !decodeJSON(w, r, &in) {
		return
	}
	username := strings.TrimSpace(in.Admin.Username)
	if username == "" || len(username) > 64 {
		writeError(w, http.StatusBadRequest, "管理员用户名不能为空，且不超过 64 个字节")
		return
	}
	hash, err := auth.HashPassword(in.Admin.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var endpoint *store.Endpoint
	if in.Endpoint != nil {
		e, err := in.Endpoint.model()
		if err != nil {
			writeError(w, http.StatusBadRequest, "访问地址："+err.Error())
			return
		}
		endpoint = &e
	}

	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	state, err := s.setupState(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if !state.NeedsConfig && !state.NeedsAdmin {
		writeError(w, http.StatusConflict, "已经初始化，不能再次进入向导")
		return
	}

	var result setupResult
	st := s.store()
	var newCfg *config.Config
	if state.NeedsConfig {
		if in.Database == nil {
			writeError(w, http.StatusBadRequest, "缺少数据库配置")
			return
		}
		key, err := secret.GenerateKey()
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		newCfg = &config.Config{
			Listen:    s.cfg.Listen,
			Database:  in.Database.config(),
			MasterKey: base64.StdEncoding.EncodeToString(key),
		}
		if err := newCfg.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if st, err = openAndMigrate(newCfg.Database); err != nil {
			writeError(w, http.StatusBadRequest, "连不上数据库："+err.Error())
			return
		}
		// 先落盘配置、再建管理员：建管理员失败时，状态退回“有配置、缺管理员”，
		// 重新提交向导即可，不会出现库里有管理员却没有配置文件的情况。
		if err := newCfg.Save(s.configPath); err != nil {
			_ = st.Close()
			s.internalError(w, r, err)
			return
		}
		s.mu.Lock()
		s.cfg, s.st, s.ownsStore = newCfg, st, true
		s.mu.Unlock()
		result.MasterKey = newCfg.MasterKey
		result.ConfigPath = s.configPath
	} else if in.Database != nil {
		writeError(w, http.StatusBadRequest, "数据库已经由配置给出，不能在向导里修改")
		return
	}

	now := s.now()
	err = st.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&store.User{}).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return errAlreadyInitialized
		}
		u := store.User{Username: username, PasswordHash: hash}
		if err := tx.Create(&u).Error; err != nil {
			return err
		}
		if err := tx.Save(&store.Setting{Key: "setup.completed_at", Value: now.UTC().Format(time.RFC3339)}).Error; err != nil {
			return err
		}
		if endpoint != nil {
			if err := tx.Create(endpoint).Error; err != nil {
				return err
			}
		}
		return tx.Create(&store.AuditLog{
			Actor: actorUser(u.ID), Action: "setup.complete", Target: "user:" + u.Username, IP: clientIP(r), At: now,
		}).Error
	})
	if errors.Is(err, errAlreadyInitialized) {
		writeError(w, http.StatusConflict, "已经初始化，不能再次进入向导")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}

	s.mu.Lock()
	s.setupToken = ""
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, result)
}

var errAlreadyInitialized = errors.New("已经初始化")
