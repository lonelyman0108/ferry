package store

import "time"

// User 是管理员账号。目前只有一个管理员，表结构允许以后加人。
type User struct {
	ID           uint   `gorm:"primaryKey"`
	Username     string `gorm:"size:64;uniqueIndex;not null"`
	PasswordHash string `gorm:"size:255;not null"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Session 是管理员的登录会话。ID 是 Cookie 里令牌的 SHA-256，数据库里不存原文。
type Session struct {
	ID        string    `gorm:"primaryKey;size:64"`
	UserID    uint      `gorm:"index;not null"`
	User      User      `gorm:"constraint:OnDelete:CASCADE"`
	ExpiresAt time.Time `gorm:"index;not null"`
	// LastSeenAt 建会话时就要写入；MySQL 严格模式不接受零值时间。
	LastSeenAt time.Time `gorm:"not null"`
	IP         string    `gorm:"size:64"`
	UserAgent  string    `gorm:"size:255"`
	CreatedAt  time.Time
}

// APIToken 给自动化调用 API 用，只存哈希；Prefix 是令牌开头几位，方便在界面上辨认。
type APIToken struct {
	ID         uint   `gorm:"primaryKey"`
	Name       string `gorm:"size:64;not null"`
	Prefix     string `gorm:"size:16;not null"`
	TokenHash  string `gorm:"size:64;uniqueIndex;not null"`
	LastUsedAt *time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

// AuditLog 记录谁在什么时候做了什么，敏感值不写进 Detail。
type AuditLog struct {
	ID uint `gorm:"primaryKey" json:"id"`
	// Actor 形如 user:1、token:3、subscription:2、system。
	Actor  string    `gorm:"size:64;index;not null" json:"actor"`
	Action string    `gorm:"size:64;index;not null" json:"action"`
	Target string    `gorm:"size:128" json:"target"`
	Detail string    `gorm:"type:text" json:"detail"`
	IP     string    `gorm:"size:64" json:"ip"`
	At     time.Time `gorm:"index;not null" json:"at"`
}

// Setting 是简单的键值配置，例如初始化完成时间。
type Setting struct {
	Key       string `gorm:"primaryKey;size:64"`
	Value     string `gorm:"type:text;not null"`
	UpdatedAt time.Time
}

// Source 是一个需要同步登录态的 App 或站点。Key 会出现在订阅、回传和目的地备注里，建好后不能改。
type Source struct {
	ID          uint   `gorm:"primaryKey"`
	Key         string `gorm:"size:64;uniqueIndex;not null"`
	Name        string `gorm:"size:128;not null"`
	Description string `gorm:"type:text"`
	Enabled     bool   `gorm:"not null"`
	// IdentityField 是用作身份标识的字段 key；为空时所有回传都归到默认身份。
	IdentityField string `gorm:"size:64"`
	// MergeWindowSec 内到达的字段合并成一次更新。
	MergeWindowSec int `gorm:"not null"`
	// NotifyMode 是手机端通知时机：always、first、changed。
	NotifyMode     string `gorm:"size:16;not null"`
	MinIntervalSec int    `gorm:"not null"`
	Debug          bool   `gorm:"not null"`
	// ReviewMode 为 manual 时新值要人工审核才生效，默认 auto。
	ReviewMode string `gorm:"size:16;not null;default:auto"`
	// Probe 是主动探测配置（JSON），为空表示不探测。
	Probe     string    `gorm:"type:text"`
	Matchers  []Matcher `gorm:"constraint:OnDelete:CASCADE"`
	Fields    []Field   `gorm:"constraint:OnDelete:CASCADE"`
	Outputs   []Output  `gorm:"constraint:OnDelete:CASCADE"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Matcher 是来源下的一条请求匹配规则，Name 在来源内唯一，字段用它来引用。
type Matcher struct {
	ID        uint   `gorm:"primaryKey"`
	SourceID  uint   `gorm:"not null;uniqueIndex:idx_matcher_source_name"`
	Name      string `gorm:"size:64;not null;uniqueIndex:idx_matcher_source_name"`
	Host      string `gorm:"size:255;not null"`
	PathRegex string `gorm:"size:512"`
	Method    string `gorm:"size:16"`
	Position  int    `gorm:"not null"`
}

// Field 是从请求里取的一个原始值。Matcher 是匹配规则的名字，为空表示本来源的任意一条规则。
type Field struct {
	ID        uint   `gorm:"primaryKey"`
	SourceID  uint   `gorm:"not null;uniqueIndex:idx_field_source_key"`
	Key       string `gorm:"size:64;not null;uniqueIndex:idx_field_source_key"`
	Name      string `gorm:"size:128"`
	Matcher   string `gorm:"size:64"`
	In        string `gorm:"size:16;not null"`
	Path      string `gorm:"size:512;not null"`
	RegexOn   string `gorm:"size:8"`
	Type      string `gorm:"size:16;not null"`
	Sensitive bool   `gorm:"not null"`
	MinLen    int    `gorm:"not null"`
	MaxLen    int    `gorm:"not null"`
	Pattern   string `gorm:"size:512"`
	Position  int    `gorm:"not null"`
}

// Output 是推给目的地的变量，由字段经模板拼接而成。
type Output struct {
	ID       uint   `gorm:"primaryKey"`
	SourceID uint   `gorm:"not null;uniqueIndex:idx_output_source_name"`
	Name     string `gorm:"size:128;not null;uniqueIndex:idx_output_source_name"`
	Template string `gorm:"type:text;not null"`
	Position int    `gorm:"not null"`
}

// Endpoint 是手机访问平台用的地址，例如局域网、公网各一个。
type Endpoint struct {
	ID        uint   `gorm:"primaryKey"`
	Name      string `gorm:"size:64;uniqueIndex;not null"`
	BaseURL   string `gorm:"size:255;not null"`
	Note      string `gorm:"size:255"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Subscription 是给某台设备、某个客户端生成的订阅。令牌既存哈希（用来查找），
// 也用主密钥加密存一份原文：界面要能再次显示订阅链接，MITM 变化的通知也要附上安装链接。
type Subscription struct {
	ID          uint   `gorm:"primaryKey"`
	Name        string `gorm:"size:64;uniqueIndex;not null"`
	Client      string `gorm:"size:16;not null"`
	EndpointID  uint   `gorm:"not null;index"`
	FallbackID  *uint
	TokenHash   string `gorm:"size:64;uniqueIndex;not null"`
	TokenPrefix string `gorm:"size:16;not null"`
	TokenSealed string `gorm:"type:text;not null"`
	// AllSources 为 true 时包含全部来源，否则只包含 SourceKeys（JSON 数组）里列出的。
	AllSources    bool   `gorm:"not null"`
	SourceKeys    string `gorm:"type:text;not null"`
	LastFetchedAt *time.Time
	// MitmHash 是上次检查时 MITM 域名集合的哈希，用来发现域名变化。
	MitmHash string `gorm:"size:64"`
	// FileFetchedAt 是客户端最近一次下载订阅文件的时间，按分钟更新；有值说明订阅装到了客户端里。
	FileFetchedAt *time.Time
	// FileMitmHash 是最近一次下载时订阅里 MITM 域名集合的哈希；和现在的不同，说明手机上那份订阅需要更新。
	FileMitmHash string `gorm:"size:64;not null;default:''"`
	// ScriptURL 是订阅里引用的脚本地址；为空时用主访问地址下的 /script/ferry.js。
	// 主地址是 http 时可以改成一个 https 静态地址，免得脚本在下载途中被替换。
	ScriptURL string `gorm:"size:512;not null;default:''"`
	// TransportMode 是手机和平台之间数据通道的传输方式，取值见 TransportPlain、TransportSealed。
	TransportMode string `gorm:"size:16;not null;default:plain"`
	// TransportKID 是传输密钥的公开编号，手机放在请求头里，平台据此找到密钥；从没开过加密时为空。
	TransportKID *string `gorm:"column:transport_kid;size:16;uniqueIndex:idx_subscriptions_transport_kid"`
	// TransportKeySealed 是用主密钥加密的传输密钥。切回明文时保留，好让已加密的手机收到“已改为明文”的加密回复。
	TransportKeySealed string `gorm:"type:text"`
	// TransportVerifiedAt 是最近一次换密钥后第一次成功解开报文的时间，说明手机上的密钥填对了。
	TransportVerifiedAt *time.Time
	// TransportLastAt 是最近一次成功解开报文的时间，按分钟更新。
	TransportLastAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// 订阅的传输方式。
const (
	// TransportPlain 用订阅令牌认证，正文明文，是否加密取决于访问地址是不是 https。
	TransportPlain = "plain"
	// TransportSealed 用预共享密钥加密整个报文（internal/transport），访问地址是 http 时也读不到、改不了。
	TransportSealed = "sealed"
)

// TransportNonce 记录加密通道里见过的请求 nonce，用来拒绝重放；过期后由后台清理。
type TransportNonce struct {
	ID             uint   `gorm:"primaryKey"`
	SubscriptionID uint   `gorm:"not null;uniqueIndex:idx_transport_nonce"`
	Nonce          string `gorm:"size:32;not null;uniqueIndex:idx_transport_nonce"`
	// ExpiresAt 之后这条报文本来就会因时间超窗被拒，记录可以删除。
	ExpiresAt time.Time `gorm:"not null;index"`
}

// Identity 是来源下的一个身份，Key 来自来源的身份字段；没有身份字段时为 default。
type Identity struct {
	ID         uint      `gorm:"primaryKey"`
	SourceID   uint      `gorm:"not null;uniqueIndex:idx_identity_source_key"`
	Key        string    `gorm:"size:255;not null;uniqueIndex:idx_identity_source_key"`
	LastSeenAt time.Time `gorm:"not null"`
	CreatedAt  time.Time
}

// 批次状态。
const (
	BatchCollecting = "collecting"
	BatchDone       = "done"
)

// Batch 把同一订阅在合并窗口内回传的同一来源字段合成一次更新，窗口关闭后统一定身份、校验、生效。
type Batch struct {
	ID             uint      `gorm:"primaryKey"`
	SourceID       uint      `gorm:"not null;index"`
	SubscriptionID uint      `gorm:"not null;index"`
	IdentityID     *uint     `gorm:"index"`
	Status         string    `gorm:"size:16;not null;index"`
	ClosesAt       time.Time `gorm:"not null;index"`
	ProcessedAt    *time.Time
	CreatedAt      time.Time
}

// 版本状态。
const (
	VersionPending       = "pending"
	VersionPendingReview = "pending_review"
	VersionValid         = "valid"
	VersionRejected      = "rejected"
)

// Version 是某个字段的一次取值。敏感字段的值用主密钥加密（Sealed 为 true），
// ValueHash 是值的 HMAC，用来去重，不需要解密。每个“身份 + 字段”最多一个 Current。
type Version struct {
	ID          uint   `gorm:"primaryKey"`
	BatchID     uint   `gorm:"not null;index"`
	SourceID    uint   `gorm:"not null;index"`
	FieldID     uint   `gorm:"not null;index"`
	IdentityID  *uint  `gorm:"index"`
	Value       string `gorm:"type:text;not null"`
	Sealed      bool   `gorm:"not null"`
	ValueHash   string `gorm:"size:64;not null;index"`
	Status      string `gorm:"size:16;not null;index"`
	Reason      string `gorm:"size:255"`
	Current     bool   `gorm:"not null;index"`
	ExpiresAt   *time.Time
	SeenCount   int       `gorm:"not null"`
	LastSeenAt  time.Time `gorm:"not null"`
	ActivatedAt *time.Time
	// ExpiryNotifiedAt 是发过“即将过期”提醒的时间，每个版本只提醒一次。
	ExpiryNotifiedAt *time.Time
	CreatedAt        time.Time
}

// Destination 是推送目标（青龙、Webhook）。Config 是整份配置的密文，里面有 client_secret 这类密钥。
type Destination struct {
	ID        uint   `gorm:"primaryKey"`
	Name      string `gorm:"size:64;uniqueIndex;not null"`
	Kind      string `gorm:"size:16;not null"`
	Config    string `gorm:"type:text;not null"`
	Enabled   bool   `gorm:"not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// 多身份放法。
const (
	// ModePerIdentity 一个身份一条变量，备注 ferry:<来源>:<身份>。
	ModePerIdentity = "per_identity"
	// ModeJoined 所有身份合成一条变量，值用 & 连接，备注 ferry:<来源>。
	ModeJoined = "joined"
)

// Binding 把一个来源接到一个目的地。Rename 是输出变量改名（JSON 对象，键是来源里的输出名）。
type Binding struct {
	ID            uint   `gorm:"primaryKey"`
	SourceID      uint   `gorm:"not null;uniqueIndex:idx_binding_source_dest"`
	DestinationID uint   `gorm:"not null;uniqueIndex:idx_binding_source_dest"`
	Mode          string `gorm:"size:16;not null"`
	Rename        string `gorm:"type:text;not null"`
	Enabled       bool   `gorm:"not null"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// 推送任务状态。
const (
	PushQueued  = "queued"
	PushRunning = "running"
	PushSuccess = "success"
	PushFailed  = "failed"
	PushSkipped = "skipped"
)

// PushJob 是一次推送。不保存要推的值，执行时才读最新的当前值，重试不会推出过时的值。
type PushJob struct {
	ID         uint   `gorm:"primaryKey"`
	BindingID  uint   `gorm:"not null;index"`
	IdentityID *uint  `gorm:"index"`
	Status     string `gorm:"size:16;not null;index"`
	Attempts   int    `gorm:"not null"`
	// NextAttemptAt 是下次可以执行的时间；运行中时是认领的时间，用来回收卡住的任务。
	NextAttemptAt time.Time `gorm:"not null;index"`
	Trigger       string    `gorm:"size:32;not null"`
	LastError     string    `gorm:"size:512"`
	// Detail 记录推了哪些变量名，不含值。
	Detail     string `gorm:"size:512"`
	CreatedAt  time.Time
	FinishedAt *time.Time
}

// Channel 是通知渠道（Bark、Telegram、Webhook）。Config 是整份配置的密文。
type Channel struct {
	ID        uint   `gorm:"primaryKey"`
	Name      string `gorm:"size:64;uniqueIndex;not null"`
	Kind      string `gorm:"size:16;not null"`
	Config    string `gorm:"type:text;not null"`
	Enabled   bool   `gorm:"not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NotifyRule 决定什么事件发到哪些渠道。Events、SourceKeys、ChannelIDs 都是 JSON 数组，SourceKeys 为空表示全部来源。
type NotifyRule struct {
	ID         uint   `gorm:"primaryKey"`
	Name       string `gorm:"size:64;not null"`
	Events     string `gorm:"type:text;not null"`
	SourceKeys string `gorm:"type:text;not null"`
	ChannelIDs string `gorm:"type:text;not null"`
	Enabled    bool   `gorm:"not null"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
