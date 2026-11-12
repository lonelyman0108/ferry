package server

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"regexp"
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
			writeError(w, http.StatusConflict, "已完成初始化，无法再次进入初始化向导")
			return
		}
		if !auth.Equal(r.Header.Get(setupTokenHeader), token) {
			writeError(w, http.StatusForbidden, "初始化令牌无效，请查看服务启动日志")
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

// resolve 把向导回填的打码连接串换回原值：驱动没变、连接串和打码后的一模一样，说明用户没改。
func (s *Server) resolve(d databaseInput) config.Database {
	db := d.config()
	s.mu.RLock()
	pre := s.cfg.Database
	s.mu.RUnlock()
	if pre.DSN != "" && db.Driver == pre.Driver && db.DSN == maskDSN(pre.DSN) {
		db.DSN = pre.DSN
	}
	return db
}

var (
	// URL 写法 scheme://user:pass@host 与 go-sql-driver 写法 user:pass@tcp(host) 里的密码
	dsnURLPassword   = regexp.MustCompile(`(://[^:/@]*):[^@/]*@`)
	dsnMySQLPassword = regexp.MustCompile(`^([^:/@]*):[^@]*@`)
	// key=value 写法里的 password=...
	dsnKVPassword = regexp.MustCompile(`(?i)(password=)('[^']*'|\S+)`)
)

// maskDSN 把连接串里的密码换成 ••••，回填给向导时用，密码本身不发给浏览器。
func maskDSN(dsn string) string {
	if strings.Contains(dsn, "://") {
		return dsnURLPassword.ReplaceAllString(dsn, "$1:••••@")
	}
	dsn = dsnMySQLPassword.ReplaceAllString(dsn, "$1:••••@")
	return dsnKVPassword.ReplaceAllString(dsn, "${1}••••")
}

// setupPreset 是向导回填用的已有配置：来自配置文件或环境变量的数据库与主密钥。
type setupPreset struct {
	Driver     string        `json:"driver"`
	DSN        string        `json:"dsn"`
	DriverFrom config.Source `json:"driver_from"`
	DSNFrom    config.Source `json:"dsn_from"`
	// MasterKey 只说明有没有、从哪里来，不给值。
	MasterKey     bool          `json:"master_key"`
	MasterKeyFrom config.Source `json:"master_key_from"`
	ConfigPath    string        `json:"config_path"`
}

// handleSetupPreset 返回回填信息。凭向导令牌才能看，连接串里的密码打码。
func (s *Server) handleSetupPreset(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	c := s.cfg
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, setupPreset{
		Driver: c.Database.Driver, DSN: maskDSN(c.Database.DSN),
		DriverFrom: c.Sources.Driver, DSNFrom: c.Sources.DSN,
		MasterKey: c.MasterKey != "", MasterKeyFrom: c.Sources.MasterKey,
		ConfigPath: s.configPath,
	})
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
	st, err := store.Open(s.resolve(in))
	if err == nil {
		err = st.Ping()
		_ = st.Close()
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "连接数据库失败："+err.Error())
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
		writeError(w, http.StatusConflict, "已完成初始化，无法再次进入初始化向导")
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
		// 已经由配置文件或环境变量给出主密钥时沿用它，不再生成，也不在响应里返回。
		newCfg = &config.Config{
			Listen:    s.cfg.Listen,
			Database:  s.resolve(*in.Database),
			MasterKey: s.cfg.MasterKey,
			Sources:   s.cfg.Sources,
		}
		generated := newCfg.MasterKey == ""
		if generated {
			key, err := secret.GenerateKey()
			if err != nil {
				s.internalError(w, r, err)
				return
			}
			newCfg.MasterKey = base64.StdEncoding.EncodeToString(key)
			newCfg.Sources.MasterKey = config.Source{From: config.SourceFile}
		}
		if err := newCfg.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if st, err = openAndMigrate(newCfg.Database); err != nil {
			writeError(w, http.StatusBadRequest, "连接数据库失败："+err.Error())
			return
		}
		// 先落盘配置、再建管理员：建管理员失败时，状态退回“有配置、缺管理员”，
		// 重新提交向导即可，不会出现库里有管理员却没有配置文件的情况。
		if err := newCfg.Save(s.configPath); err != nil {
			_ = st.Close()
			s.internalError(w, r, err)
			return
		}
		newCfg.Sources.Driver = config.Source{From: config.SourceFile}
		newCfg.Sources.DSN = config.Source{From: config.SourceFile}
		s.mu.Lock()
		s.cfg, s.st, s.ownsStore = newCfg, st, true
		s.mu.Unlock()
		if generated {
			result.MasterKey = newCfg.MasterKey
		}
		result.ConfigPath = s.configPath
	} else if in.Database != nil {
		writeError(w, http.StatusBadRequest, "数据库已在配置中指定，无法在初始化向导中修改")
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
		writeError(w, http.StatusConflict, "已完成初始化，无法再次进入初始化向导")
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

var errAlreadyInitialized = errors.New("已完成初始化")
