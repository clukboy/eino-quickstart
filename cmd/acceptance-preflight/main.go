// Command acceptance-preflight validates an isolated API/worker environment
// without creating schema, collections, indexes, or acceptance resources.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"eino-quickstart/internal/platform/config"

	"github.com/hibiken/asynq"
	_ "github.com/lib/pq"
	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/milvusclient"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
)

const (
	statusPass    = "PASS"
	statusBlocked = "BLOCKED"
	statusFail    = "FAIL"
)

type check struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Detail  string `json:"detail,omitempty"`
}

type report struct {
	Plan       string            `json:"plan"`
	Mode       string            `json:"mode"`
	RunID      string            `json:"run_id"`
	StartedAt  time.Time         `json:"started_at"`
	FinishedAt time.Time         `json:"finished_at"`
	Config     map[string]string `json:"config"`
	Resources  map[string]string `json:"resources"`
	Checks     []check           `json:"checks"`
	Status     string            `json:"status"`
	Message    string            `json:"message"`
	Fixture    string            `json:"fixture"`
}

type runner struct {
	ctx     context.Context
	report  *report
	client  *http.Client
	timeout time.Duration
}

func main() {
	var (
		mode           = flag.String("mode", "degraded", "retrieval mode: complete or degraded")
		apiConfig      = flag.String("api-config", config.DefaultAPIConfig, "API business config")
		workerConfig   = flag.String("worker-config", config.DefaultWorkerConfig, "worker config")
		restConfig     = flag.String("rest-config", config.DefaultAPITransportConfig, "API HTTP config")
		reportPath     = flag.String("report", "", "JSON report path")
		probeEmbedding = flag.Bool("probe-embedding", false, "send one embedding request to verify provider and vector dimension")
		timeout        = flag.Duration("timeout", 8*time.Second, "timeout per external check")
	)
	flag.Parse()

	if *mode != "complete" && *mode != "degraded" {
		fmt.Fprintln(os.Stderr, "mode must be complete or degraded")
		os.Exit(2)
	}
	if *timeout <= 0 {
		fmt.Fprintln(os.Stderr, "timeout must be positive")
		os.Exit(2)
	}

	r := &report{
		Plan:      "P02",
		Mode:      *mode,
		RunID:     time.Now().UTC().Format("20060102T150405.000000000Z"),
		StartedAt: time.Now().UTC(),
		Config: map[string]string{
			"api":    filepath.Clean(*apiConfig),
			"worker": filepath.Clean(*workerConfig),
			"rest":   filepath.Clean(*restConfig),
		},
		Resources: map[string]string{},
		Checks:    []check{},
	}
	ctx := context.Background()

	apiCfg, workerCfg, restCfg, err := loadConfigs(*apiConfig, *workerConfig, *restConfig)
	if err != nil {
		r.add(statusBlocked, "config", "配置未通过角色校验", err.Error())
		finish(r, statusBlocked, "修复配置或环境变量后重新执行 P02", *reportPath)
		os.Exit(2)
	}
	r.Resources["database"] = fmt.Sprintf("%s:%d/%s", workerCfg.Storage.Host, workerCfg.Storage.Port, workerCfg.Storage.DBName)
	r.Resources["redis"] = fmt.Sprintf("%s/db-%d", workerCfg.Asynq.Redis.Addr, workerCfg.Asynq.Redis.DB)
	r.Resources["milvus"] = fmt.Sprintf("%s/%s", workerCfg.Milvus.Address, workerCfg.Milvus.Collection)
	r.Resources["es"] = strings.Join(workerCfg.ES.Address, ",")
	r.Resources["embedding"] = fmt.Sprintf("%s model=%s dimensions=%d", workerCfg.Embedding.BaseURL, workerCfg.Embedding.Model, workerCfg.Embedding.Dimensions)

	r.add(statusPass, "config", "API、worker、HTTP 配置结构和字段边界通过", "密钥未写入报告")
	apiBase := fmt.Sprintf("http://%s:%d", restCfg.Host, restCfg.Port)
	if restCfg.Host == "" {
		apiBase = fmt.Sprintf("http://127.0.0.1:%d", restCfg.Port)
	}
	runner := &runner{ctx: ctx, report: r, client: &http.Client{Timeout: *timeout}, timeout: *timeout}
	runner.checkAPI(apiBase)
	runner.checkPostgres(workerCfg.Storage)
	redisReady := runner.checkRedis(workerCfg.Asynq.Redis, workerCfg.Asynq.Queues)
	runner.checkMilvus(workerCfg.Milvus, workerCfg.Embedding.Dimensions)
	runner.checkEmbedding(workerCfg.Embedding, *probeEmbedding)
	runner.checkES(workerCfg.ES, *mode == "complete")

	// Worker discovery is deliberately read from Asynq's server registry. A Redis
	// TCP connection alone is never treated as proof that indexing can consume.
	runner.checkWorker(workerCfg.Asynq, redisReady)
	_ = apiCfg // Loading API config above is the role/secret validation we need here.

	fixture := filepath.Join(filepath.Dir(*reportPath), "fixture.md")
	if *reportPath != "" {
		if err := writeFixture(fixture, r.RunID); err != nil {
			r.add(statusFail, "fixture", "无法生成本轮唯一语料", err.Error())
		} else {
			r.Fixture = fixture
			r.add(statusPass, "fixture", "已生成本轮唯一验收语料和建议数据集标记", fixture)
		}
	}

	status, message := r.finalStatus()
	finish(r, status, message, *reportPath)
	if status == statusPass {
		return
	}
	if status == statusBlocked {
		os.Exit(2)
	}
	os.Exit(1)
}

