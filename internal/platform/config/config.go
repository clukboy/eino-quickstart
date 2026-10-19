package config

import (
	"eino-quickstart/internal/platform/im/wechat"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/conf"
)

type ESConfig struct {
	Address     []string `json:"Address,optional"`
	Index       string   `json:"Index,optional"`
	Analyzer    string   `json:"Analyzer,optional"`
	Username    string   `json:"Username,optional"`
	Password    string   `json:"-"`
	PasswordEnv string   `json:"PasswordEnv,optional"`
	CaCertPath  string   `json:"CaCertPath,optional"`
	BulkSize    int      `json:"BulkSize,optional"`
	MappingFile string   `json:"MappingFile,optional"`
}

// Enabled 报告 ES 段是否配了集群地址。
func (c ESConfig) Enabled() bool { return len(c.Address) > 0 }

type Config struct {
	Server        ServerConfig        `json:"Server,optional"`
	Runtime       RuntimeConfig       `json:"Runtime,optional"`
	Observability ObservabilityConfig `json:"Observability,optional"`

	Agent       AgentConfig       `json:"Agent,optional"`
	Model       ModelConfig       `json:"Model,optional"`
	Workspace   WorkspaceConfig   `json:"Workspace,optional"`
	Context     Context           `json:"Context,optional"`
	Skills      Skills            `json:"Skills,optional"`
	Security    Security          `json:"Security,optional"`
	Storage     Storage           `json:"Storage,optional"`
	Auth        Auth              `json:"Auth,optional"`
	Execution   ExecutionConfig   `json:"Execution,optional"`
	Knowledge   KnowledgeConfig   `json:"Knowledge,optional"`
	Embedding   EmbeddingConfig   `json:"Embedding,optional"`
	Milvus      MilvusConfig      `json:"Milvus,optional"`
	ES          ESConfig          `json:"ES,optional"`
	Maintenance MaintenanceConfig `json:"Maintenance,optional"`
	Retrieval   RetrievalConfig   `json:"Retrieval,optional"`
	Indexer     IndexerConfig     `json:"Indexer,optional"`

	Asynq AsynqConfig `json:"Asynq,optional"`

	Redis RedisConfig `json:"Redis,optional"`
}

// RedisConfig contains only the serializable Redis settings used by the API cache.
type RedisConfig struct {
	Addrs       []string `json:"Addrs,optional"`
	Username    string   `json:"Username,optional"`
	Password    string   `json:"-"`
	PasswordEnv string   `json:"PasswordEnv,optional"`
}

func (c RedisConfig) UniversalOptions() redis.UniversalOptions {
	return redis.UniversalOptions{Addrs: c.Addrs, Username: c.Username, Password: c.Password}
}

type ServerConfig struct {
	Host string `json:"Host,optional"`
	Port int    `json:"Port,optional"`
}

type AgentConfig struct {
	Name          string `json:"Name,optional"`
	Instruction   string `json:"Instruction,optional"`
	MaxIterations int    `json:"MaxIterations,optional"`
}

type ModelConfig struct {
	Provider    string  `json:"Provider,optional"`
	BaseURL     string  `json:"BaseURL,optional"`
	APIKey      string  `json:"APIKey,optional"`
	Model       string  `json:"Model,optional"`
	Temperature float32 `json:"Temperature,optional"`
}

type WorkspaceConfig struct {
	Root                string `json:"Root,optional"`
	ShellTimeoutSeconds int    `json:"ShellTimeoutSeconds,optional"`
	MaxOutputBytes      int    `json:"MaxOutputBytes,optional"`
}

type Context struct {
	MaxHistoryMessages int `json:"MaxHistoryMessages,optional"`
	MaxToolOutputBytes int `json:"MaxToolOutputBytes,optional"`
}
type Skills struct {
	Root         string `json:"Root,optional"`
	MaxReadBytes int    `json:"MaxReadBytes,optional"`
}
type Security struct {
	AllowedTools             []string `json:"AllowedTools,optional"`
	RequireApprovalForShell  bool     `json:"RequireApprovalForShell,optional"`
	RequireApprovalForWrite  bool     `json:"RequireApprovalForWrite,optional"`
	MaxApprovalArgumentBytes int      `json:"MaxApprovalArgumentBytes,optional"`
	SensitiveArgumentKeys    []string `json:"SensitiveArgumentKeys,optional"`
	ApprovalTTLSeconds       int      `json:"ApprovalTTLSeconds,optional"`

	// SecretKeyEnv 指向一个环境变量名，该变量里放 base64 编码的 32 字节主密钥，
	// 用于加解密 ModelProvider.secret_ciphertext。SecretKey 是它的运行期取值，
	// 不出现在 JSON 里。
	SecretKeyEnv string `json:"SecretKeyEnv,optional"`
	SecretKey    string `json:"-"`
}

type Storage struct {
	Host        string `json:"Host,optional"`
	Port        int    `json:"Port,optional"`
	Username    string `json:"Username,optional"`
	Password    string `json:"-"`
	PasswordEnv string `json:"PasswordEnv,optional"`
	DBName      string `json:"DBName,optional"`
	SSLMode     string `json:"SSLMode,optional"`
	MaxOpenConn int    `json:"MaxOpenConn,optional"`
}

