package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type GitHost string
type ApiProvider string
type Config struct {
	OpenAIKey      string      `mapstructure:"openai_api_key"`
	ClaudeKey      string      `mapstructure:"claude_api_key"`
	OpenAIModel    string      `mapstructure:"openai_model"`
	ClaudeModel    string      `mapstructure:"claude_model"`
	OpenAIEndpoint string      `mapstructure:"openai_endpoint"`
	ClaudeEndpoint string      `mapstructure:"claude_endpoint"`
	Provider       ApiProvider `mapstructure:"provider"`
}

type PromptTemplate struct {
	Purpose      string
	Instructions string
}

const (
	OpenAI ApiProvider = "openai"
	Claude ApiProvider = "claude"
)

const (
	GitHub  GitHost = "github"
	GitLab  GitHost = "gitlab"
	Unknown GitHost = "unknown"
)

func loadConfig() (*Config, error) {
	cfg := &Config{
		Provider:       OpenAI,
		OpenAIModel:    "gpt-4o-mini",
		OpenAIEndpoint: "https://api.openai.com/v1/chat/completions",
		ClaudeModel:    "claude-3-7-sonnet-20250219",
		ClaudeEndpoint: "https://api.anthropic.com/v1/messages",
	}
	viper.SetConfigName(".ai-mr-comment.toml")
	viper.SetConfigType("toml")
	viper.AddConfigPath("$HOME")
	if err := viper.ReadInConfig(); err == nil {
		viper.AutomaticEnv()
		viper.SetEnvPrefix("AI_MR_COMMENT")
		if err := viper.UnmarshalExact(cfg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}
	return cfg, nil
}

func detectGitHost() GitHost {
	cmd := exec.Command("git", "remote", "-v")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return Unknown
	}
	output := string(out)
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "origin") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				url := parts[1]
				if strings.Contains(url, "github.com") {
					return GitHub
				} else if strings.Contains(url, "gitlab.com") {
					return GitLab
				}
			}
		}
	}
	return Unknown
}

func getGitDiff(commit string) (string, error) {
	var cmd *exec.Cmd
	if commit != "" {
		if strings.Contains(commit, "..") {
			cmd = exec.Command("git", "diff", commit)
		} else {
			cmd = exec.Command("git", "diff", fmt.Sprintf("%s^", commit), commit)
		}
	} else {
		cmd = exec.Command("git", "diff")
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func readDiffFromFile(path string) (string, error) {
	bytes, err := os.ReadFile(path)
	return string(bytes), err
}

func processDiff(raw string, maxLines int) string {
	var filteredLines []string
	var newFiles, deletedFiles []string
	var currentFile string
	var inNew, inDelete bool

	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Binary files") {
			continue
		}
		if strings.HasPrefix(line, "diff --git") {
			if currentFile != "" {
				if inNew {
					newFiles = append(newFiles, currentFile)
				} else if inDelete {
					deletedFiles = append(deletedFiles, currentFile)
				}
			}
			inNew, inDelete = false, false
			parts := strings.Split(line, " ")
			if len(parts) > 2 {
				currentFile = strings.TrimPrefix(parts[2], "a/")
			}
			continue
		}
		if strings.HasPrefix(line, "+++ /dev/null") {
			inDelete = true
		} else if strings.HasPrefix(line, "--- /dev/null") {
			inNew = true
		}
		if !inNew && !inDelete {
			filteredLines = append(filteredLines, line)
		}
	}
	if currentFile != "" {
		if inNew {
			newFiles = append(newFiles, currentFile)
		} else if inDelete {
			deletedFiles = append(deletedFiles, currentFile)
		}
	}

	summary := ""
	if len(newFiles) > 0 {
		summary += "\nNew files:\n"
		for _, f := range newFiles {
			summary += "• " + f + "\n"
		}
	}
	if len(deletedFiles) > 0 {
		summary += "\nDeleted files:\n"
		for _, f := range deletedFiles {
			summary += "• " + f + "\n"
		}
	}

	truncated := truncateDiff(filteredLines, maxLines)
	return truncated + summary
}

func truncateDiff(lines []string, max int) string {
	if len(lines) <= max {
		return strings.Join(lines, "\n")
	}
	head := strings.Join(lines[:max/2], "\n")
	tail := strings.Join(lines[len(lines)-(max/2):], "\n")
	return head + "\n[...diff truncated...]\n" + tail
}

func estimateTokens(text string) int {
	// Claude counts ~4 chars per token, OpenAI ~3.5 - we'll use conservative estimate
	return int(math.Ceil(float64(len(text)) / 3.5))
}