func loadConfigs(apiPath, workerPath, restPath string) (*config.Config, *config.Config, *rest.RestConf, error) {
	apiCfg, err := config.LoadAPI(apiPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("API config: %w", err)
	}
	workerCfg, err := config.LoadWorker(workerPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("worker config: %w", err)
	}
	var restCfg rest.RestConf
	if err := conf.Load(restPath, &restCfg); err != nil {
		return nil, nil, nil, fmt.Errorf("REST config: %w", err)
	}
	if workerCfg.Storage.DBName != apiCfg.Storage.DBName || workerCfg.Storage.Host != apiCfg.Storage.Host || workerCfg.Storage.Port != apiCfg.Storage.Port {
		return nil, nil, nil, errors.New("API and worker storage endpoints differ")
	}
	if workerCfg.Asynq.Redis.Addr != apiCfg.Asynq.Redis.Addr || workerCfg.Asynq.Redis.DB != apiCfg.Asynq.Redis.DB {
		return nil, nil, nil, errors.New("API and worker Redis endpoint/DB differ")
	}
	return apiCfg, workerCfg, &restCfg, nil
}

func (r *report) add(status, name, summary, detail string) {
	r.Checks = append(r.Checks, check{Name: name, Status: status, Summary: summary, Detail: detail})
	fmt.Printf("%s %-12s %s", status, name, summary)
	if detail != "" {
		fmt.Printf(" (%s)", detail)
	}
	fmt.Println()
}

func (r *report) finalStatus() (string, string) {
	for _, c := range r.Checks {
		if c.Status == statusFail {
			return statusFail, "存在配置或依赖检查失败；查看报告修复后重试"
		}
	}
	for _, c := range r.Checks {
		if c.Status == statusBlocked {
			return statusBlocked, "环境未就绪；没有把 Redis/worker/检索链路伪装成可写入"
		}
	}
	return statusPass, "P02 预检通过；API、worker、依赖和检索模式均满足当前验收配置"
}

func finish(r *report, status, message, path string) {
	r.Status = status
	r.Message = message
	r.FinishedAt = time.Now().UTC()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "write report directory: %v\n", err)
		return
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode report: %v\n", err)
		return
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "write report: %v\n", err)
		return
	}
	fmt.Printf("report: %s\n", path)
}

func (r *runner) checkAPI(base string) {
	for _, path := range []string{"/health", "/ready"} {
		request, err := http.NewRequestWithContext(r.ctx, http.MethodGet, strings.TrimRight(base, "/")+path, nil)
		if err != nil {
			r.report.add(statusFail, "api"+path, "API 地址无效", err.Error())
			continue
		}
		response, err := r.client.Do(request)
		if err != nil {
			r.report.add(statusBlocked, "api"+path, "API 不可达", err.Error())
			continue
		}
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			r.report.add(statusBlocked, "api"+path, "API 返回非 200", fmt.Sprintf("HTTP %d", response.StatusCode))
			continue
		}
		r.report.add(statusPass, "api"+path, "API 端点可用", base+path)
	}
}