const maxAnonymousTTLHours = 24 * 365

const maxAccountTTLHours = 24 * 30

// 管理员初始密码的 bcrypt 成本上下界（0 表示不配、走 bcrypt.DefaultCost）。
//
// 下界 10 就是 bcrypt.DefaultCost：比它更低的成本在现代硬件上能被离线爆破
// 打穿，而它省下的那点 CPU 换不来任何东西。上界 16 的理由见校验处的注释。
const (
	minAccountBcryptCost = 10
	maxAccountBcryptCost = 16
)

type Auth struct {
	Enabled   bool                `json:"Enabled,optional"`
	APIKeys   []APIKeyConfig      `json:"APIKeys,optional"`
	Anonymous AnonymousAuthConfig `json:"Anonymous,optional"`
	Accounts  AccountsAuthConfig  `json:"Accounts,optional"`
	UserSync  UserSyncConfig      `json:"UserSync,optional"`
}

// the indexing process.
type UserSyncConfig struct {
	Enabled      bool                      `json:"Enabled,optional"`
	Type         string                    `json:"Type,optional"`
	WechatConfig map[string]*wechat.Config `json:"WechatConfig,optional"`
}
type APIKeyConfig struct {
	Subject string `json:"Subject,optional"`
	Role    string `json:"Role,optional"`
	KeyEnv  string `json:"KeyEnv,optional"`
}

type AnonymousAuthConfig struct {
	Enabled       bool   `json:"Enabled,optional"`
	Role          string `json:"Role,optional"`
	SecretEnv     string `json:"SecretEnv,optional"`
	TTLHours      int    `json:"TTLHours,optional"`
	SubjectPrefix string `json:"SubjectPrefix,optional"`

	Secret string `json:"-"`
}

type AccountsAuthConfig struct {
	Enabled bool `json:"Enabled,optional"`

	SecretEnv  string `json:"SecretEnv,optional"`
	TTLHours   int    `json:"TTLHours,optional"`
	BcryptCost int    `json:"BcryptCost,optional"`
	Secret     string `json:"-"`
}

type RuntimeConfig struct {
	ReadTimeoutSeconds  int `json:"ReadTimeoutSeconds,optional"`
	WriteTimeoutSeconds int `json:"WriteTimeoutSeconds,optional"`
	IdleTimeoutSeconds  int `json:"IdleTimeoutSeconds,optional"`
	MaxRequestBodyBytes int `json:"MaxRequestBodyBytes,optional"`
}

type ObservabilityConfig struct {
	LogLevel       string `json:"LogLevel,optional"`
	MetricsEnabled bool   `json:"MetricsEnabled,optional"`

	ServiceName string `json:"ServiceName,optional"`

	WorkerServiceName string `json:"WorkerServiceName,optional"`
	Environment       string `json:"Environment,optional"`

	OTLPEndpoint     string  `json:"OTLPEndpoint,optional"`
	OTLPInsecure     bool    `json:"OTLPInsecure,optional"`
	TraceSampleRatio float64 `json:"TraceSampleRatio,optional"`

	LogFilePath   string `json:"LogFilePath,optional"`
	LogMaxSizeMB  int    `json:"LogMaxSizeMB,optional"`
	LogMaxBackups int    `json:"LogMaxBackups,optional"`
	LogMaxAgeDays int    `json:"LogMaxAgeDays,optional"`
}

// WorkerTraceName 返回 worker 进程在 trace 里的服务名。
func (c ObservabilityConfig) WorkerTraceName() string {
	if name := strings.TrimSpace(c.WorkerServiceName); name != "" {
		return name
	}
	if name := strings.TrimSpace(c.ServiceName); name != "" {
		return name + "-worker"
	}
	return "eino-worker"
}

type ExecutionConfig struct {
	Mode             string `json:"Mode,optional"`
	DockerBinary     string `json:"DockerBinary,optional"`
	Image            string `json:"Image,optional"`
	User             string `json:"User,optional"`
	MemoryLimit      string `json:"MemoryLimit,optional"`
	CPULimit         string `json:"CPULimit,optional"`
	PIDsLimit        int    `json:"PIDsLimit,optional"`
	TmpFSSize        string `json:"TmpFSSize,optional"`
	AllowNetwork     bool   `json:"AllowNetwork,optional"`
	AllowLocalRunner bool   `json:"AllowLocalRunner,optional"`
}
type KnowledgeConfig struct {
	Root                  string            `json:"Root,optional"`
	MaxDocumentBytes      int               `json:"MaxDocumentBytes,optional"`
	MaxUploadRequestBytes int               `json:"MaxUploadRequestBytes,optional"`
	MaxUploadFiles        int               `json:"MaxUploadFiles,optional"`
	ChunkSizeCharacters   int               `json:"ChunkSizeCharacters,optional"`
	ChunkOverlapChars     int               `json:"ChunkOverlapCharacters,optional"`
	MaxChunksPerDoc       int               `json:"MaxChunksPerDocument,optional"`
	DefaultTopK           int               `json:"DefaultTopK,optional"`
	MaxTopK               int               `json:"MaxTopK,optional"`
	MaxQueryCharacters    int               `json:"MaxQueryCharacters,optional"`
	MaxResultBytes        int               `json:"MaxResultBytes,optional"`
	RecallGrouping        map[string]string `json:"RecallGrouping,optional"`
}

