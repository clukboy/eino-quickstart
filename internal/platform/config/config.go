package config

import (
	"bytes"
	"eino-quickstart/internal/platform/im/wechat"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/redis/go-redis/v9"
	"gopkg.in/yaml.v3"
)

type ESConfig struct {
	Address     []string `yaml:"address"`
	Index       string   `yaml:"index"`
	Analyzer    string   `yaml:"analyzer"`
	Username    string   `yaml:"username"`
	Password    string   `yaml:"-"`
	PasswordEnv string   `yaml:"passwordEnv"`
	CaCertPath  string   `yaml:"caCertPath"`
	BulkSize    int      `yaml:"bulkSize"`
	MappingFile string `yaml:"mappingFile"`
}

// Enabled 报告 ES 段是否配了集群地址。
func (c ESConfig) Enabled() bool { return len(c.Address) > 0 }

type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Runtime       RuntimeConfig       `yaml:"runtime"`
	Observability ObservabilityConfig `yaml:"observability"`

	Agent       AgentConfig       `yaml:"agent"`
	Model       ModelConfig       `yaml:"model"`
	Workspace   WorkspaceConfig   `yaml:"workspace"`
	Context     Context           `yaml:"context"`
	Skills      Skills            `yaml:"skills"`
	Security    Security          `yaml:"security"`
	Storage     Storage           `yaml:"storage"`
	Auth        Auth              `yaml:"auth"`
	Execution   ExecutionConfig   `yaml:"execution"`
	Knowledge   KnowledgeConfig   `yaml:"knowledge"`
	Embedding   EmbeddingConfig   `yaml:"embedding"`
	Milvus      MilvusConfig      `yaml:"milvus"`
	ES          ESConfig          `yaml:"es"`
	Maintenance MaintenanceConfig `yaml:"maintenance"`
	Retrieval   RetrievalConfig   `yaml:"retrieval"`
	Indexer     IndexerConfig     `yaml:"indexer"`

	Asynq AsynqConfig `yaml:"asynq"`

	Redis redis.UniversalOptions `yaml:"redis"`

}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type AgentConfig struct {
	Name          string `yaml:"name"`
	Instruction   string `yaml:"instruction"`
	MaxIterations int    `yaml:"max_iterations"`
}

type ModelConfig struct {
	Provider    string  `yaml:"provider"`
	BaseURL     string  `yaml:"baseURL"`
	APIKey      string  `yaml:"apiKey"`
	Model       string  `yaml:"model"`
	Temperature float32 `yaml:"temperature"`
}

type WorkspaceConfig struct {
	Root                string `yaml:"root"`
	ShellTimeoutSeconds int    `yaml:"shellTimeoutSeconds"`
	MaxOutputBytes      int    `yaml:"maxOutputBytes"`
}

type Context struct {
	MaxHistoryMessages int `yaml:"maxHistoryMessages"`
	MaxToolOutputBytes int `yaml:"maxToolOutputBytes"`
}
type Skills struct {
	Root         string `yaml:"root"`
	MaxReadBytes int    `yaml:"maxReadBytes"`
}
type Security struct {
	AllowedTools             []string `yaml:"allowedTools"`
	RequireApprovalForShell  bool     `yaml:"requireApprovalForShell"`
	RequireApprovalForWrite  bool     `yaml:"requireApprovalForWrite"`
	MaxApprovalArgumentBytes int      `yaml:"maxApprovalArgumentBytes"`
	SensitiveArgumentKeys    []string `yaml:"sensitiveArgumentKeys"`
	ApprovalTTLSeconds       int      `yaml:"approvalTTLSeconds"`
}

type Storage struct {
	Host        string `yaml:"host"`
	Port        int    `yaml:"port"`
	Username    string `yaml:"username"`
	Password    string `yaml:"-"`
	PasswordEnv string `yaml:"passwordEnv"`
	DBName      string `yaml:"dbName"`
	SSLMode     string `yaml:"sslMode"`
	MaxOpenConn int    `yaml:"maxOpenConn"`
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
	Enabled   bool                `yaml:"enabled"`
	APIKeys   []APIKeyConfig      `yaml:"apiKeys"`
	Anonymous AnonymousAuthConfig `yaml:"anonymous"`
	Accounts  AccountsAuthConfig  `yaml:"accounts"`
	UserSync  UserSyncConfig      `yaml:"userSync"`
}

