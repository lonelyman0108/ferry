package server

import (
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/lonelyman0108/ferry/internal/events"
	"github.com/lonelyman0108/ferry/internal/store"
)

// 审计日志的筛选类别。
const (
	auditLogin  = "login"
	auditConfig = "config"
	auditSync   = "sync"
	auditPush   = "push"
	auditNotify = "notify"
	auditReveal = "reveal"
)

// 审计记录的结果。修改配置这类没有成败之分的操作（记下来就是做成了）为空。
const (
	outcomeSuccess = "success"
	outcomeFailure = "failure"
	outcomeWarning = "warning"
)

type auditKind struct{ label, category string }

// auditKinds 是各审计动作的中文名和类别。发送通知的动作是 notify.<事件>，见 kindOf。
var auditKinds = map[string]auditKind{
	"setup.complete":        {"完成初始化", auditLogin},
	"auth.login":            {"登录", auditLogin},
	"auth.login_failed":     {"登录失败", auditLogin},
	"auth.logout":           {"退出登录", auditLogin},
	"auth.password_changed": {"修改密码", auditLogin},

	"source.create":                     {"新建来源", auditConfig},
	"source.import":                     {"导入来源", auditConfig},
	"source.import_example":             {"导入示例来源", auditConfig},
	"source.update":                     {"修改来源", auditConfig},
	"source.delete":                     {"删除来源", auditConfig},
	"endpoint.create":                   {"新建访问地址", auditConfig},
	"endpoint.update":                   {"修改访问地址", auditConfig},
	"endpoint.delete":                   {"删除访问地址", auditConfig},
	"subscription.create":               {"新建订阅", auditConfig},
	"subscription.update":               {"修改订阅", auditConfig},
	"subscription.delete":               {"删除订阅", auditConfig},
	"subscription.rotate":               {"更换订阅令牌", auditConfig},
	"subscription.transport_mode":       {"切换传输方式", auditConfig},
	"subscription.transport_key_rotate": {"更换传输密钥", auditConfig},
	"destination.create":                {"新建目的地", auditConfig},
	"destination.update":                {"修改目的地", auditConfig},
	"destination.delete":                {"删除目的地", auditConfig},
	"binding.create":                    {"新建推送规则", auditConfig},
	"binding.update":                    {"修改推送规则", auditConfig},
	"binding.delete":                    {"删除推送规则", auditConfig},
	"channel.create":                    {"新建通知渠道", auditConfig},
	"channel.update":                    {"修改通知渠道", auditConfig},
	"channel.delete":                    {"删除通知渠道", auditConfig},
	"notify_rule.create":                {"新建通知规则", auditConfig},
	"notify_rule.update":                {"修改通知规则", auditConfig},
	"notify_rule.delete":                {"删除通知规则", auditConfig},
	"token.create":                      {"新建 API 令牌", auditConfig},
	"token.revoke":                      {"吊销 API 令牌", auditConfig},

	"sync.accepted":            {"回传新值", auditSync},
	"sync.rejected":            {"回传被驳回", auditSync},
	"transport.decrypt_failed": {"回传解密失败", auditSync},
	"batch.process":            {"立即处理批次", auditSync},
	"version.approve":          {"审核通过", auditSync},
	"version.reject":           {"驳回新值", auditSync},
	"version.activate":         {"恢复旧值", auditSync},

	"push.manual":      {"手动推送", auditPush},
	"push.retry":       {"重试推送", auditPush},
	"destination.test": {"测试目的地", auditPush},

	"channel.test": {"测试通知渠道", auditNotify},

	"version.reveal":                    {"查看原值", auditReveal},
	"subscription.transport_key_reveal": {"查看传输密钥", auditReveal},
}

// eventLabels 和界面上通知规则里的事件名一致。
var eventLabels = map[string]string{
	events.IdentityNew:        "新身份",
	events.ValueUpdated:       "登录态已更新",
	events.ValueRejected:      "新值被驳回",
	events.ValuePendingReview: "新值待审核",
	events.PushFailed:         "推送失败",
	events.ValueExpiring:      "登录态即将过期",
	events.MitmChanged:        "订阅待更新",
}

const notifyPrefix = "notify."