type EmbeddingConfig struct {
	BaseURL    string `json:"BaseURL,optional"`
	APIKeyEnv  string `json:"APIKeyEnv,optional"`
	Model      string `json:"Model,optional"`
	Dimensions int    `json:"Dimensions,optional"`
	BatchSize  int    `json:"BatchSize,optional"`
}

type MilvusConfig struct {
	Address         string `json:"Address,optional"`
	Collection      string `json:"Collection,optional"`
	MetricType      string `json:"MetricType,optional"`
	TopKCandidate   int    `json:"TopKCandidate,optional"`
	SearchTimeoutMS int    `json:"SearchTimeoutMS,optional"`
}

type MaintenanceConfig struct {
	CleanupIntervalSeconds   int `json:"CleanupIntervalSeconds,optional"`
	ApprovalRetentionHours   int `json:"ApprovalRetentionHours,optional"`
	CheckpointRetentionHours int `json:"CheckpointRetentionHours,optional"`
	TurnRetentionHours       int `json:"TurnRetentionHours,optional"`
	CleanupBatchSize         int `json:"CleanupBatchSize,optional"`
}

type RetrievalConfig struct {
	VectorWeight  float64 `json:"VectorWeight,optional"`
	KeywordWeight float64 `json:"KeywordWeight,optional"`
	ExactWeight   float64 `json:"ExactWeight,optional"`

	RRFSmoothing int `json:"RRFSmoothing,optional"`

	VectorCandidateLimit  int `json:"VectorCandidateLimit,optional"`
	KeywordCandidateLimit int `json:"KeywordCandidateLimit,optional"`
	ExactCandidateLimit   int `json:"ExactCandidateLimit,optional"`

	EnableRerank        bool   `json:"EnableRerank,optional"`
	MaxRerankCandidates int    `json:"MaxRerankCandidates,optional"`
	RerankEndpoint      string `json:"RerankEndpoint,optional"`
	RerankAPIKeyEnv     string `json:"RerankAPIKeyEnv,optional"`
	RerankAPIKey        string `json:"-"`
	RerankModel         string `json:"RerankModel,optional"`
	RerankTimeoutMS     int    `json:"RerankTimeoutMS,optional"`
}

type IndexerConfig struct {
	BatchSize int `json:"BatchSize,optional"`
}

type AsynqConfig struct {
	Enabled bool             `json:"Enabled,optional"`
	Redis   AsynqRedisConfig `json:"Redis,optional"`

	// Queues 是队列名到权重的映射，权重越高被拉取得越频繁。
	Queues []AsynqQueueConfig `json:"Queues,optional"`

	// Concurrency 是同时处理的任务数。索引任务大部分时间在等 embedding
	// 服务的网络往返，所以这个值可以远大于 CPU 核数。
	Concurrency int `json:"Concurrency,optional"`

	// MaxRetries 是单个任务的重试次数上限，含首次共执行 MaxRetries+1 次。
	MaxRetries int `json:"MaxRetries,optional"`

	RetryDelaySeconds    int `json:"RetryDelaySeconds,optional"`
	MaxRetryDelaySeconds int `json:"MaxRetryDelaySeconds,optional"`

	// ShutdownTimeoutSeconds 是优雅关闭的上限：已开始的任务最多再跑这么久，
	// 超时未完成的任务会被退回队列，由启动后的实例重新领取。
	ShutdownTimeoutSeconds int `json:"ShutdownTimeoutSeconds,optional"`
}

type AsynqRedisConfig struct {
	Addr        string `json:"Addr,optional"`
	Username    string `json:"Username,optional"`
	Password    string `json:"-"`
	PasswordEnv string `json:"PasswordEnv,optional"`
	DB          int    `json:"DB,optional"`
}

type AsynqQueueConfig struct {
	Name   string `json:"Name,optional"`
	Weight int    `json:"Weight,optional"`
}

// 配置文件的键名 = 结构体上的 json tag 名 = 大驼峰字段名（Host、MaxIterations、
// Knowledge.MaxChunksPerDocument……）。go-zero 的 mapping 只做大小写不敏感的
// 精确匹配，不认识下划线，所以 `HOST`、`max_iterations` 这类写法并不会命中
// 字段，只会被静默忽略 —— 改配置文件时请照抄 tag 的拼写。
//
// 每个字段都带 `,optional`：进程配置按角色拆成 api / worker 两份，任何一份都
// 必然缺另一份的段，go-zero 对没有 optional 的字段会直接报 "field not set"。

// Load retains the legacy combined profile for callers with existing configurations.
func Load(path string) (*Config, error) { return load(path, profileLegacy) }

// LoadAPI loads only API-owned settings; worker-only sections are rejected.
func LoadAPI(path string) (*Config, error) { return load(path, profileAPI) }