// the indexing process.
type UserSyncConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Type 	 string `yaml:"type"`
	WechatConfig map[string]*wechat.Config `yaml:"wechatConfig"`
}
type APIKeyConfig struct {
	Subject string `yaml:"subject"`
	Role    string `yaml:"role"`
	KeyEnv  string `yaml:"keyEnv"`
}

type AnonymousAuthConfig struct {
	Enabled       bool   `yaml:"enabled"`
	Role          string `yaml:"role"`
	SecretEnv     string `yaml:"secretEnv"`
	TTLHours      int    `yaml:"ttlHours"`
	SubjectPrefix string `yaml:"subjectPrefix"`

	Secret string `yaml:"-"`
}

type AccountsAuthConfig struct {
	Enabled bool `yaml:"enabled"`

	SecretEnv string `yaml:"secretEnv"`
	TTLHours  int    `yaml:"ttlHours"`
	BcryptCost int `yaml:"bcryptCost"`
	Secret string `yaml:"-"`
}

type RuntimeConfig struct {
	ReadTimeoutSeconds  int `yaml:"readTimeoutSeconds"`
	WriteTimeoutSeconds int `yaml:"writeTimeoutSeconds"`
	IdleTimeoutSeconds  int `yaml:"idleTimeoutSeconds"`
	MaxRequestBodyBytes int `yaml:"maxRequestBodyBytes"`
}

type ObservabilityConfig struct {
	LogLevel       string `yaml:"logLevel"`
	MetricsEnabled bool   `yaml:"metricsEnabled"`

	ServiceName string `yaml:"serviceName"`

	WorkerServiceName string `yaml:"workerServiceName"`
	Environment       string `yaml:"environment"`

	OTLPEndpoint     string  `yaml:"otlpEndpoint"`
	OTLPInsecure     bool    `yaml:"otlpInsecure"`
	TraceSampleRatio float64 `yaml:"traceSampleRatio"`

	LogFilePath   string `yaml:"logFilePath"`
	LogMaxSizeMB  int    `yaml:"logMaxSizeMB"`
	LogMaxBackups int    `yaml:"logMaxBackups"`
	LogMaxAgeDays int    `yaml:"logMaxAgeDays"`
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
	Mode             string `yaml:"mode"`
	DockerBinary     string `yaml:"dockerBinary"`
	Image            string `yaml:"image"`
	User             string `yaml:"user"`
	MemoryLimit      string `yaml:"memoryLimit"`
	CPULimit         string `yaml:"cpuLimit"`
	PIDsLimit        int    `yaml:"pidsLimit"`
	TmpFSSize        string `yaml:"tmpFSSize"`
	AllowNetwork     bool   `yaml:"allowNetwork"`
	AllowLocalRunner bool   `yaml:"allowLocalRunner"`
}
type KnowledgeConfig struct {
	Root                  string `yaml:"root"`
	MaxDocumentBytes      int    `yaml:"maxDocumentBytes"`
	MaxUploadRequestBytes int    `yaml:"maxUploadRequestBytes"`
	MaxUploadFiles        int    `yaml:"maxUploadFiles"`
	ChunkSizeCharacters   int    `yaml:"chunkSizeCharacters"`
	ChunkOverlapChars     int    `yaml:"chunkOverlapCharacters"`
	MaxChunksPerDoc       int    `yaml:"maxChunksPerDocument"`
	DefaultTopK           int    `yaml:"defaultTopK"`
	MaxTopK               int    `yaml:"maxTopK"`
	MaxQueryCharacters    int    `yaml:"maxQueryCharacters"`
	MaxResultBytes int `yaml:"maxResultBytes"`
	RecallGrouping map[string]string `yaml:"recallGrouping"`

}

type EmbeddingConfig struct {
	BaseURL    string `yaml:"baseURL"`
	APIKeyEnv  string `yaml:"apiKeyEnv"`
	Model      string `yaml:"model"`
	Dimensions int    `yaml:"dimensions"`
	BatchSize int `yaml:"batchSize"`
}

