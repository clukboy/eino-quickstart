package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

	// MappingFile 是检索面的声明文件（JSON 或 YAML），一个索引一份。
	//
	// 独立成文件而不是内联在这里，是因为它描述的是字段名、类型、权重与取值
	// 路径 —— 这些必须与 internal/rag/parser 产出的元数据对齐，改动也常常和
	// 「换了一套产品线」同时发生，值得单独评审与对照。
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

	// Asynq 是文档索引的异步队列。它取代了早期的 queue 段：那时 AsynqConf
	// 只有连接参数，而队列名、重试上限、关闭超时都散在别处，两套配置只有
	// 一套在生效。现在连接参数与队列语义收在同一个段里，见 validateAsynq。
	Asynq AsynqConfig `yaml:"asynq"`
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

type Auth struct {
	Enabled bool           `yaml:"enabled"`
	APIKeys []APIKeyConfig `yaml:"apiKeys"`
}
type APIKeyConfig struct {
	Subject string `yaml:"subject"`
	Role    string `yaml:"role"`
	KeyEnv  string `yaml:"keyEnv"`
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
	Root                string `yaml:"root"`
	MaxDocumentBytes    int    `yaml:"maxDocumentBytes"`
	ChunkSizeCharacters int    `yaml:"chunkSizeCharacters"`
	ChunkOverlapChars   int    `yaml:"chunkOverlapCharacters"`
	MaxChunksPerDoc     int    `yaml:"maxChunksPerDocument"`
	DefaultTopK         int    `yaml:"defaultTopK"`
	MaxTopK             int    `yaml:"maxTopK"`
	MaxQueryCharacters  int    `yaml:"maxQueryCharacters"`
	MaxResultBytes      int    `yaml:"maxResultBytes"`
}

type EmbeddingConfig struct {
	BaseURL    string `yaml:"baseURL"`
	APIKeyEnv  string `yaml:"apiKeyEnv"`
	Model      string `yaml:"model"`
	Dimensions int    `yaml:"dimensions"`
	BatchSize  int    `yaml:"batchSize"`
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

	EnableRerank        bool `yaml:"enableRerank"`
	MaxRerankCandidates int  `yaml:"maxRerankCandidates"`
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

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

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
	if cfg.Storage.PasswordEnv == "" {
		return nil, fmt.Errorf("storage.passwordEnv is required")
	}
	if cfg.Storage.Password == "" {
		return nil, fmt.Errorf(
			"environment variable %s is required",
			cfg.Storage.PasswordEnv,
		)
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

	if cfg.Knowledge.DefaultTopK <= 0 ||
		cfg.Knowledge.DefaultTopK > cfg.Knowledge.MaxTopK {
		return nil, fmt.Errorf(
			"knowledge.defaultTopK must be between 1 and maxTopK",
		)
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

	if cfg.Retrieval.EnableRerank &&
		cfg.Retrieval.MaxRerankCandidates <= 0 {
		return nil, fmt.Errorf(
			"retrieval.maxRerankCandidates must be greater than zero when rerank is enabled",
		)
	}

	if cfg.Indexer.BatchSize <= 0 {
		return nil, fmt.Errorf(
			"indexer.batchSize must be greater than zero",
		)
	}

	// ES 段关着（address 为空）时一律不校验：这条降级路径是设计的一部分，
	// 不该被一段没人用的配置挡住进程启动。
	if cfg.ES.Enabled() {
		if err := validateES(cfg.ES); err != nil {
			return nil, err
		}
	}

	if cfg.Asynq.Enabled {
		if err := validateAsynq(cfg.Asynq); err != nil {
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
func validateAsynq(cfg AsynqConfig) error {
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
	if cfg.MaxRetries < 0 {
		return fmt.Errorf("asynq.maxRetries must not be negative")
	}
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
	return nil
}
