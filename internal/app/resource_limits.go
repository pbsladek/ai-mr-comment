package app

import (
	"fmt"

	internalconfig "github.com/pbsladek/ai-mr-comment/internal/config"
	"github.com/spf13/cobra"
)

func addResourceFlags(cmd *cobra.Command) {
	cmd.Flags().Int("chunk-concurrency", internalconfig.DefaultChunkConcurrency, "Maximum simultaneous smart-chunk requests")
	cmd.Flags().Int("max-output-tokens", internalconfig.DefaultMaxOutputTokens, "Maximum generated tokens per API request (includes reasoning where applicable)")
	cmd.Flags().Int64("max-input-bytes", internalconfig.DefaultMaxInputBytes, "Maximum diff input size in bytes")
}

func loadCommandConfig(cmd *cobra.Command, load func(string) (*Config, error), profile string) (*Config, error) {
	cfg, err := load(profile)
	if err != nil {
		return nil, err
	}
	copy := *cfg
	copy.MaxOutputTokens = cfg.OutputTokenLimit()
	copy.MaxInputBytes = cfg.InputByteLimit()
	copy.ChunkConcurrency = cfg.ChunkLimit()
	if cmd.Flags().Changed("chunk-concurrency") {
		copy.ChunkConcurrency, err = cmd.Flags().GetInt("chunk-concurrency")
		if err != nil {
			return nil, err
		}
	}
	if cmd.Flags().Changed("max-output-tokens") {
		copy.MaxOutputTokens, err = cmd.Flags().GetInt("max-output-tokens")
		if err != nil {
			return nil, err
		}
	}
	if cmd.Flags().Changed("max-input-bytes") {
		copy.MaxInputBytes, err = cmd.Flags().GetInt64("max-input-bytes")
		if err != nil {
			return nil, err
		}
	}
	if err := copy.ValidateLimits(); err != nil {
		return nil, withExitCode(4, err)
	}
	return &copy, nil
}

func checkInputSize(content string, cfg *Config) error {
	if int64(len(content)) > cfg.InputByteLimit() {
		return fmt.Errorf("diff exceeds --max-input-bytes limit (%d bytes)", cfg.InputByteLimit())
	}
	return nil
}