type MilvusConfig struct {
	Address         string `yaml:"address"`
	Collection      string `yaml:"collection"`
	MetricType      string `yaml:"metricType"`
	TopKCandidate   int    `yaml:"topKCandidate"`
	SearchTimeoutMS int    `yaml:"searchTimeoutMS"`
}

type MaintenanceConfig struct {
	CleanupIntervalSeconds   int `yaml:"cleanupIntervalSeconds"`
	ApprovalRetentionHours   int `yaml:"approvalRetentionHours"`
	CheckpointRetentionHours int `yaml:"checkpointRetentionHours"`
	TurnRetentionHours       int `yaml:"turnRetentionHours"`
	CleanupBatchSize         int `yaml:"cleanupBatchSize"`
}

type RetrievalConfig struct {
	VectorWeight  float64 `yaml:"vectorWeight"`
	KeywordWeight float64 `yaml:"keywordWeight"`
	ExactWeight   float64 `yaml:"exactWeight"`

	RRFSmoothing int `yaml:"rrfSmoothing"`

	VectorCandidateLimit  int `yaml:"vectorCandidateLimit"`
	KeywordCandidateLimit int `yaml:"keywordCandidateLimit"`
	ExactCandidateLimit   int `yaml:"exactCandidateLimit"`

	EnableRerank        bool   `yaml:"enableRerank"`
	MaxRerankCandidates int    `yaml:"maxRerankCandidates"`
	RerankEndpoint      string `yaml:"rerankEndpoint"`
	RerankAPIKeyEnv     string `yaml:"rerankApiKeyEnv"`
	RerankAPIKey        string `yaml:"-"`
	RerankModel         string `yaml:"rerankModel"`
	RerankTimeoutMS     int    `yaml:"rerankTimeoutMS"`
}

type IndexerConfig struct {
	BatchSize int `yaml:"batchSize"`
}

type AsynqConfig struct {
	Enabled bool             `yaml:"enabled"`
	Redis   AsynqRedisConfig `yaml:"redis"`

	// Queues 是队列名到权重的映射，权重越高被拉取得越频繁。
	Queues []AsynqQueueConfig `yaml:"queues"`

	// Concurrency 是同时处理的任务数。索引任务大部分时间在等 embedding
	// 服务的网络往返，所以这个值可以远大于 CPU 核数。
	Concurrency int `yaml:"concurrency"`

	// MaxRetries 是单个任务的重试次数上限，含首次共执行 MaxRetries+1 次。
	MaxRetries int `yaml:"maxRetries"`

	RetryDelaySeconds    int `yaml:"retryDelaySeconds"`
	MaxRetryDelaySeconds int `yaml:"maxRetryDelaySeconds"`

	// ShutdownTimeoutSeconds 是优雅关闭的上限：已开始的任务最多再跑这么久，
	// 超时未完成的任务会被退回队列，由启动后的实例重新领取。
	ShutdownTimeoutSeconds int `yaml:"shutdownTimeoutSeconds"`
}

type AsynqRedisConfig struct {
	Addr        string `yaml:"addr"`
	Username    string `yaml:"username"`
	Password    string `yaml:"-"`
	PasswordEnv string `yaml:"passwordEnv"`
	DB          int    `yaml:"db"`
}

type AsynqQueueConfig struct {
	Name   string `yaml:"name"`
	Weight int    `yaml:"weight"`
}

// Load retains the legacy combined profile for callers with existing configurations.
func Load(path string) (*Config, error) { return load(path, profileLegacy) }

// LoadAPI loads only API-owned settings; worker-only sections are rejected.
func LoadAPI(path string) (*Config, error) { return load(path, profileAPI) }

// LoadWorker does not read or validate API credentials, HTTP settings or workspace.
func LoadWorker(path string) (*Config, error) { return load(path, profileWorker) }