func (r *runner) checkPostgres(c config.Storage) {
	u := &url.URL{Scheme: "postgres", User: url.UserPassword(c.Username, c.Password), Host: fmt.Sprintf("%s:%d", c.Host, c.Port), Path: c.DBName, RawQuery: url.Values{"sslmode": []string{c.SSLMode}}.Encode()}
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		r.report.add(statusBlocked, "postgres", "PostgreSQL 客户端初始化失败", err.Error())
		return
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(r.ctx, r.timeout)
	defer cancel()
	var database, user string
	if err := db.QueryRowContext(ctx, "SELECT current_database(), current_user").Scan(&database, &user); err != nil {
		r.report.add(statusBlocked, "postgres", "PostgreSQL 连接或权限失败", err.Error())
		return
	}
	r.report.add(statusPass, "postgres", "PostgreSQL 可认证并允许只读探针", fmt.Sprintf("database=%s user=%s", database, user))
}

func (r *runner) checkRedis(c config.AsynqRedisConfig, queues []config.AsynqQueueConfig) bool {
	client := redis.NewClient(&redis.Options{
		Addr:         c.Addr,
		Username:     c.Username,
		Password:     c.Password,
		DB:           c.DB,
		MaxRetries:   0,
		DialTimeout:  r.timeout,
		ReadTimeout:  r.timeout,
		WriteTimeout: r.timeout,
	})
	defer client.Close()
	ctx, cancel := context.WithTimeout(r.ctx, r.timeout)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		r.report.add(statusBlocked, "redis", "Redis 认证或连接失败", err.Error())
		return false
	}
	probeKey := "eino:acceptance:preflight:" + r.report.RunID
	if err := client.Set(ctx, probeKey, "ok", time.Second).Err(); err != nil {
		r.report.add(statusBlocked, "redis", "Redis 可连接但不可写入目标 DB", err.Error())
		return false
	}
	defer client.Del(context.Background(), probeKey)
	r.report.Resources["redis_probe_key"] = probeKey
	queueNames := make([]string, 0, len(queues))
	for _, q := range queues {
		queueNames = append(queueNames, q.Name)
	}
	r.report.add(statusPass, "redis", "Redis 可认证且目标 DB 可写", fmt.Sprintf("db=%d queues=%s", c.DB, strings.Join(queueNames, ",")))
	return true
}

func (r *runner) checkWorker(c config.AsynqConfig, redisReady bool) {
	if !redisReady {
		r.report.add(statusBlocked, "worker", "未执行 worker 注册检查", "Redis 预检未通过；修复 Redis 后重跑")
		return
	}
	inspector := asynq.NewInspector(asynq.RedisClientOpt{Network: "tcp", Addr: c.Redis.Addr, Username: c.Redis.Username, Password: c.Redis.Password, DB: c.Redis.DB})
	defer inspector.Close()
	servers, err := inspector.Servers()
	if err != nil {
		r.report.add(statusBlocked, "worker", "无法从 Redis 确认 worker 注册状态", err.Error())
		return
	}
	for _, server := range servers {
		if server.Queues["index"] > 0 {
			r.report.add(statusPass, "worker", "worker 已注册并消费 index 队列", fmt.Sprintf("server=%s host=%s pid=%d status=%s", server.ID, server.Host, server.PID, server.Status))
			return
		}
	}
	r.report.add(statusBlocked, "worker", "Redis 可用但没有消费 index 的 worker", "请启动隔离 worker；不能把 API 可用当成索引可写")
}

func (r *runner) checkMilvus(c config.MilvusConfig, dimensions int) {
	ctx, cancel := context.WithTimeout(r.ctx, r.timeout)
	defer cancel()
	client, err := milvusclient.New(ctx, &milvusclient.ClientConfig{Address: c.Address})
	if err != nil {
		r.report.add(statusBlocked, "milvus", "Milvus 连接失败", err.Error())
		return
	}
	defer client.Close(context.Background())
	collection, err := client.DescribeCollection(ctx, milvusclient.NewDescribeCollectionOption(c.Collection))
	if err != nil {
		r.report.add(statusBlocked, "milvus", "Milvus collection 不存在或无读取权限", err.Error())
		return
	}
	foundDim := ""
	for _, field := range collection.Schema.Fields {
		if field.Name == "embedding" {
			if field.DataType != entity.FieldTypeFloatVector {
				r.report.add(statusFail, "milvus", "embedding 字段类型错误", field.DataType.Name())
				return
			}
			foundDim = field.TypeParams[entity.TypeParamDim]
		}
	}
	wantDim := strconv.Itoa(dimensions)
	if foundDim != wantDim {
		r.report.add(statusFail, "milvus", "embedding 维度不匹配", fmt.Sprintf("configured=%s actual=%s", wantDim, foundDim))
		return
	}
	state, err := client.GetLoadState(ctx, milvusclient.NewGetLoadStateOption(c.Collection))
	if err != nil {
		r.report.add(statusBlocked, "milvus", "collection 加载状态不可读", fmt.Sprintf("error=%v", err))
		return
	}
	if state.State != entity.LoadStateLoaded {
		r.report.add(statusBlocked, "milvus", "collection 未加载就绪", fmt.Sprintf("state=%v", state.State))
		return
	}
	r.report.add(statusPass, "milvus", "Milvus collection 存在、维度匹配且已加载", fmt.Sprintf("collection=%s dimensions=%s", c.Collection, foundDim))
}