// LoadWorker does not read or validate API credentials, HTTP settings or workspace.
func LoadWorker(path string) (*Config, error) { return load(path, profileWorker) }

func load(path string, profile configProfile) (*Config, error) {
	// 角色校验（含未知键检测）按文件内容做，解析本身交给 go-zero 的 conf：
	// 它按扩展名选解析器，.yaml / .yml / .json 共用同一套结构体与同一套键名。
	if err := validateConfigFile(path, profile); err != nil {
		return nil, err
	}

	var cfg Config
	if err := conf.Load(path, &cfg); err != nil {
		return nil, err
	}

	if profile != profileWorker {
		if v := os.Getenv("EINO_MODEL_API_KEY"); v != "" {
			cfg.Model.APIKey = v
		}
		if v := os.Getenv("EINO_MODEL_BASE_URL"); v != "" {
			cfg.Model.BaseURL = v
		}
		if v := os.Getenv("EINO_MODEL"); v != "" {
			cfg.Model.Model = v
		}
		if v := os.Getenv("EINO_SERVER_PORT"); v != "" {
			var port int
			if _, err := fmt.Sscanf(v, "%d", &port); err == nil {
				cfg.Server.Port = port
			}
		}
		if v := os.Getenv("EINO_WORKSPACE_ROOT"); v != "" {
			cfg.Workspace.Root = v
		}
	}

	if cfg.Storage.PasswordEnv != "" {
		cfg.Storage.Password = os.Getenv(cfg.Storage.PasswordEnv)
	}
	if cfg.Asynq.Redis.PasswordEnv != "" {
		cfg.Asynq.Redis.Password = os.Getenv(cfg.Asynq.Redis.PasswordEnv)
	}
	// 与 storage / asynq 同一口径：集群口令只从环境变量读，不进配置文件。
	if cfg.ES.PasswordEnv != "" {
		cfg.ES.Password = os.Getenv(cfg.ES.PasswordEnv)
	}
	if cfg.Retrieval.RerankAPIKeyEnv != "" {
		cfg.Retrieval.RerankAPIKey = os.Getenv(cfg.Retrieval.RerankAPIKeyEnv)
	}
	if profile != profileWorker {
		// 外部模型供应商密钥的主密钥：数据库里只存密文，明文密钥来自环境变量。
		// 留空不算配置错误 —— 此时数据库里配置的供应商不可用，兜底专员会回落
		// 到统一配置的 Model 段（见 application/providermodel）。
		if cfg.Security.SecretKeyEnv != "" {
			cfg.Security.SecretKey = os.Getenv(cfg.Security.SecretKeyEnv)
		}
		// 匿名令牌的签名密钥同理：它一旦泄漏，任何人都能伪造任意 subject。
		if cfg.Auth.Anonymous.SecretEnv != "" {
			cfg.Auth.Anonymous.Secret = os.Getenv(cfg.Auth.Anonymous.SecretEnv)
		}
		// 账号令牌的签名密钥同理，且后果更重：泄漏出去等于任何人都能伪造任意
		// `user:<id>`（包括 admin）的身份。
		if cfg.Auth.Accounts.SecretEnv != "" {
			cfg.Auth.Accounts.Secret = os.Getenv(cfg.Auth.Accounts.SecretEnv)
		}
	}

	if cfg.Storage.PasswordEnv == "" {
		return nil, fmt.Errorf("Storage.PasswordEnv is required")
	}
	if cfg.Storage.Password == "" {
		return nil, fmt.Errorf(
			"environment variable %s is required",
			cfg.Storage.PasswordEnv,
		)
	}
	if profile != profileWorker {
		if !filepath.IsAbs(cfg.Workspace.Root) {
			abs, err := filepath.Abs(cfg.Workspace.Root)
			if err != nil {
				return nil, err
			}
			cfg.Workspace.Root = abs
		}

		if len(cfg.Security.AllowedTools) == 0 {
			return nil, fmt.Errorf("Security.AllowedTools must contain at least one tool")
		}
		if cfg.Security.MaxApprovalArgumentBytes <= 0 {
			return nil, errors.New("Security.MaxApprovalArgumentBytes must be greater than zero")
		}
		if len(cfg.Security.SensitiveArgumentKeys) == 0 {
			return nil, errors.New("Security.SensitiveArgumentKeys must contain at least one key")
		}
		if cfg.Security.ApprovalTTLSeconds <= 0 {
			return nil, fmt.Errorf(
				"Security.ApprovalTTLSeconds must be greater than zero",
			)
		}

		if !cfg.Auth.Enabled {
			return nil, fmt.Errorf("Auth.Enabled must be true")
		}
		if len(cfg.Auth.APIKeys) == 0 {
			return nil, fmt.Errorf("Auth.APIKeys must contain at least one API key")
		}

		for _, key := range cfg.Auth.APIKeys {
			if key.Subject == "" {
				return nil, fmt.Errorf("Auth.APIKeys.Subject must be set")
			}
			if key.KeyEnv == "" {
				return nil, fmt.Errorf(
					"Auth.APIKeys entry %q has an empty KeyEnv",
					key.Subject,
				)
			}
			switch key.Role {
			case "agent", "approver", "admin":
			default:
				return nil, fmt.Errorf(
					"Auth.APIKeys entry %q has invalid Role %q",
					key.Subject,
					key.Role,
				)
			}
			if os.Getenv(key.KeyEnv) == "" {
				return nil, fmt.Errorf(
					"environment variable %s is required",
					key.KeyEnv,
				)
			}
		}

		// 匿名身份。关掉时一个字段都不校验（部署形态就是「只有管理台」），
		// 打开时**每一项都必须齐**：leaving 一个可选的签名密钥等于线上静默地
		// 给所有人签发同一个（或空密钥签的）身份，而症状只是「会话还是互相可见」，
		// 与没开匿名一模一样 —— 所以宁可启动失败。
		if cfg.Auth.Anonymous.Enabled {
			switch cfg.Auth.Anonymous.Role {
			case "agent", "approver", "admin":
			default:
				return nil, fmt.Errorf(
					"Auth.Anonymous.Role %q is not a known role",
					cfg.Auth.Anonymous.Role,
				)
			}
			if cfg.Auth.Anonymous.SecretEnv == "" {
				return nil, fmt.Errorf(
					"Auth.Anonymous.SecretEnv is required when anonymous access is enabled",
				)
			}
			if cfg.Auth.Anonymous.Secret == "" {
				return nil, fmt.Errorf(
					"environment variable %s is required",
					cfg.Auth.Anonymous.SecretEnv,
				)
			}
			if cfg.Auth.Anonymous.TTLHours <= 0 {
				return nil, fmt.Errorf(
					"Auth.Anonymous.TTLHours must be greater than zero",
				)
			}
			if cfg.Auth.Anonymous.TTLHours > maxAnonymousTTLHours {
				return nil, fmt.Errorf(
					"Auth.Anonymous.TTLHours must not exceed %d",
					maxAnonymousTTLHours,
				)
			}
			// 前缀空着时回填默认值而不是报错：它有一个显然正确的取值，且必须与
			// internal/platform/auth 的 AnonymousSubjectPrefix 一致，否则
			// 「哪个 subject 是匿名的」在库里就认不出来。
			if cfg.Auth.Anonymous.SubjectPrefix == "" {
				cfg.Auth.Anonymous.SubjectPrefix = "anon:"
			}
		}

		// 账号登录。口径与匿名完全一致：关掉时一个字段都不校验（部署形态就是
		// 「只有管理台 + 匿名访客」），打开时每一项都必须齐 —— 少一个密钥会让
		// 所有账号令牌用空密钥签出去（谁都能伪造任意 user:<id>，包括 admin），
		// 而症状不是崩溃，只是「一切正常」。
		//
		// 刻意**不**校验「accounts 与 anonymous 的密钥必须不同」：两个 issuer 的
		// 令牌前缀不同（user. / anon.），配成同一个值不会导致串用；把它写成硬
		// 校验只会让「本地图省事只配一个密钥」变成启动失败。这一点写在
		// configs/config.yaml 的注释里。
		if cfg.Auth.Accounts.Enabled {
			if cfg.Auth.Accounts.SecretEnv == "" {
				return nil, fmt.Errorf(
					"Auth.Accounts.SecretEnv is required when accounts are enabled",
				)
			}
			if cfg.Auth.Accounts.Secret == "" {
				return nil, fmt.Errorf(
					"environment variable %s is required",
					cfg.Auth.Accounts.SecretEnv,
				)
			}
			if cfg.Auth.Accounts.TTLHours <= 0 {
				return nil, fmt.Errorf(
					"Auth.Accounts.TTLHours must be greater than zero",
				)
			}
			if cfg.Auth.Accounts.TTLHours > maxAccountTTLHours {
				return nil, fmt.Errorf(
					"Auth.Accounts.TTLHours must not exceed %d",
					maxAccountTTLHours,
				)
			}
			// 成本留 0 表示「用默认值」（bcrypt.DefaultCost = 10）。
			//
			// 上界不是洁癖：bcrypt 的成本每加 1 就把单次哈希的耗时翻倍，而
			// /auth/login 没有任何限流（见 docs/known-gaps.md）。配成 20 以上时
			// 每次登录要占用秒级的 CPU，几个并发请求就能把进程的 CPU 打满 ——
			// 那是一个配置就能造成的自伤。
			if cost := cfg.Auth.Accounts.BcryptCost; cost != 0 &&
				(cost < minAccountBcryptCost || cost > maxAccountBcryptCost) {
				return nil, fmt.Errorf(
					"Auth.Accounts.BcryptCost must be 0 (default) or between %d and %d",
					minAccountBcryptCost,
					maxAccountBcryptCost,
				)
			}
		}

		if cfg.Auth.UserSync.Enabled {
			if !cfg.Auth.Accounts.Enabled {
				return nil, fmt.Errorf("Auth.UserSync requires Auth.Accounts.Enabled")
			}
			if cfg.Auth.UserSync.Type == "" {
				return nil, fmt.Errorf("Auth.UserSync.Type is required when user sync is enabled")
			}
		}

		if cfg.Runtime.ReadTimeoutSeconds <= 0 {
			return nil, fmt.Errorf("Runtime.ReadTimeoutSeconds must be greater than zero")
		}

		if cfg.Runtime.WriteTimeoutSeconds <= 0 {
			return nil, fmt.Errorf("Runtime.WriteTimeoutSeconds must be greater than zero")
		}

		if cfg.Runtime.IdleTimeoutSeconds <= 0 {
			return nil, fmt.Errorf("Runtime.IdleTimeoutSeconds must be greater than zero")
		}

		if cfg.Runtime.MaxRequestBodyBytes <= 0 {
			return nil, fmt.Errorf("Runtime.MaxRequestBodyBytes must be greater than zero")
		}
		if cfg.Knowledge.MaxDocumentBytes <= 0 {
			return nil, fmt.Errorf("Knowledge.MaxDocumentBytes must be greater than zero")
		}
		if cfg.Runtime.MaxRequestBodyBytes < cfg.Knowledge.MaxDocumentBytes {
			return nil, fmt.Errorf(
				"Runtime.MaxRequestBodyBytes must be at least Knowledge.MaxDocumentBytes",
			)
		}
		if cfg.Knowledge.MaxUploadRequestBytes <= 0 {
			return nil, fmt.Errorf(
				"Knowledge.MaxUploadRequestBytes must be greater than zero",
			)
		}
		if cfg.Knowledge.MaxUploadRequestBytes < cfg.Knowledge.MaxDocumentBytes {
			return nil, fmt.Errorf(
				"Knowledge.MaxUploadRequestBytes must be at least Knowledge.MaxDocumentBytes",
			)
		}
		if cfg.Knowledge.MaxUploadFiles <= 0 {
			return nil, fmt.Errorf(
				"Knowledge.MaxUploadFiles must be greater than zero",
			)
		}
	}

	switch cfg.Observability.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return nil, fmt.Errorf(
			"Observability.LogLevel must be debug, info, warn, or error",
		)
	}
	if cfg.Observability.ServiceName == "" {
		return nil, fmt.Errorf(
			"Observability.ServiceName is required",
		)
	}

	if cfg.Observability.Environment == "" {
		return nil, fmt.Errorf(
			"Observability.Environment is required",
		)
	}

	if cfg.Observability.OTLPEndpoint == "" {
		return nil, fmt.Errorf(
			"Observability.OTLPEndpoint is required",
		)
	}

	if cfg.Observability.TraceSampleRatio < 0 ||
		cfg.Observability.TraceSampleRatio > 1 {
		return nil, fmt.Errorf(
			"Observability.TraceSampleRatio must be between 0 and 1",
		)
	}

	if cfg.Observability.LogFilePath == "" {
		return nil, fmt.Errorf(
			"Observability.LogFilePath is required",
		)
	}

	if cfg.Observability.LogMaxSizeMB <= 0 {
		return nil, fmt.Errorf(
			"Observability.LogMaxSizeMB must be greater than zero",
		)
	}

	if cfg.Observability.LogMaxBackups <= 0 {
		return nil, fmt.Errorf(
			"Observability.LogMaxBackups must be greater than zero",
		)
	}

	if cfg.Observability.LogMaxAgeDays <= 0 {
		return nil, fmt.Errorf(
			"Observability.LogMaxAgeDays must be greater than zero",
		)
	}

	if profile != profileWorker {
		switch cfg.Execution.Mode {
		case "disabled", "docker", "local":
		default:
			return nil, fmt.Errorf(
				"Execution.Mode must be docker or local",
			)
		}

		if cfg.Execution.Mode == "docker" &&
			cfg.Server.Host != "127.0.0.1" &&
			cfg.Server.Host != "localhost" {
			if cfg.Execution.Image == "" {
				return nil, fmt.Errorf(
					"Execution.Image is required in docker mode",
				)
			}
			if cfg.Execution.User == "" {
				return nil, fmt.Errorf(
					"Execution.User is required in docker mode",
				)
			}
			if cfg.Execution.MemoryLimit == "" {
				return nil, fmt.Errorf(
					"Execution.MemoryLimit is required in docker mode",
				)
			}
			if cfg.Execution.CPULimit == "" {
				return nil, fmt.Errorf(
					"Execution.CPULimit is required in docker mode",
				)
			}
			if cfg.Execution.PIDsLimit <= 0 {
				return nil, fmt.Errorf(
					"Execution.PIDsLimit must be greater than zero",
				)
			}
		}

		if cfg.Execution.Mode == "local" &&
			!cfg.Execution.AllowLocalRunner {
			return nil, fmt.Errorf(
				"local execution requires Execution.AllowLocalRunner=true",
			)
		}
	}

	if profile != profileAPI {
		if cfg.Knowledge.Root == "" {
			return nil, fmt.Errorf("Knowledge.Root is required")
		}

		if cfg.Knowledge.MaxDocumentBytes <= 0 {
			return nil, fmt.Errorf(
				"Knowledge.MaxDocumentBytes must be greater than zero",
			)
		}

		if cfg.Knowledge.ChunkSizeCharacters <= 0 {
			return nil, fmt.Errorf(
				"Knowledge.ChunkSizeCharacters must be greater than zero",
			)
		}

		if cfg.Knowledge.ChunkOverlapChars < 0 ||
			cfg.Knowledge.ChunkOverlapChars >=
				cfg.Knowledge.ChunkSizeCharacters {
			return nil, fmt.Errorf(
				"Knowledge.ChunkOverlapCharacters must be non-negative and smaller than chunkSizeCharacters",
			)
		}

		if cfg.Knowledge.MaxChunksPerDoc <= 0 {
			return nil, fmt.Errorf(
				"Knowledge.MaxChunksPerDocument must be greater than zero",
			)
		}
	}

	if profile != profileWorker {
		if cfg.Knowledge.DefaultTopK <= 0 ||
			cfg.Knowledge.DefaultTopK > cfg.Knowledge.MaxTopK {
			return nil, fmt.Errorf(
				"Knowledge.DefaultTopK must be between 1 and Knowledge.MaxTopK",
			)
		}
	}

	if cfg.Embedding.Model == "" {
		return nil, fmt.Errorf("Embedding.Model is required")
	}

	if cfg.Embedding.Dimensions <= 0 {
		return nil, fmt.Errorf(
			"Embedding.Dimensions must be greater than zero",
		)
	}

	if cfg.Embedding.BatchSize <= 0 {
		return nil, fmt.Errorf(
			"Embedding.BatchSize must be greater than zero",
		)
	}

	if os.Getenv(cfg.Embedding.APIKeyEnv) == "" {
		return nil, fmt.Errorf(
			"environment variable %s is required",
			cfg.Embedding.APIKeyEnv,
		)
	}

	if cfg.Maintenance.CleanupIntervalSeconds <= 0 {
		return nil, fmt.Errorf(
			"Maintenance.CleanupIntervalSeconds must be greater than zero",
		)
	}

	if cfg.Maintenance.ApprovalRetentionHours <= 0 {
		return nil, fmt.Errorf(
			"Maintenance.ApprovalRetentionHours must be greater than zero",
		)
	}

	if cfg.Maintenance.CheckpointRetentionHours <= 0 {
		return nil, fmt.Errorf(
			"Maintenance.CheckpointRetentionHours must be greater than zero",
		)
	}

	if cfg.Maintenance.TurnRetentionHours <= 0 {
		return nil, fmt.Errorf(
			"Maintenance.TurnRetentionHours must be greater than zero",
		)
	}

	if cfg.Maintenance.CleanupBatchSize <= 0 {
		return nil, fmt.Errorf(
			"Maintenance.CleanupBatchSize must be greater than zero",
		)
	}

	if profile != profileWorker {
		if cfg.Retrieval.VectorWeight < 0 ||
			cfg.Retrieval.KeywordWeight < 0 ||
			(cfg.Retrieval.VectorWeight == 0 &&
				cfg.Retrieval.KeywordWeight == 0 &&
				cfg.Retrieval.ExactWeight == 0) {
			return nil, fmt.Errorf(
				"at least one retrieval weight must be greater than zero",
			)
		}

		if cfg.Retrieval.RRFSmoothing <= 0 {
			return nil, fmt.Errorf(
				"Retrieval.RRFSmoothing must be greater than zero",
			)
		}

		if cfg.Retrieval.VectorCandidateLimit <
			cfg.Knowledge.MaxTopK {
			return nil, fmt.Errorf(
				"Retrieval.VectorCandidateLimit must be at least Knowledge.MaxTopK",
			)
		}

		if cfg.Retrieval.KeywordCandidateLimit <
			cfg.Knowledge.MaxTopK {
			return nil, fmt.Errorf(
				"Retrieval.KeywordCandidateLimit must be at least Knowledge.MaxTopK",
			)
		}

		if cfg.Retrieval.ExactCandidateLimit <
			cfg.Knowledge.MaxTopK {
			return nil, fmt.Errorf(
				"Retrieval.ExactCandidateLimit must be at least Knowledge.MaxTopK",
			)
		}

		if cfg.Retrieval.EnableRerank {
			if cfg.Retrieval.MaxRerankCandidates <= 0 {
				return nil, fmt.Errorf(
					"Retrieval.MaxRerankCandidates must be greater than zero when rerank is enabled",
				)
			}
			if strings.TrimSpace(cfg.Retrieval.RerankEndpoint) == "" {
				return nil, fmt.Errorf(
					"Retrieval.RerankEndpoint is required when rerank is enabled",
				)
			}
			if cfg.Retrieval.RerankTimeoutMS < 0 {
				return nil, fmt.Errorf("Retrieval.RerankTimeoutMS must not be negative")
			}
		}
	}

	if profile != profileAPI {
		if cfg.Indexer.BatchSize <= 0 {
			return nil, fmt.Errorf(
				"Indexer.BatchSize must be greater than zero",
			)
		}
	}

	// ES 段关着（address 为空）时一律不校验：这条降级路径是设计的一部分，
	// 不该被一段没人用的配置挡住进程启动。
	if cfg.ES.Enabled() {
		if err := validateES(cfg.ES); err != nil {
			return nil, err
		}
	}

	if profile == profileWorker {
		if !cfg.Asynq.Enabled {
			return nil, fmt.Errorf("Asynq.Enabled must be true for worker")
		}
		consumesIndex := false
		for _, q := range cfg.Asynq.Queues {
			consumesIndex = consumesIndex || q.Name == "index"
		}
		if !consumesIndex {
			return nil, fmt.Errorf("worker Asynq.Queues must include the index queue")
		}
		if cfg.Milvus.Address == "" || cfg.Milvus.Collection == "" {
			return nil, fmt.Errorf("worker Milvus.Address and Milvus.Collection are required")
		}
	}

	if cfg.Asynq.Enabled {
		if err := validateAsynq(cfg.Asynq, profile != profileAPI); err != nil {
			return nil, err
		}
	}

	return &cfg, nil
}