func load(path string, profile configProfile) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if profile != profileLegacy {
		if err := validateProfileSections(data, profile); err != nil {
			return nil, err
		}
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(profile != profileLegacy)
	if err := decoder.Decode(&cfg); err != nil {
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
		return nil, fmt.Errorf("storage.passwordEnv is required")
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
			return nil, fmt.Errorf("security.allowedTools must contain at least one tool")
		}
		if cfg.Security.MaxApprovalArgumentBytes <= 0 {
			return nil, errors.New("security.maxApprovalArgumentBytes must be greater than zero")
		}
		if len(cfg.Security.SensitiveArgumentKeys) == 0 {
			return nil, errors.New("security.sensitiveArgumentKeys must contain at least one key")
		}
		if cfg.Security.ApprovalTTLSeconds <= 0 {
			return nil, fmt.Errorf(
				"security.approvalTTLSeconds must be greater than zero",
			)
		}

		if !cfg.Auth.Enabled {
			return nil, fmt.Errorf("auth.enabled must be true")
		}
		if len(cfg.Auth.APIKeys) == 0 {
			return nil, fmt.Errorf("auth.apiKeys must contain at least one API key")
		}

		for _, key := range cfg.Auth.APIKeys {
			if key.Subject == "" {
				return nil, fmt.Errorf("auth.apiKeys.subject must be set")
			}
			if key.KeyEnv == "" {
				return nil, fmt.Errorf(
					"auth API key %q has an empty keyEnv",
					key.Subject,
				)
			}
			switch key.Role {
			case "agent", "approver", "admin":
			default:
				return nil, fmt.Errorf(
					"auth API key %q has invalid role %q",
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
					"auth.anonymous.role %q is not a known role",
					cfg.Auth.Anonymous.Role,
				)
			}
			if cfg.Auth.Anonymous.SecretEnv == "" {
				return nil, fmt.Errorf(
					"auth.anonymous.secretEnv is required when anonymous access is enabled",
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
					"auth.anonymous.ttlHours must be greater than zero",
				)
			}
			if cfg.Auth.Anonymous.TTLHours > maxAnonymousTTLHours {
				return nil, fmt.Errorf(
					"auth.anonymous.ttlHours must not exceed %d",
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
					"auth.accounts.secretEnv is required when accounts are enabled",
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
					"auth.accounts.ttlHours must be greater than zero",
				)
			}
			if cfg.Auth.Accounts.TTLHours > maxAccountTTLHours {
				return nil, fmt.Errorf(
					"auth.accounts.ttlHours must not exceed %d",
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
					"auth.accounts.bcryptCost must be 0 (default) or between %d and %d",
					minAccountBcryptCost,
					maxAccountBcryptCost,
				)
			}
		}

		if cfg.Auth.UserSync.Enabled {
			if !cfg.Auth.Accounts.Enabled {
				return nil, fmt.Errorf("auth.userSync requires auth.accounts.enabled")
			}
			if cfg.Auth.UserSync.Type == "" {
				return nil, fmt.Errorf("auth.userSync.type is required when user sync is enabled")
			}
		}

		if cfg.Runtime.ReadTimeoutSeconds <= 0 {
			return nil, fmt.Errorf("runtime.readTimeoutSeconds must be greater than zero")
		}

		if cfg.Runtime.WriteTimeoutSeconds <= 0 {
			return nil, fmt.Errorf("runtime.writeTimeoutSeconds must be greater than zero")
		}

		if cfg.Runtime.IdleTimeoutSeconds <= 0 {
			return nil, fmt.Errorf("runtime.idleTimeoutSeconds must be greater than zero")
		}

		if cfg.Runtime.MaxRequestBodyBytes <= 0 {
			return nil, fmt.Errorf("runtime.maxRequestBodyBytes must be greater than zero")
		}
		if cfg.Knowledge.MaxDocumentBytes <= 0 {
			return nil, fmt.Errorf("knowledge.maxDocumentBytes must be greater than zero")
		}
		if cfg.Runtime.MaxRequestBodyBytes < cfg.Knowledge.MaxDocumentBytes {
			return nil, fmt.Errorf(
				"runtime.maxRequestBodyBytes must be at least knowledge.maxDocumentBytes",
			)
		}
		if cfg.Knowledge.MaxUploadRequestBytes <= 0 {
			return nil, fmt.Errorf(
				"knowledge.maxUploadRequestBytes must be greater than zero",
			)
		}
		if cfg.Knowledge.MaxUploadRequestBytes < cfg.Knowledge.MaxDocumentBytes {
			return nil, fmt.Errorf(
				"knowledge.maxUploadRequestBytes must be at least knowledge.maxDocumentBytes",
			)
		}
		if cfg.Knowledge.MaxUploadFiles <= 0 {
			return nil, fmt.Errorf(
				"knowledge.maxUploadFiles must be greater than zero",
			)
		}
	}

	switch cfg.Observability.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return nil, fmt.Errorf(
			"observability.logLevel must be debug, info, warn, or error",
		)
	}
	if cfg.Observability.ServiceName == "" {
		return nil, fmt.Errorf(
			"observability.serviceName is required",
		)
	}

	if cfg.Observability.Environment == "" {
		return nil, fmt.Errorf(
			"observability.environment is required",
		)
	}

	if cfg.Observability.OTLPEndpoint == "" {
		return nil, fmt.Errorf(
			"observability.otlpEndpoint is required",
		)
	}

	if cfg.Observability.TraceSampleRatio < 0 ||
		cfg.Observability.TraceSampleRatio > 1 {
		return nil, fmt.Errorf(
			"observability.traceSampleRatio must be between 0 and 1",
		)
	}

	if cfg.Observability.LogFilePath == "" {
		return nil, fmt.Errorf(
			"observability.logFilePath is required",
		)
	}

	if cfg.Observability.LogMaxSizeMB <= 0 {
		return nil, fmt.Errorf(
			"observability.logMaxSizeMB must be greater than zero",
		)
	}

	if cfg.Observability.LogMaxBackups <= 0 {
		return nil, fmt.Errorf(
			"observability.logMaxBackups must be greater than zero",
		)
	}

	if cfg.Observability.LogMaxAgeDays <= 0 {
		return nil, fmt.Errorf(
			"observability.logMaxAgeDays must be greater than zero",
		)
	}

	if profile != profileWorker {
		switch cfg.Execution.Mode {
		case "disabled", "docker", "local":
		default:
			return nil, fmt.Errorf(
				"execution.mode must be docker or local",
			)
		}

		if cfg.Execution.Mode == "docker" &&
			cfg.Server.Host != "127.0.0.1" &&
			cfg.Server.Host != "localhost" {
			if cfg.Execution.Image == "" {
				return nil, fmt.Errorf(
					"execution.image is required in docker mode",
				)
			}
			if cfg.Execution.User == "" {
				return nil, fmt.Errorf(
					"execution.user is required in docker mode",
				)
			}
			if cfg.Execution.MemoryLimit == "" {
				return nil, fmt.Errorf(
					"execution.memoryLimit is required in docker mode",
				)
			}
			if cfg.Execution.CPULimit == "" {
				return nil, fmt.Errorf(
					"execution.cpuLimit is required in docker mode",
				)
			}
			if cfg.Execution.PIDsLimit <= 0 {
				return nil, fmt.Errorf(
					"execution.pidsLimit must be greater than zero",
				)
			}
		}

		if cfg.Execution.Mode == "local" &&
			!cfg.Execution.AllowLocalRunner {
			return nil, fmt.Errorf(
				"local execution requires execution.allowLocalRunner=true",
			)
		}
	}

	if profile != profileAPI {
		if cfg.Knowledge.Root == "" {
			return nil, fmt.Errorf("knowledge.root is required")
		}

		if cfg.Knowledge.MaxDocumentBytes <= 0 {
			return nil, fmt.Errorf(
				"knowledge.maxDocumentBytes must be greater than zero",
			)
		}

		if cfg.Knowledge.ChunkSizeCharacters <= 0 {
			return nil, fmt.Errorf(
				"knowledge.chunkSizeCharacters must be greater than zero",
			)
		}

		if cfg.Knowledge.ChunkOverlapChars < 0 ||
			cfg.Knowledge.ChunkOverlapChars >=
				cfg.Knowledge.ChunkSizeCharacters {
			return nil, fmt.Errorf(
				"knowledge.chunkOverlapCharacters must be non-negative and smaller than chunkSizeCharacters",
			)
		}

		if cfg.Knowledge.MaxChunksPerDoc <= 0 {
			return nil, fmt.Errorf(
				"knowledge.maxChunksPerDocument must be greater than zero",
			)
		}
	}

	if profile != profileWorker {
		if cfg.Knowledge.DefaultTopK <= 0 ||
			cfg.Knowledge.DefaultTopK > cfg.Knowledge.MaxTopK {
			return nil, fmt.Errorf(
				"knowledge.defaultTopK must be between 1 and maxTopK",
			)
		}
	}

	if cfg.Embedding.Model == "" {
		return nil, fmt.Errorf("embedding.model is required")
	}

	if cfg.Embedding.Dimensions <= 0 {
		return nil, fmt.Errorf(
			"embedding.dimensions must be greater than zero",
		)
	}

	if cfg.Embedding.BatchSize <= 0 {
		return nil, fmt.Errorf(
			"embedding.batchSize must be greater than zero",
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
			"maintenance.cleanupIntervalSeconds must be greater than zero",
		)
	}

	if cfg.Maintenance.ApprovalRetentionHours <= 0 {
		return nil, fmt.Errorf(
			"maintenance.approvalRetentionHours must be greater than zero",
		)
	}

	if cfg.Maintenance.CheckpointRetentionHours <= 0 {
		return nil, fmt.Errorf(
			"maintenance.checkpointRetentionHours must be greater than zero",
		)
	}

	if cfg.Maintenance.TurnRetentionHours <= 0 {
		return nil, fmt.Errorf(
			"maintenance.turnRetentionHours must be greater than zero",
		)
	}

	if cfg.Maintenance.CleanupBatchSize <= 0 {
		return nil, fmt.Errorf(
			"maintenance.cleanupBatchSize must be greater than zero",
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
				"retrieval.rrfSmoothing must be greater than zero",
			)
		}

		if cfg.Retrieval.VectorCandidateLimit <
			cfg.Knowledge.MaxTopK {
			return nil, fmt.Errorf(
				"retrieval.vectorCandidateLimit must be at least knowledge.maxTopK",
			)
		}

		if cfg.Retrieval.KeywordCandidateLimit <
			cfg.Knowledge.MaxTopK {
			return nil, fmt.Errorf(
				"retrieval.keywordCandidateLimit must be at least knowledge.maxTopK",
			)
		}

		if cfg.Retrieval.ExactCandidateLimit <
			cfg.Knowledge.MaxTopK {
			return nil, fmt.Errorf(
				"retrieval.exactCandidateLimit must be at least knowledge.maxTopK",
			)
		}

		if cfg.Retrieval.EnableRerank {
			if cfg.Retrieval.MaxRerankCandidates <= 0 {
				return nil, fmt.Errorf(
					"retrieval.maxRerankCandidates must be greater than zero when rerank is enabled",
				)
			}
			if strings.TrimSpace(cfg.Retrieval.RerankEndpoint) == "" {
				return nil, fmt.Errorf(
					"retrieval.rerankEndpoint is required when rerank is enabled",
				)
			}
			if cfg.Retrieval.RerankTimeoutMS < 0 {
				return nil, fmt.Errorf("retrieval.rerankTimeoutMS must not be negative")
			}
		}
	}

	if profile != profileAPI {
		if cfg.Indexer.BatchSize <= 0 {
			return nil, fmt.Errorf(
				"indexer.batchSize must be greater than zero",
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
			return nil, fmt.Errorf("asynq.enabled must be true for worker")
		}
		consumesIndex := false
		for _, q := range cfg.Asynq.Queues {
			consumesIndex = consumesIndex || q.Name == "index"
		}
		if !consumesIndex {
			return nil, fmt.Errorf("worker asynq.queues must include the index queue")
		}
		if cfg.Milvus.Address == "" || cfg.Milvus.Collection == "" {
			return nil, fmt.Errorf("worker milvus.address and milvus.collection are required")
		}
	}

	if cfg.Asynq.Enabled {
		if err := validateAsynq(cfg.Asynq, profile != profileAPI); err != nil {
			return nil, err
		}
	}

	return &cfg, nil
}