func NewPromptTemplate(host GitHost) PromptTemplate {
	var purpose, platform, artifact string
	switch host {
	case GitHub:
		purpose, platform, artifact = "GitHub PR comment", "GitHub", "PR"
	case GitLab:
		purpose, platform, artifact = "GitLab MR comment", "GitLab", "MR"
	default:
		purpose, platform, artifact = "MR/PR comment", "version control system", "MR/PR"
	}

	instructions := fmt.Sprintf(`Carefully review the provided git diff and generate a concise, professional %s comment. Use this format:

%s Title: [1-sentence summary]
%s Summary: [brief overview]
## Key Changes:

- [bulleted list of major updates]

## Why These Changes:

[explanation]

## Review Checklist:

- [ ] Item 1
- [ ] Item 2

## Notes:

[additional context]

Formatting rules:
- Use %s-appropriate terminology
- Maintain technical clarity while being concise
- Add blank lines after headings using '\n\n'
- Never include section headers in title/summary
- Adapt structure to %s conventions

Example %s Title: Add user authentication middleware
Example %s Summary: Implemented JWT-based authentication flow for API endpoints

The git diff may be truncated - focus analysis on visible changes.`, artifact, artifact, artifact, platform, platform, artifact, artifact)

	return PromptTemplate{Purpose: purpose, Instructions: instructions}
}

func (p PromptTemplate) SystemMessage() string {
	return p.Purpose + "\n\n" + p.Instructions
}

func callOpenAI(cfg *Config, prompt, diff string) (string, error) {
	client := &http.Client{}
	body := map[string]any{
		"model": cfg.OpenAIModel,
		"messages": []map[string]string{
			{"role": "system", "content": prompt},
			{"role": "user", "content": diff},
		},
		"temperature": 0.7,
	}
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", cfg.OpenAIEndpoint, bytes.NewBuffer(buf))
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cfg.OpenAIKey))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	res, _ := io.ReadAll(resp.Body)
	var parsed map[string]any
	if err := json.Unmarshal(res, &parsed); err != nil {
		return "", err
	}
	choices, ok := parsed["choices"].([]any)
	if !ok || len(choices) == 0 {
		return "", errors.New("no choices returned")
	}
	first := choices[0].(map[string]any)
	msg := first["message"].(map[string]any)
	return msg["content"].(string), nil
}

func callClaude(cfg *Config, prompt, diff string) (string, error) {
	client := &http.Client{}
	body := map[string]any{
		"model":  cfg.ClaudeModel,
		"system": prompt,
		"messages": []map[string]string{
			{"role": "user", "content": diff},
		},
		"temperature": 0.7,
		"max_tokens":  4000,
	}
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", cfg.ClaudeEndpoint, bytes.NewBuffer(buf))
	req.Header.Set("x-api-key", cfg.ClaudeKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	res, _ := io.ReadAll(resp.Body)
	var parsed map[string]any
	if err := json.Unmarshal(res, &parsed); err != nil {
		return "", err
	}
	content, ok := parsed["content"].([]any)
	if !ok || len(content) == 0 {
		return "", nil
	}
	entry := content[0].(map[string]any)
	return entry["text"].(string), nil
}

func callAPI(cfg *Config, provider ApiProvider, prompt, diff string) (string, error) {
	if provider == OpenAI {
		return callOpenAI(cfg, prompt, diff)
	} else if provider == Claude {
		return callClaude(cfg, prompt, diff)
	}
	return "", errors.New("unsupported provider")
}

func main() {
	var commit, filePath, outputPath, provider string
	var debug bool

	rootCmd := &cobra.Command{
		Use:   "ai-mr-comment",
		Short: "Generate MR/PR comments using AI",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := loadConfig()
			if cmd.Flags().Changed("provider") {
				cfg.Provider = ApiProvider(provider)
			} else {
				cfg.Provider = ApiProvider(cfg.Provider)
			}
			var diff string
			var err error
			if filePath != "" {
				diff, err = readDiffFromFile(filePath)
			} else {
				diff, err = getGitDiff(commit)
			}
			if err != nil {
				return err
			}
			diff = processDiff(diff, 4000)
			host := detectGitHost()
			prompt := NewPromptTemplate(host).SystemMessage()

			if debug {
				systemTokens := estimateTokens(prompt)
				diffTokens := estimateTokens(diff)
				originalLen := len(strings.Split(diff, "\n"))
				totalTokens := systemTokens + diffTokens
				fmt.Println("Token estimation:")
				fmt.Printf("- System prompt: %d tokens\n", systemTokens)
				fmt.Printf("- Diff content: %d tokens (%d lines)\n", diffTokens, originalLen)
				fmt.Printf("- Total estimate: %d tokens\n", totalTokens)
				fmt.Println("OpenApi limit: 200,000 tokens")
				fmt.Println("Claude's limit: 200,000 tokens")
				return nil
			}

			comment, err := callAPI(cfg, cfg.Provider, prompt, diff)
			if err != nil {
				return err
			}
			if outputPath != "" {
				return os.WriteFile(outputPath, []byte(comment), 0644)
			}
			fmt.Println(comment)
			return nil
		},
	}

	rootCmd.Flags().StringVar(&commit, "commit", "", "Commit or commit range")
	rootCmd.Flags().StringVar(&filePath, "file", "", "Path to diff file")
	rootCmd.Flags().StringVar(&outputPath, "output", "", "Output file path")
	rootCmd.Flags().StringVar(&provider, "provider", "openai", "API provider (openai or claude)")
	rootCmd.Flags().BoolVar(&debug, "debug", false, "Estimate token usage")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