// validateES 只在 ES.Address 非空时执行。
//
// 校验口径是「配了一半比没配更危险」：地址配上、索引名或口令没配，进程能起来，
// 但要到第一个分块写不进去、或者第一次关键字查询静默返回空结果时才暴露 ——
// 那时人已经在排查检索质量了，不会想到是配置缺字段。
func validateES(cfg ESConfig) error {
	for _, addr := range cfg.Address {
		if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
			// 裸 host:port 也能被客户端接受，但会被当成 http，托管集群上会
			// 以「连接被重置」这种和真实原因无关的方式失败。
			return fmt.Errorf("ES.Address %q must start with http:// or https://", addr)
		}
	}
	if cfg.Index == "" {
		return fmt.Errorf("ES.Index is required when ES.Address is set")
	}
	if cfg.Analyzer == "" {
		return fmt.Errorf("ES.Analyzer is required when ES.Address is set")
	}
	if cfg.BulkSize <= 0 {
		return fmt.Errorf("ES.BulkSize must be greater than zero")
	}
	// 映射文件是检索面的唯一定义处：没有它，进程连「该写哪些字段」都不知道。
	// 在这里拦下来，好过等到 es.New 里报错时还带着一个已经连上的集群连接。
	if cfg.MappingFile == "" {
		return fmt.Errorf(
			"ES.MappingFile is required when ES.Address is set; " +
				"see configs/es/chunk_mapping.yaml for a working example",
		)
	}
	if _, err := os.Stat(cfg.MappingFile); err != nil {
		return fmt.Errorf(
			"ES.MappingFile %q is not readable: %w（路径相对进程的工作目录，通常是仓库根）",
			cfg.MappingFile, err,
		)
	}
	// basic auth 是 user + password 成对的：只配 user 不配口令只会拿到 401。
	if cfg.Username != "" {
		if cfg.PasswordEnv == "" {
			return fmt.Errorf("ES.PasswordEnv is required when ES.Username is set")
		}
		if cfg.Password == "" {
			return fmt.Errorf("environment variable %s is required", cfg.PasswordEnv)
		}
	}
	return nil
}