// validateES 只在 es.address 非空时执行。
//
// 校验口径是「配了一半比没配更危险」：地址配上、索引名或口令没配，进程能起来，
// 但要到第一个分块写不进去、或者第一次关键字查询静默返回空结果时才暴露 ——
// 那时人已经在排查检索质量了，不会想到是配置缺字段。
func validateES(cfg ESConfig) error {
	for _, addr := range cfg.Address {
		if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
			// 裸 host:port 也能被客户端接受，但会被当成 http，托管集群上会
			// 以「连接被重置」这种和真实原因无关的方式失败。
			return fmt.Errorf("es.address %q must start with http:// or https://", addr)
		}
	}
	if cfg.Index == "" {
		return fmt.Errorf("es.index is required when es.address is set")
	}
	if cfg.Analyzer == "" {
		return fmt.Errorf("es.analyzer is required when es.address is set")
	}
	if cfg.BulkSize <= 0 {
		return fmt.Errorf("es.bulkSize must be greater than zero")
	}
	// 映射文件是检索面的唯一定义处：没有它，进程连「该写哪些字段」都不知道。
	// 在这里拦下来，好过等到 es.New 里报错时还带着一个已经连上的集群连接。
	if cfg.MappingFile == "" {
		return fmt.Errorf(
			"es.mappingFile is required when es.address is set; " +
				"see configs/es/chunk_mapping.json for a working example",
		)
	}
	if _, err := os.Stat(cfg.MappingFile); err != nil {
		return fmt.Errorf(
			"es.mappingFile %q is not readable: %w（路径相对进程的工作目录，通常是仓库根）",
			cfg.MappingFile, err,
		)
	}
	// basic auth 是 user + password 成对的：只配 user 不配口令只会拿到 401。
	if cfg.Username != "" {
		if cfg.PasswordEnv == "" {
			return fmt.Errorf("es.passwordEnv is required when es.username is set")
		}
		if cfg.Password == "" {
			return fmt.Errorf("environment variable %s is required", cfg.PasswordEnv)
		}
	}
	return nil
}

