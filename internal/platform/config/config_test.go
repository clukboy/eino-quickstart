package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate repository")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
}

// Use non-secret test values only. Loading config must never contact a service.
func setConfigEnv(t *testing.T, api bool) {
	t.Helper()
	for _, name := range []string{"EINO_MODEL_API_KEY", "EINO_MODEL_BASE_URL", "EINO_MODEL", "EINO_SERVER_PORT", "EINO_WORKSPACE_ROOT"} {
		t.Setenv(name, "")
	}
	for _, name := range []string{"EINO_STORAGE_PASSWORD", "EINO_EMBEDDING_API_KEY", "EINO_ES_PASSWORD"} {
		t.Setenv(name, "config-test-placeholder")
	}
	for _, name := range []string{"EINO_API_KEY_DEVELOPER", "EINO_API_KEY_APPROVER", "EINO_API_KEY_ADMIN", "EINO_ANONYMOUS_SECRET", "EINO_ACCOUNT_SECRET"} {
		value := ""
		if api {
			value = "config-test-placeholder"
		}
		t.Setenv(name, value)
	}
}

func fixture(t *testing.T, relative string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), relative))
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	// Paths in production are relative to the repository working directory.
	if es, ok := cfg["es"].(map[string]any); ok {
		es["mappingFile"] = filepath.Join(repoRoot(t), "configs/es/chunk_mapping.yaml")
	}
	return cfg
}

func writeFixture(t *testing.T, cfg map[string]any) string {
	t.Helper()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestShippedProfilesLoad(t *testing.T) {
	for _, tc := range []struct {
		name, file string
		api        bool
		load       func(string) (*Config, error)
	}{
		{"api", "configs/api/config.yaml", true, LoadAPI},
		{"worker", "configs/worker/config.yaml", false, LoadWorker},
		{"acceptance-api", "configs/acceptance/api.yaml", true, LoadAPI},
		{"acceptance-worker", "configs/acceptance/worker.yaml", false, LoadWorker},
		{"legacy", "configs/config.yaml", true, Load},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setConfigEnv(t, tc.api)
			cfg, err := tc.load(writeFixture(t, fixture(t, tc.file)))
			if err != nil {
				t.Fatal(err)
			}
			if !tc.api && (cfg.Workspace.Root != "" || cfg.Model.APIKey != "" || cfg.Auth.Enabled) {
				t.Fatal("worker acquired API-only settings")
			}
		})
	}
}