// validateAsynq 只在 Asynq.Enabled=true 时执行：关掉队列意味着整个索引链路
// 停摆，那种情况下再校验连接参数只会挡住一次有意为之的排障启动。
func validateAsynq(cfg AsynqConfig, consumer bool) error {
	if cfg.Redis.Addr == "" {
		return fmt.Errorf("Asynq.Redis.Addr is required")
	}
	if cfg.Redis.DB < 0 {
		return fmt.Errorf("Asynq.Redis.DB must not be negative")
	}
	if cfg.Redis.PasswordEnv != "" && cfg.Redis.Password == "" {
		return fmt.Errorf(
			"environment variable %s is required",
			cfg.Redis.PasswordEnv,
		)
	}
	if consumer {
		if len(cfg.Queues) == 0 {
			return fmt.Errorf("Asynq.Queues must contain at least one queue")
		}
		for _, queue := range cfg.Queues {
			if queue.Name == "" {
				return fmt.Errorf("Asynq.Queues.Name must be set")
			}
			if queue.Weight <= 0 {
				return fmt.Errorf(
					"asynq queue %q must have a positive weight",
					queue.Name,
				)
			}
		}
		if cfg.Concurrency <= 0 {
			return fmt.Errorf("Asynq.Concurrency must be greater than zero")
		}
	}
	if cfg.MaxRetries < 0 {
		return fmt.Errorf("Asynq.MaxRetries must not be negative")
	}
	if consumer {
		if cfg.RetryDelaySeconds <= 0 {
			return fmt.Errorf("Asynq.RetryDelaySeconds must be greater than zero")
		}
		if cfg.MaxRetryDelaySeconds < cfg.RetryDelaySeconds {
			return fmt.Errorf(
				"Asynq.MaxRetryDelaySeconds must be >= retryDelaySeconds",
			)
		}
		if cfg.ShutdownTimeoutSeconds <= 0 {
			return fmt.Errorf(
				"Asynq.ShutdownTimeoutSeconds must be greater than zero",
			)
		}
	}
	return nil
}
