package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const (
	DefaultAPIConfig          = "./configs/api/config.yaml"
	DefaultAPITransportConfig = "./configs/api/restapi.yaml"
	DefaultWorkerConfig       = "./configs/worker/config.yaml"
)

type configProfile string

const (
	profileLegacy configProfile = "legacy"
	profileAPI    configProfile = "api"
	profileWorker configProfile = "worker"
)

// Each entry point has an independent override. EINO_CONFIG is intentionally
// not a fallback: a leftover shared override must not make both processes read
// the same file again.
func APIConfigPath() string { return configPath("EINO_API_CONFIG", DefaultAPIConfig) }
func APITransportConfigPath() string {
	return configPath("EINO_REST_CONFIG", DefaultAPITransportConfig)
}
func WorkerConfigPath() string { return configPath("EINO_WORKER_CONFIG", DefaultWorkerConfig) }
func configPath(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// The struct remains shared by application components, but serialized profiles
// cannot contain settings owned by the other process. KnownFields also catches
// misspellings before a process opens any external connection.
func validateProfileSections(data []byte, profile configProfile) error {
	var sections map[string]yaml.Node
	if err := yaml.Unmarshal(data, &sections); err != nil {
		return err
	}
	allowed := map[string]bool{
		"storage": true, "observability": true, "knowledge": true,
		"embedding": true, "milvus": true, "es": true, "asynq": true,
	}
	if profile == profileAPI {
		for _, key := range []string{"server", "runtime", "agent", "model", "workspace", "context", "skills", "security", "auth", "execution", "maintenance", "retrieval"} {
			allowed[key] = true
		}
	} else {
		allowed["indexer"] = true
	}
	for key, node := range sections {
		if !allowed[key] {
			return fmt.Errorf("%s config must not contain %q section", profile, key)
		}
		var forbidden []string
		if key == "observability" {
			forbidden = []string{"workerServiceName"}
		}
		if profile == profileAPI {
			switch key {
			case "knowledge":
				forbidden = []string{"root", "chunkSizeCharacters", "chunkOverlapCharacters", "maxChunksPerDocument"}
			case "asynq":
				forbidden = []string{"queues", "concurrency", "retryDelaySeconds", "maxRetryDelaySeconds", "shutdownTimeoutSeconds"}
			}
		} else if key == "knowledge" {
			forbidden = []string{"defaultTopK", "maxTopK", "maxQueryCharacters", "maxResultBytes", "recallGrouping"}
		}
		if profile == profileWorker {
			if key == "milvus" {
				forbidden = []string{"topKCandidate", "searchTimeoutMS"}
			}
			if key == "observability" {
				forbidden = []string{"workerServiceName", "metricsEnabled"}
			}
		}
		for i := 0; node.Kind == yaml.MappingNode && i < len(node.Content); i += 2 {
			for _, name := range forbidden {
				if node.Content[i].Value == name {
					return fmt.Errorf("%s config must not contain %s.%s", profile, key, name)
				}
			}
		}
	}
	return nil
}
