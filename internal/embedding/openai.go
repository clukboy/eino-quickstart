package embedding

import "net/http"

type OpenAIConfig struct {
	BaseURL    string
	APIKey     string
	Model      string
	Dimensions int
	BatchSize  int
	Client     *http.Client
}

type OpenAIEmbedder struct {
	baseURL    string
	apiKey     string
	model      string
	dimensions int
	batchSize  int
	client     *http.Client
}