func kindOf(action string) auditKind {
	if k, ok := auditKinds[action]; ok {
		return k
	}
	if ev, ok := strings.CutPrefix(action, notifyPrefix); ok {
		if l, ok := eventLabels[ev]; ok {
			ev = l
		}
		return auditKind{"发送通知：" + ev, auditNotify}
	}
	return auditKind{label: action}
}

// filterAudit 按类别筛选，类别不存在时返回 false。
func filterAudit(q *gorm.DB, category string) (*gorm.DB, bool) {
	var actions []string
	for a, k := range auditKinds {
		if k.category == category {
			actions = append(actions, a)
		}
	}
	if len(actions) == 0 {
		return q, false
	}
	sort.Strings(actions)
	if category == auditNotify {
		return q.Where("(action IN ? OR action LIKE ?)", actions, notifyPrefix+"%"), true
	}
	return q.Where("action IN ?", actions), true
}

// outcomeOf 从详情里读出成败；详情的格式由写这条审计的地方决定。
func outcomeOf(a store.AuditLog) string {
	switch {
	case a.Action == "auth.login":
		return outcomeSuccess
	case a.Action == "auth.login_failed", a.Action == "transport.decrypt_failed":
		return outcomeFailure
	case a.Action == "sync.rejected":
		return outcomeWarning
	case a.Action == "sync.accepted":
		if strings.Contains(a.Detail, "未通过校验") {
			return outcomeWarning
		}
		return outcomeSuccess
	case a.Action == "destination.test", a.Action == "channel.test":
		if a.Detail == "true" {
			return outcomeSuccess
		}
		return outcomeFailure
	case a.Action == "push.manual", a.Action == "push.retry":
		switch a.Detail {
		case store.PushSuccess:
			return outcomeSuccess
		case store.PushFailed:
			return outcomeFailure
		case store.PushQueued:
			return outcomeWarning
		}
	case strings.HasPrefix(a.Action, notifyPrefix):
		if a.Detail == "ok" {
			return outcomeSuccess
		}
		return outcomeFailure
	}
	return ""
}

// auditView 是给界面看的审计记录：在原始记录上补上动作的中文名、类别、结果，以及操作者和对象的名称。
type auditView struct {
	store.AuditLog
	ActionLabel string `json:"action_label"`
	Category    string `json:"category"`
	Outcome     string `json:"outcome"`
	// ActorKind 是 user、token、subscription、system、anonymous 之一。
	ActorKind  string `json:"actor_kind"`
	ActorName  string `json:"actor_name"`
	TargetName string `json:"target_name"`
}

// auditNames 是一页审计记录里引用到的对象名称。对象删掉以后查不到，显示编号。
type auditNames struct {
	users, tokens, subs, sourceByID, dests, fields map[uint]string
	sourceByKey                                    map[string]string
	bindings, versions, batches                    map[uint]string
}

func idOf(ref, prefix string) (uint, bool) {
	rest, ok := strings.CutPrefix(ref, prefix)
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseUint(rest, 10, 64)
	return uint(n), err == nil
}