func (r *runner) checkEmbedding(c config.EmbeddingConfig, probe bool) {
	if !probe {
		r.report.add(statusBlocked, "embedding", "未执行 embedding 维度探针", "为避免意外计费，显式传入 -probe-embedding")
		return
	}
	endpoint := strings.TrimRight(c.BaseURL, "/") + "/embeddings"
	payload, _ := json.Marshal(map[string]any{"model": c.Model, "input": []string{"eino-acceptance-p02-dimension-probe"}})
	request, err := http.NewRequestWithContext(r.ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		r.report.add(statusFail, "embedding", "embedding URL 无效", err.Error())
		return
	}
	request.Header.Set("Content-Type", "application/json")
	apiKey := strings.TrimSpace(os.Getenv(c.APIKeyEnv))
	if apiKey == "" {
		r.report.add(statusBlocked, "embedding", "embedding API key 未设置", "环境变量 "+c.APIKeyEnv+" 为空")
		return
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	response, err := r.client.Do(request)
	if err != nil {
		r.report.add(statusBlocked, "embedding", "embedding 服务不可达", err.Error())
		return
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		r.report.add(statusBlocked, "embedding", "embedding 请求失败", fmt.Sprintf("HTTP %d", response.StatusCode))
		return
	}
	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil || len(result.Data) == 0 {
		r.report.add(statusFail, "embedding", "embedding 响应缺少向量", "响应格式无法解析")
		return
	}
	got := len(result.Data[0].Embedding)
	if got != c.Dimensions {
		r.report.add(statusFail, "embedding", "embedding 返回维度不匹配", fmt.Sprintf("configured=%d actual=%d", c.Dimensions, got))
		return
	}
	r.report.add(statusPass, "embedding", "embedding 服务可调用且返回维度匹配", fmt.Sprintf("dimensions=%d", got))
}

func (r *runner) checkES(c config.ESConfig, required bool) {
	if len(c.Address) == 0 {
		if required {
			r.report.add(statusBlocked, "es", "完整检索模式要求配置 ES", "使用 --mode degraded 才允许走 PostgreSQL 降级")
		} else {
			r.report.add(statusPass, "es", "ES 未启用，按降级模式允许", "关键词通道将使用 PostgreSQL 回落")
		}
		return
	}
	for _, address := range c.Address {
		request, err := http.NewRequestWithContext(r.ctx, http.MethodGet, strings.TrimRight(address, "/")+"/", nil)
		if err != nil {
			r.report.add(statusFail, "es", "ES 地址无效", err.Error())
			return
		}
		if c.Username != "" {
			request.SetBasicAuth(c.Username, c.Password)
		}
		response, err := r.client.Do(request)
		if err != nil {
			r.report.add(statusBlocked, "es", "ES 不可达", err.Error())
			return
		}
		response.Body.Close()
		if response.StatusCode >= 400 {
			r.report.add(statusBlocked, "es", "ES 认证或权限失败", fmt.Sprintf("HTTP %d", response.StatusCode))
			return
		}
	}
	r.report.add(statusPass, "es", "ES 地址可达", strings.Join(c.Address, ","))
}

func writeFixture(path, runID string) error {
	if path == "" {
		return nil
	}
	content := fmt.Sprintf("---\ntitle: P02 acceptance fixture\n---\n\n唯一验收标记：eino-acceptance-p02-%s\n\n该文件只用于本轮隔离环境上传验证。\n", strings.ReplaceAll(runID, ":", "-"))
	return os.WriteFile(path, []byte(content), 0o600)
}