// validateAsynq 只在 asynq.enabled=true 时执行：关掉队列意味着整个索引链路
// 停摆，那种情况下再校验连接参数只会挡住一次有意为之的排障启动。
func validateAsynq(cfg AsynqConfig, consumer bool) error {
	if cfg.Redis.Addr == "" {
		return fmt.Errorf("asynq.redis.addr is required")
	}
	if cfg.Redis.DB < 0 {
		return fmt.Errorf("asynq.redis.db must not be negative")
	}
	if cfg.Redis.PasswordEnv != "" && cfg.Redis.Password == "" {
		return fmt.Errorf(
			"environment variable %s is required",
			cfg.Redis.PasswordEnv,
		)
	}
	if consumer {
		if len(cfg.Queues) == 0 {
			return fmt.Errorf("asynq.queues must contain at least one queue")
		}
		for _, queue := range cfg.Queues {
			if queue.Name == "" {
				return fmt.Errorf("asynq.queues.name must be set")
			}
			if queue.Weight <= 0 {
				return fmt.Errorf(
					"asynq queue %q must have a positive weight",
					queue.Name,
				)
			}
		}
		if cfg.Concurrency <= 0 {
			return fmt.Errorf("asynq.concurrency must be greater than zero")
		}
	}
	if cfg.MaxRetries < 0 {
		return fmt.Errorf("asynq.maxRetries must not be negative")
	}
	if consumer {
		if cfg.RetryDelaySeconds <= 0 {
			return fmt.Errorf("asynq.retryDelaySeconds must be greater than zero")
		}
		if cfg.MaxRetryDelaySeconds < cfg.RetryDelaySeconds {
			return fmt.Errorf(
				"asynq.maxRetryDelaySeconds must be >= retryDelaySeconds",
			)
		}
		if cfg.ShutdownTimeoutSeconds <= 0 {
			return fmt.Errorf(
				"asynq.shutdownTimeoutSeconds must be greater than zero",
			)
		}
	}
	return nil
}