func loadAuditNames(db *gorm.DB, logs []store.AuditLog) (auditNames, error) {
	n := auditNames{
		users: map[uint]string{}, tokens: map[uint]string{}, subs: map[uint]string{}, sourceByID: map[uint]string{},
		dests: map[uint]string{}, fields: map[uint]string{}, sourceByKey: map[string]string{},
		bindings: map[uint]string{}, versions: map[uint]string{}, batches: map[uint]string{},
	}
	var versionIDs, batchIDs []uint
	for _, l := range logs {
		if id, ok := idOf(l.Target, "version:"); ok {
			versionIDs = append(versionIDs, id)
		}
		if id, ok := idOf(l.Target, "batch:"); ok {
			batchIDs = append(batchIDs, id)
		}
	}

	// 用户、令牌、订阅、来源、目的地、字段、推送规则都不多，整表读出来。
	// 列名按切片传，GORM 才会加引号：key 在 MySQL 里是保留字。
	var users []store.User
	var tokens []store.APIToken
	var subs []store.Subscription
	var sources []store.Source
	var dests []store.Destination
	var fields []store.Field
	var bindings []store.Binding
	for _, q := range []struct {
		dest any
		cols []string
	}{
		{&users, []string{"id", "username"}}, {&tokens, []string{"id", "name"}}, {&subs, []string{"id", "name"}},
		{&sources, []string{"id", "key", "name"}}, {&dests, []string{"id", "name"}}, {&fields, []string{"id", "key", "name"}},
		{&bindings, []string{"id", "source_id", "destination_id"}},
	} {
		if err := db.Select(q.cols).Find(q.dest).Error; err != nil {
			return n, err
		}
	}
	for _, u := range users {
		n.users[u.ID] = u.Username
	}
	for _, t := range tokens {
		n.tokens[t.ID] = t.Name
	}
	for _, s := range subs {
		n.subs[s.ID] = s.Name
	}
	for _, s := range sources {
		n.sourceByID[s.ID], n.sourceByKey[s.Key] = s.Name, s.Name
	}
	for _, d := range dests {
		n.dests[d.ID] = d.Name
	}
	for _, f := range fields {
		n.fields[f.ID] = fieldLabel(f)
	}
	for _, b := range bindings {
		n.bindings[b.ID] = n.source(b.SourceID) + " → " + nameOr(n.dests, b.DestinationID)
	}
	if len(versionIDs) > 0 {
		var vs []store.Version
		if err := db.Select([]string{"id", "source_id", "field_id"}).Where("id IN ?", versionIDs).Find(&vs).Error; err != nil {
			return n, err
		}
		for _, v := range vs {
			n.versions[v.ID] = n.source(v.SourceID) + " · " + nameOr(n.fields, v.FieldID)
		}
	}
	if len(batchIDs) > 0 {
		var bs []store.Batch
		if err := db.Select([]string{"id", "source_id"}).Where("id IN ?", batchIDs).Find(&bs).Error; err != nil {
			return n, err
		}
		for _, b := range bs {
			n.batches[b.ID] = n.source(b.SourceID)
		}
	}
	return n, nil
}

// fieldLabel 是字段给人看的名字：有名称用名称，没有用标识。
func fieldLabel(f store.Field) string {
	if f.Name != "" {
		return f.Name
	}
	return f.Key
}

func nameOr(m map[uint]string, id uint) string {
	if v, ok := m[id]; ok {
		return v
	}
	return "#" + strconv.FormatUint(uint64(id), 10)
}

func (n auditNames) source(id uint) string { return nameOr(n.sourceByID, id) }

func (n auditNames) actor(actor string) (kind, name string) {
	switch actor {
	case "system":
		return "system", "系统"
	case "anonymous":
		return "anonymous", "未登录"
	}
	for _, k := range []struct {
		prefix, kind string
		names        map[uint]string
	}{{"user:", "user", n.users}, {"token:", "token", n.tokens}, {"subscription:", "subscription", n.subs}} {
		if id, ok := idOf(actor, k.prefix); ok {
			return k.kind, nameOr(k.names, id)
		}
	}
	return "", actor
}

func (n auditNames) target(target string) string {
	if id, ok := idOf(target, "token:"); ok {
		return nameOr(n.tokens, id)
	}
	if id, ok := idOf(target, "binding:"); ok {
		return nameOr(n.bindings, id)
	}
	if id, ok := idOf(target, "version:"); ok {
		return nameOr(n.versions, id)
	}
	if id, ok := idOf(target, "batch:"); ok {
		return nameOr(n.batches, id)
	}
	if key, ok := strings.CutPrefix(target, "source:"); ok {
		if name, ok := n.sourceByKey[key]; ok {
			return name
		}
		return key
	}
	if kid, ok := strings.CutPrefix(target, "kid:"); ok {
		return "密钥 " + kid
	}
	// 其余的对象（用户名、访问地址、订阅、目的地、渠道、通知规则）写入时就记的是名称。
	if _, name, ok := strings.Cut(target, ":"); ok {
		return name
	}
	return target
}

func viewAudit(db *gorm.DB, logs []store.AuditLog) ([]auditView, error) {
	names, err := loadAuditNames(db, logs)
	if err != nil {
		return nil, err
	}
	out := make([]auditView, 0, len(logs))
	for _, l := range logs {
		k := kindOf(l.Action)
		v := auditView{AuditLog: l, ActionLabel: k.label, Category: k.category, Outcome: outcomeOf(l), TargetName: names.target(l.Target)}
		v.ActorKind, v.ActorName = names.actor(l.Actor)
		out = append(out, v)
	}
	return out, nil
}
