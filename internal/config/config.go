package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

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
	Maintenance MaintenanceConfig `yaml:"maintenance"`
	Retrieval   RetrievalConfig   `yaml:"retrieval"`
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
	Password    string `yaml:"password"`
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
	Environment string `yaml:"environment"`

	OTLPEndpoint     string  `yaml:"otlpEndpoint"`
	OTLPInsecure     bool    `yaml:"otlpInsecure"`
	TraceSampleRatio float64 `yaml:"traceSampleRatio"`

	LogFilePath   string `yaml:"logFilePath"`
	LogMaxSizeMB  int    `yaml:"logMaxSizeMB"`
	LogMaxBackups int    `yaml:"logMaxBackups"`
	LogMaxAgeDays int    `yaml:"logMaxAgeDays"`
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
	VectorWeight          float64 `yaml:"vectorWeight"`
	KeywordWeight         float64 `yaml:"keywordWeight"`
	RRFSmoothing          int     `yaml:"rrfSmoothing"`
	VectorCandidateLimit  int     `yaml:"vectorCandidateLimit"`
	KeywordCandidateLimit int     `yaml:"keywordCandidateLimit"`
	EnableRerank          bool    `yaml:"enableRerank"`
	MaxRerankCandidates   int     `yaml:"maxRerankCandidates"`
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
			cfg.Retrieval.KeywordWeight == 0) {
		return nil, fmt.Errorf(
			"retrieval.vectorWeight and retrieval.keywordWeight must not both be zero",
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

	if cfg.Retrieval.EnableRerank &&
		cfg.Retrieval.MaxRerankCandidates <= 0 {
		return nil, fmt.Errorf(
			"retrieval.maxRerankCandidates must be greater than zero when rerank is enabled",
		)
	}

	return &cfg, nil
}
