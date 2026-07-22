package main

import (
	"os"
	"testing"
)

func TestViperConfigResolution(t *testing.T) {
	// Create a temp config file
	cfg := []byte("model: config-model\nprovider:\n  base_url: http://config:9999\n")
	tmpFile := "/tmp/test-ouroboros-viper.yaml"
	os.WriteFile(tmpFile, cfg, 0644)
	defer os.Remove(tmpFile)

	// Test 1: config file sets model when --model not explicitly passed
	t.Run("config sets model", func(t *testing.T) {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"--config", tmpFile, "-p", "hello"})
		// Execute will fail (no ollama), but PreRunE should have run initConfig
		cmd.Execute()
		modelVal := cmd.Flags().Lookup("model").Value.String()
		if modelVal != "config-model" {
			t.Errorf("expected model=config-model from config, got %q", modelVal)
		}
	})

	// Test 2: explicit --model overrides config
	t.Run("flag overrides config", func(t *testing.T) {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"--config", tmpFile, "-m", "explicit-model", "-p", "hello"})
		cmd.Execute()
		modelVal := cmd.Flags().Lookup("model").Value.String()
		if modelVal != "explicit-model" {
			t.Errorf("expected model=explicit-model from flag, got %q", modelVal)
		}
	})

	// Test 3: provider-base-url from config
	t.Run("config sets provider base url", func(t *testing.T) {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"--config", tmpFile, "-p", "hello"})
		cmd.Execute()
		urlVal := cmd.Flags().Lookup("provider-base-url").Value.String()
		if urlVal != "http://config:9999" {
			t.Errorf("expected url=http://config:9999 from config, got %q", urlVal)
		}
	})
}
