package analyze

import (
	"os"
	"testing"
)

func TestNewAIAnalyzer(t *testing.T) {
	tests := []struct {
		provider    string
		envKey      string
		envVal      string
		wantBaseURL string
		wantModel   string
	}{
		{
			provider:    "openai",
			envKey:      "OPENAI_API_KEY",
			envVal:      "test-key",
			wantBaseURL: "https://api.openai.com/v1",
			wantModel:   "gpt-4-turbo-preview",
		},
		{
			provider:    "qwen",
			envKey:      "DASHSCOPE_API_KEY",
			envVal:      "test-key",
			wantBaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
			wantModel:   "qwen-max",
		},
		{
			provider:    "deepseek",
			envKey:      "DEEPSEEK_API_KEY",
			envVal:      "test-key",
			wantBaseURL: "https://api.deepseek.com",
			wantModel:   "deepseek-chat",
		},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			os.Setenv(tt.envKey, tt.envVal)
			defer os.Unsetenv(tt.envKey)

			a := NewAIAnalyzer(tt.provider)
			if a.BaseURL != tt.wantBaseURL {
				t.Errorf("NewAIAnalyzer(%q) BaseURL = %q, want %q", tt.provider, a.BaseURL, tt.wantBaseURL)
			}
			if a.Model != tt.wantModel {
				t.Errorf("NewAIAnalyzer(%q) Model = %q, want %q", tt.provider, a.Model, tt.wantModel)
			}
			if a.APIKey != tt.envVal {
				t.Errorf("NewAIAnalyzer(%q) APIKey = %q, want %q", tt.provider, a.APIKey, tt.envVal)
			}
		})
	}
}

func TestNewAIAnalyzer_QwenFallback(t *testing.T) {
	os.Unsetenv("DASHSCOPE_API_KEY")
	os.Setenv("QWEN_API_KEY", "fallback-key")
	defer os.Unsetenv("QWEN_API_KEY")

	a := NewAIAnalyzer("qwen")
	if a.APIKey != "fallback-key" {
		t.Errorf("NewAIAnalyzer(qwen) fallback APIKey = %q, want %q", a.APIKey, "fallback-key")
	}
}