func TestWorkerIgnoresAPIEnvironment(t *testing.T) {
	setConfigEnv(t, false)
	t.Setenv("EINO_WORKSPACE_ROOT", filepath.Join(t.TempDir(), "must-not-be-created"))
	t.Setenv("EINO_MODEL_API_KEY", "unrelated-api-key")
	t.Setenv("EINO_SERVER_PORT", "9999")
	cfg, err := LoadWorker(writeFixture(t, fixture(t, "configs/acceptance/worker.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Workspace.Root != "" || cfg.Model.APIKey != "" || cfg.Server.Port != 0 {
		t.Fatal("worker applied API environment overrides")
	}
}

func TestWorkerRejectsMissingRequirements(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		mutate     func(*testing.T, map[string]any)
	}{
		{"storage-secret", "EINO_STORAGE_PASSWORD", func(t *testing.T, m map[string]any) { t.Setenv("EINO_STORAGE_PASSWORD", "") }},
		{"embedding-secret", "EINO_EMBEDDING_API_KEY", func(t *testing.T, m map[string]any) { t.Setenv("EINO_EMBEDDING_API_KEY", "") }},
		{"batch-size", "indexer.batchSize", func(t *testing.T, m map[string]any) { m["indexer"].(map[string]any)["batchSize"] = 0 }},
		{"queue-disabled", "asynq.enabled", func(t *testing.T, m map[string]any) { m["asynq"].(map[string]any)["enabled"] = false }},
		{"wrong-queue", "index queue", func(t *testing.T, m map[string]any) {
			m["asynq"].(map[string]any)["queues"] = []any{map[string]any{"name": "default", "weight": 1}}
		}},
		{"redis-address", "asynq.redis.addr", func(t *testing.T, m map[string]any) {
			m["asynq"].(map[string]any)["redis"].(map[string]any)["addr"] = ""
		}},
		{"concurrency", "asynq.concurrency", func(t *testing.T, m map[string]any) { m["asynq"].(map[string]any)["concurrency"] = 0 }},
		{"milvus-address", "milvus.address", func(t *testing.T, m map[string]any) { m["milvus"].(map[string]any)["address"] = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setConfigEnv(t, false)
			cfg := fixture(t, "configs/acceptance/worker.yaml")
			tc.mutate(t, cfg)
			_, err := LoadWorker(writeFixture(t, cfg))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestProfilesRejectMixedAndMisspelledSettings(t *testing.T) {
	for _, tc := range []struct {
		name, file, section, field, want string
		load                             func(string) (*Config, error)
	}{
		{"worker-model", "worker", "model", "", "model", LoadWorker},
		{"worker-http", "worker", "runtime", "", "runtime", LoadWorker},
		{"worker-search", "worker", "knowledge", "defaultTopK", "knowledge.defaultTopK", LoadWorker},
		{"api-indexer", "api", "indexer", "", "indexer", LoadAPI},
		{"api-consumer", "api", "asynq", "concurrency", "asynq.concurrency", LoadAPI},
		{"api-chunking", "api", "knowledge", "chunkSizeCharacters", "knowledge.chunkSizeCharacters", LoadAPI},
		{"worker-search-tuning", "worker", "milvus", "topKCandidate", "milvus.topKCandidate", LoadWorker},
		{"worker-typo", "worker", "indexer", "batchSzie", "batchSzie", LoadWorker},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setConfigEnv(t, true)
			cfg := fixture(t, "configs/acceptance/"+tc.file+".yaml")
			if tc.field == "" {
				cfg[tc.section] = map[string]any{}
			} else {
				cfg[tc.section].(map[string]any)[tc.field] = 1
			}
			_, err := tc.load(writeFixture(t, cfg))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestProcessOverridesAreIndependent(t *testing.T) {
	t.Setenv("EINO_CONFIG", "/legacy/combined.yaml")
	t.Setenv("EINO_API_CONFIG", "")
	t.Setenv("EINO_WORKER_CONFIG", "")
	t.Setenv("EINO_REST_CONFIG", "")
	if APIConfigPath() != DefaultAPIConfig || WorkerConfigPath() != DefaultWorkerConfig || APITransportConfigPath() != DefaultAPITransportConfig {
		t.Fatal("legacy environment changed process defaults")
	}
	t.Setenv("EINO_API_CONFIG", "/custom/api.yaml")
	t.Setenv("EINO_REST_CONFIG", "/custom/http.yaml")
	if APIConfigPath() != "/custom/api.yaml" || WorkerConfigPath() != DefaultWorkerConfig || APITransportConfigPath() != "/custom/http.yaml" {
		t.Fatal("API override leaked to worker")
	}
	t.Setenv("EINO_WORKER_CONFIG", "/custom/worker.yaml")
	if WorkerConfigPath() != "/custom/worker.yaml" || APIConfigPath() != "/custom/api.yaml" {
		t.Fatal("worker override leaked to API")
	}
}

func TestShippedProcessPairsTargetSameResources(t *testing.T) {
	for _, pair := range [][2]string{{"configs/api/config.yaml", "configs/worker/config.yaml"}, {"configs/acceptance/api.yaml", "configs/acceptance/worker.yaml"}} {
		api, worker := fixture(t, pair[0]), fixture(t, pair[1])
		for _, key := range []string{"storage", "embedding", "es"} {
			if !reflect.DeepEqual(api[key], worker[key]) {
				t.Errorf("%s and %s disagree on %s", pair[0], pair[1], key)
			}
		}
		for _, key := range []string{"address", "collection", "metricType"} {
			if !reflect.DeepEqual(api["milvus"].(map[string]any)[key], worker["milvus"].(map[string]any)[key]) {
				t.Errorf("Milvus %s differs", key)
			}
		}
		if api["knowledge"].(map[string]any)["maxDocumentBytes"] != worker["knowledge"].(map[string]any)["maxDocumentBytes"] {
			t.Error("document limits differ")
		}
		if api["asynq"].(map[string]any)["maxRetries"] != worker["asynq"].(map[string]any)["maxRetries"] {
			t.Error("retry budgets differ")
		}
		if !reflect.DeepEqual(api["asynq"].(map[string]any)["redis"], worker["asynq"].(map[string]any)["redis"]) {
			t.Error("Redis targets differ")
		}
		if api["observability"].(map[string]any)["logFilePath"] == worker["observability"].(map[string]any)["logFilePath"] {
			t.Error("processes share a rotating log file")
		}
	}
	acceptance := fixture(t, "configs/acceptance/worker.yaml")
	if acceptance["storage"].(map[string]any)["dbName"] != "eino_acceptance" || acceptance["asynq"].(map[string]any)["redis"].(map[string]any)["db"] != 15 || acceptance["milvus"].(map[string]any)["collection"] != "eino_acceptance_document_chunks_v1" {
		t.Fatal("acceptance resources are not isolated")
	}
}

func TestAPIStillRequiresAPIConfiguration(t *testing.T) {
	setConfigEnv(t, true)
	t.Setenv("EINO_API_KEY_DEVELOPER", "")
	_, err := LoadAPI(writeFixture(t, fixture(t, "configs/acceptance/api.yaml")))
	if err == nil || !strings.Contains(err.Error(), "EINO_API_KEY_DEVELOPER") {
		t.Fatalf("API validation was bypassed: %v", err)
	}
}
