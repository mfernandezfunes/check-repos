package main

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-github/v62/github"
)

// MockGitHubClient implements GitHubClient for testing
// If repo is empty, owner contains the full repoFullName
// If path looks like a branch (such as "main"), then real path is empty
// Normal case where owner and repo are separated
// Repository skipped, check that there are no images
// Use t.Setenv to isolate environment
// Create initial file with some images
// Images to add (one new, one repeated)
// Read file and check content
// Clean up
type MockGitHubClient struct {
	repos    []*github.Repository
	contents map[string][]*github.RepositoryContent
	fileData map[string]string
	errors   map[string]error
}

func (m *MockGitHubClient) ListByOrg(ctx context.Context, org string, opt *github.RepositoryListByOrgOptions) ([]*github.Repository, *github.Response, error) {
	if err, exists := m.errors["ListByOrg"]; exists {
		return nil, nil, err
	}
	return m.repos, &github.Response{NextPage: 0}, nil
}

func (m *MockGitHubClient) GetContents(ctx context.Context, owner, repo, path string, opt *github.RepositoryContentGetOptions) (*github.RepositoryContent, []*github.RepositoryContent, *github.Response, error) {
	var key string
	if repo == "" {
		// Si repo está vacío, owner contiene el repoFullName completo
		// Si path parece ser un branch (como "main"), entonces path real está vacío
		if path == "main" || path == "master" || path == "develop" {
			key = fmt.Sprintf("%s/", owner)
		} else if path == "" {
			key = fmt.Sprintf("%s/", owner)
		} else {
			key = fmt.Sprintf("%s/%s", owner, path)
		}
	} else {
		// Caso normal donde owner y repo están separados
		if path == "" {
			key = fmt.Sprintf("%s/%s/", owner, repo)
		} else {
			key = fmt.Sprintf("%s/%s/%s", owner, repo, path)
		}
	}

	fmt.Printf("DEBUG: Mock GetContents called with owner=%s, repo=%s, path=%s, key=%s\n", owner, repo, path, key)
	fmt.Printf("DEBUG: Available contents keys: %v\n", m.contents)
	fmt.Printf("DEBUG: Available fileData keys: %v\n", m.fileData)

	if err, exists := m.errors[key]; exists {
		return nil, nil, nil, err
	}

	if contents, exists := m.contents[key]; exists {
		fmt.Printf("DEBUG: Found contents for key %s\n", key)
		return nil, contents, &github.Response{}, nil
	}

	if fileData, exists := m.fileData[key]; exists {
		fmt.Printf("DEBUG: Found fileData for key %s\n", key)
		content := &github.RepositoryContent{
			Type:    github.String("file"),
			Path:    github.String(path),
			Content: github.String(fileData),
		}
		return content, nil, &github.Response{}, nil
	}

	fmt.Printf("DEBUG: No data found for key %s\n", key)
	return nil, nil, &github.Response{}, fmt.Errorf("not found")
}

func TestGetRepositories(t *testing.T) {
	tests := []struct {
		name        string
		repos       []*github.Repository
		limit       int
		offset      int
		expectError bool
		expected    int
	}{
		{
			name: "Basic repository list",
			repos: []*github.Repository{
				{
					Name:          github.String("repo1"),
					FullName:      github.String("org/repo1"),
					HTMLURL:       github.String("https://github.com/org/repo1"),
					DefaultBranch: github.String("main"),
				},
				{
					Name:          github.String("repo2"),
					FullName:      github.String("org/repo2"),
					HTMLURL:       github.String("https://github.com/org/repo2"),
					DefaultBranch: github.String("main"),
				},
			},
			limit:       10,
			offset:      0,
			expectError: false,
			expected:    2,
		},
		{
			name: "With limit",
			repos: []*github.Repository{
				{
					Name:          github.String("repo1"),
					FullName:      github.String("org/repo1"),
					HTMLURL:       github.String("https://github.com/org/repo1"),
					DefaultBranch: github.String("main"),
				},
				{
					Name:          github.String("repo2"),
					FullName:      github.String("org/repo2"),
					HTMLURL:       github.String("https://github.com/org/repo2"),
					DefaultBranch: github.String("main"),
				},
			},
			limit:       1,
			offset:      0,
			expectError: false,
			expected:    1,
		},
		{
			name: "With offset",
			repos: []*github.Repository{
				{
					Name:          github.String("repo1"),
					FullName:      github.String("org/repo1"),
					HTMLURL:       github.String("https://github.com/org/repo1"),
					DefaultBranch: github.String("main"),
				},
				{
					Name:          github.String("repo2"),
					FullName:      github.String("org/repo2"),
					HTMLURL:       github.String("https://github.com/org/repo2"),
					DefaultBranch: github.String("main"),
				},
			},
			limit:       10,
			offset:      1,
			expectError: false,
			expected:    1,
		},
		{
			name:        "API error",
			repos:       []*github.Repository{},
			limit:       10,
			offset:      0,
			expectError: true,
			expected:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockGitHubClient{
				repos: tt.repos,
			}
			if tt.expectError {
				mock.errors = map[string]error{
					"ListByOrg": fmt.Errorf("API error"),
				}
			}

			repos, err := getRepositories(mock, "test-org", tt.limit, tt.offset)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			if len(repos) != tt.expected {
				t.Errorf("Expected %d repositories, got %d", tt.expected, len(repos))
			}
		})
	}
}

func TestAnalyzeRepository(t *testing.T) {
	tests := []struct {
		name     string
		repo     Repository
		contents map[string][]*github.RepositoryContent
		fileData map[string]string
		errors   map[string]error
		expected []DockerImage
	}{
		{
			name: "Repository with Dockerfile",
			repo: Repository{
				Name:          "test-repo",
				FullName:      "org/test-repo",
				DefaultBranch: "main",
			},
			contents: map[string][]*github.RepositoryContent{
				"org/test-repo/": {
					{
						Type: github.String("file"),
						Name: github.String("Dockerfile"),
						Path: github.String("Dockerfile"),
					},
				},
			},
			fileData: map[string]string{
				"org/test-repo/Dockerfile":    "FROM node:18-alpine AS test\nRUN npm install",
				"org/test-repo/kilonova.yaml": "some content",
			},
			errors: map[string]error{},
			expected: []DockerImage{
				{
					Repository: "test-repo",
					Branch:     "main",
					File:       "Dockerfile",
					Image:      "node:18-alpine",
					Line:       1,
					Context:    "test",
					Stage:      "test",
					Resolved:   true,
				},
			},
		},
		{
			name: "Repository with dockerfile.test",
			repo: Repository{
				Name:          "test-repo",
				FullName:      "org/test-repo",
				DefaultBranch: "main",
			},
			contents: map[string][]*github.RepositoryContent{
				"org/test-repo/": {
					{
						Type: github.String("file"),
						Name: github.String("dockerfile.test"),
						Path: github.String("dockerfile.test"),
					},
				},
			},
			fileData: map[string]string{
				"org/test-repo/dockerfile.test": "FROM python:3.9-slim\nRUN pip install pytest",
				"org/test-repo/kilonova.yaml":   "some content",
			},
			errors: map[string]error{},
			expected: []DockerImage{
				{
					Repository: "test-repo",
					Branch:     "main",
					File:       "dockerfile.test",
					Image:      "python:3.9-slim",
					Line:       1,
					Context:    "test",
					Stage:      "",
					Resolved:   true,
				},
			},
		},
		{
			name: "Repository without Docker files",
			repo: Repository{
				Name:          "test-repo",
				FullName:      "org/test-repo",
				DefaultBranch: "main",
			},
			contents: map[string][]*github.RepositoryContent{
				"org/test-repo/": {
					{
						Type: github.String("file"),
						Name: github.String("README.md"),
						Path: github.String("README.md"),
					},
				},
			},
			fileData: map[string]string{
				"org/test-repo/kilonova.yaml": "some content",
			},
			errors:   map[string]error{},
			expected: []DockerImage{},
		},
		{
			name: "Repository without kilonova.yaml (should be skipped)",
			repo: Repository{
				Name:          "test-repo",
				FullName:      "org/test-repo",
				DefaultBranch: "main",
			},
			contents: map[string][]*github.RepositoryContent{},
			fileData: map[string]string{},
			errors: map[string]error{
				"org/test-repo/kilonova.yaml": fmt.Errorf("404 not found"),
			},
			expected: []DockerImage{},
		},
		{
			name: "Repository with nested Dockerfile",
			repo: Repository{
				Name:          "test-repo",
				FullName:      "org/test-repo",
				DefaultBranch: "main",
			},
			contents: map[string][]*github.RepositoryContent{
				"org/test-repo/": {
					{
						Type: github.String("dir"),
						Name: github.String("docker"),
						Path: github.String("docker"),
					},
				},
				"org/test-repo/docker/": {
					{
						Type: github.String("file"),
						Name: github.String("Dockerfile"),
						Path: github.String("docker/Dockerfile"),
					},
				},
			},
			fileData: map[string]string{
				"org/test-repo/docker/Dockerfile": "FROM golang:1.21-alpine AS test\nRUN go mod download",
				"org/test-repo/kilonova.yaml":     "some content",
			},
			errors: map[string]error{},
			expected: []DockerImage{
				{
					Repository: "test-repo",
					Branch:     "main",
					File:       "docker/Dockerfile",
					Image:      "golang:1.21-alpine",
					Line:       1,
					Context:    "test",
					Stage:      "test",
					Resolved:   true,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockGitHubClient{
				contents: tt.contents,
				fileData: tt.fileData,
				errors:   tt.errors,
			}

			result, err := analyzeRepository(mock, tt.repo)
			if err != nil && err.Error() == "skipped - no kilonova.yaml" {
				// Repository skipped, check that there are no images
				if len(result) != 0 {
					t.Errorf("Expected 0 images for skipped repository, got %d", len(result))
				}
				return
			}
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			if len(result) == 0 && len(tt.expected) == 0 {
				return // both empty, OK
			}
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("analyzeRepository() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestAnalyzeRepositories(t *testing.T) {
	repos := []Repository{
		{
			Name:          "repo1",
			FullName:      "org/repo1",
			DefaultBranch: "main",
		},
		{
			Name:          "repo2",
			FullName:      "org/repo2",
			DefaultBranch: "main",
		},
	}

	mock := &MockGitHubClient{
		contents: map[string][]*github.RepositoryContent{
			"org/repo1/": {
				{
					Type: github.String("file"),
					Name: github.String("Dockerfile"),
					Path: github.String("Dockerfile"),
				},
			},
			"org/repo2/": {}, // repo2 without Dockerfile
		},
		fileData: map[string]string{
			"org/repo1/Dockerfile":    "FROM node:18-alpine AS test",
			"org/repo1/kilonova.yaml": "some content",
			"org/repo2/kilonova.yaml": "some content",
		},
		errors: map[string]error{},
	}

	result := analyzeRepositories(mock, "test-org", repos, make(map[string]bool))

	if result.Organization != "test-org" {
		t.Errorf("Expected organization 'test-org', got '%s'", result.Organization)
	}
	if result.TotalRepos != 2 {
		t.Errorf("Expected 2 total repos, got %d", result.TotalRepos)
	}
	if result.ReposWithDocker != 1 {
		t.Errorf("Expected 1 repo with docker, got %d", result.ReposWithDocker)
	}
	if len(result.Images) != 1 {
		t.Errorf("Expected 1 image, got %d", len(result.Images))
	}
}

func TestGetRepositoryContents(t *testing.T) {
	tests := []struct {
		name        string
		contents    map[string][]*github.RepositoryContent
		fileData    map[string]string
		expectError bool
		expected    int
	}{
		{
			name: "Single file",
			contents: map[string][]*github.RepositoryContent{
				"org/repo/": {
					{
						Type: github.String("file"),
						Name: github.String("Dockerfile"),
						Path: github.String("Dockerfile"),
					},
				},
			},
			fileData: map[string]string{
				"org/repo/Dockerfile": "FROM node:18-alpine\nRUN npm install",
			},
			expectError: false,
			expected:    1,
		},
		{
			name: "Multiple files",
			contents: map[string][]*github.RepositoryContent{
				"org/repo/": {
					{
						Type: github.String("file"),
						Name: github.String("Dockerfile"),
						Path: github.String("Dockerfile"),
					},
					{
						Type: github.String("file"),
						Name: github.String("dockerfile.test"),
						Path: github.String("dockerfile.test"),
					},
				},
			},
			fileData: map[string]string{
				"org/repo/Dockerfile":      "FROM node:18-alpine\nRUN npm install",
				"org/repo/dockerfile.test": "FROM python:3.9-slim\nRUN pip install pytest",
			},
			expectError: false,
			expected:    2,
		},
		{
			name:        "API error",
			contents:    map[string][]*github.RepositoryContent{},
			fileData:    map[string]string{},
			expectError: true,
			expected:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockGitHubClient{
				contents: tt.contents,
				fileData: tt.fileData,
			}
			if tt.expectError {
				mock.errors = map[string]error{
					"org/repo/": fmt.Errorf("API error"),
				}
			}

			contents, err := getRepositoryContents(mock, "org/repo", "main")
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			if len(contents) != tt.expected {
				t.Errorf("Expected %d contents, got %d", tt.expected, len(contents))
			}
		})
	}
}

func TestGetFileContent(t *testing.T) {
	tests := []struct {
		name        string
		fileData    map[string]string
		expectError bool
		expected    string
	}{
		{
			name: "Valid file content",
			fileData: map[string]string{
				"org/repo/Dockerfile": "FROM node:18-alpine\nRUN npm install",
			},
			expectError: false,
			expected:    "FROM node:18-alpine\nRUN npm install",
		},
		{
			name:        "File not found",
			fileData:    map[string]string{},
			expectError: true,
			expected:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockGitHubClient{
				fileData: tt.fileData,
			}

			content, err := getFileContent(mock, "org/repo", "Dockerfile", "main")
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			if content != tt.expected {
				t.Errorf("Expected content '%s', got '%s'", tt.expected, content)
			}
		})
	}
}

func TestCreateGitHubClient(t *testing.T) {
	client := createGitHubClient("test-token")
	if client == nil {
		t.Error("Expected non-nil client")
	}
}

func TestLoadConfig(t *testing.T) {
	// Test cases
	tests := []struct {
		name        string
		envVars     map[string]string
		expectError bool
		expected    Config
	}{
		{
			name: "Valid configuration",
			envVars: map[string]string{
				"GITHUB_TOKEN": "test_token",
				"GITHUB_ORG":   "test_org",
				"OUTPUT_FILE":  "test_output.txt",
				"LIMIT":        "50",
				"OFFSET":       "10",
				"OUTPUT_PATH":  "output",
			},
			expectError: false,
			expected: Config{
				GitHubToken:  "test_token",
				Organization: "test_org",
				OutputFile:   "output/test_output.txt",
				Limit:        50,
				Offset:       10,
				DelayMs:      1000,
				MaxRetries:   2,
				ClearCache:   false,
				OutputPath:   "output",
			},
		},
		{
			name: "Missing GitHub token",
			envVars: map[string]string{
				"GITHUB_ORG":  "test_org",
				"OUTPUT_PATH": "output",
			},
			expectError: true,
		},
		{
			name: "Missing organization",
			envVars: map[string]string{
				"GITHUB_TOKEN": "test_token",
				"OUTPUT_PATH":  "output",
			},
			expectError: true,
		},
		{
			name: "Default values",
			envVars: map[string]string{
				"GITHUB_TOKEN": "test_token",
				"GITHUB_ORG":   "test_org",
				"OUTPUT_PATH":  "output",
			},
			expectError: false,
			expected: Config{
				GitHubToken:  "test_token",
				Organization: "test_org",
				OutputFile:   "output/docker-images-0-100.txt",
				Limit:        100,
				Offset:       0,
				DelayMs:      1000,
				MaxRetries:   2,
				ClearCache:   false,
				OutputPath:   "output",
			},
		},
		{
			name: "Invalid limit",
			envVars: map[string]string{
				"GITHUB_TOKEN": "test_token",
				"GITHUB_ORG":   "test_org",
				"LIMIT":        "invalid",
				"OUTPUT_PATH":  "output",
			},
			expectError: false,
			expected: Config{
				GitHubToken:  "test_token",
				Organization: "test_org",
				OutputFile:   "output/docker-images-0-100.txt",
				Limit:        100, // Should default to 100
				Offset:       0,
				DelayMs:      1000,
				MaxRetries:   2,
				ClearCache:   false,
				OutputPath:   "output",
			},
		},
		{
			name: "Invalid offset",
			envVars: map[string]string{
				"GITHUB_TOKEN": "test_token",
				"GITHUB_ORG":   "test_org",
				"OFFSET":       "invalid",
				"OUTPUT_PATH":  "output",
			},
			expectError: false,
			expected: Config{
				GitHubToken:  "test_token",
				Organization: "test_org",
				OutputFile:   "output/docker-images-0-100.txt",
				Limit:        100,
				Offset:       0, // Should default to 0
				DelayMs:      1000,
				MaxRetries:   2,
				ClearCache:   false,
				OutputPath:   "output",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use t.Setenv to isolate environment
			for _, key := range []string{"GITHUB_TOKEN", "GITHUB_ORG", "OUTPUT_FILE", "LIMIT", "OFFSET"} {
				t.Setenv(key, "")
			}
			for key, value := range tt.envVars {
				t.Setenv(key, value)
			}

			config, err := loadConfig()
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
					return
				}
				if !reflect.DeepEqual(config, tt.expected) {
					t.Errorf("loadConfig() = %v, want %v", config, tt.expected)
				}
			}
		})
	}
}

func TestIsDockerfile(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected bool
	}{
		{"Dockerfile", "Dockerfile", true},
		{"dockerfile", "dockerfile", true},
		{"DOCKERFILE", "DOCKERFILE", true},
		{"dockerfile.test", "dockerfile.test", true},
		{"Dockerfile.test", "Dockerfile.test", true},
		{"Dockerfile.prod", "Dockerfile.prod", false},
		{"docker-compose.yml", "docker-compose.yml", false},
		{"README.md", "README.md", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isDockerfile(tt.filename)
			if result != tt.expected {
				t.Errorf("isDockerfile(%q) = %v, want %v", tt.filename, result, tt.expected)
			}
		})
	}
}

func TestExtractDockerImages(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		repoName string
		filePath string
		branch   string
		expected []DockerImage
	}{
		{
			name: "Dockerfile.test - extract all images",
			content: `FROM node:18-alpine
RUN npm install
FROM python:3.9-slim
RUN pip install requests`,
			repoName: "test-repo",
			filePath: "dockerfile.test",
			branch:   "main",
			expected: []DockerImage{
				{
					Repository: "test-repo",
					Branch:     "main",
					File:       "dockerfile.test",
					Image:      "node:18-alpine",
					Line:       1,
					Context:    "test",
					Stage:      "",
					Resolved:   true,
				},
				{
					Repository: "test-repo",
					Branch:     "main",
					File:       "dockerfile.test",
					Image:      "python:3.9-slim",
					Line:       3,
					Context:    "test",
					Stage:      "",
					Resolved:   true,
				},
			},
		},
		{
			name: "Dockerfile with test target",
			content: `FROM node:18-alpine AS base
RUN npm install

FROM base AS test
RUN npm install --dev

FROM python:3.9-slim AS test
RUN pip install pytest`,
			repoName: "test-repo",
			filePath: "Dockerfile",
			branch:   "main",
			expected: []DockerImage{
				{
					Repository: "test-repo",
					Branch:     "main",
					File:       "Dockerfile",
					Image:      "node:18-alpine",
					Line:       1,
					Context:    "test",
					Stage:      "test",
					Resolved:   true,
				},
				{
					Repository: "test-repo",
					Branch:     "main",
					File:       "Dockerfile",
					Image:      "python:3.9-slim",
					Line:       7,
					Context:    "test",
					Stage:      "test",
					Resolved:   true,
				},
			},
		},
		{
			name: "Dockerfile with nested test target",
			content: `FROM golang:1.24-bullseye AS base
RUN go mod download

FROM base AS builder
RUN go build -o app

FROM builder AS test
RUN go test ./...`,
			repoName: "test-repo",
			filePath: "Dockerfile",
			branch:   "main",
			expected: []DockerImage{
				{
					Repository: "test-repo",
					Branch:     "main",
					File:       "Dockerfile",
					Image:      "golang:1.24-bullseye",
					Line:       1,
					Context:    "test",
					Stage:      "test",
					Resolved:   true,
				},
			},
		},
		{
			name: "Dockerfile with no test target",
			content: `FROM node:18-alpine
RUN npm install
COPY . .
CMD ["npm", "start"]`,
			repoName: "test-repo",
			filePath: "Dockerfile",
			branch:   "main",
			expected: []DockerImage{},
		},
		{
			name: "Dockerfile with TEST target (uppercase)",
			content: `FROM ubuntu:20.04 AS TEST
RUN apt-get update`,
			repoName: "test-repo",
			filePath: "Dockerfile",
			branch:   "main",
			expected: []DockerImage{
				{
					Repository: "test-repo",
					Branch:     "main",
					File:       "Dockerfile",
					Image:      "ubuntu:20.04",
					Line:       1,
					Context:    "test",
					Stage:      "test",
					Resolved:   true,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractDockerImages(tt.content, tt.repoName, tt.filePath, tt.branch)
			if len(result) == 0 && len(tt.expected) == 0 {
				return // both empty, OK
			}
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("extractDockerImages() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSaveResults(t *testing.T) {
	tests := []struct {
		name     string
		result   Result
		filename string
		expected string
	}{
		{
			name: "Single image",
			result: Result{
				Organization: "test-org",
				Images: []DockerImage{
					{
						Repository: "repo1",
						Branch:     "main",
						File:       "Dockerfile",
						Image:      "node:18-alpine",
					},
				},
			},
			filename: "test_output.txt",
			expected: "test-org/repo1, main, ./Dockerfile, node:18-alpine\n",
		},
		{
			name: "Multiple images",
			result: Result{
				Organization: "test-org",
				Images: []DockerImage{
					{
						Repository: "repo1",
						Branch:     "main",
						File:       "Dockerfile",
						Image:      "node:18-alpine",
					},
					{
						Repository: "repo2",
						Branch:     "develop",
						File:       "docker/Dockerfile",
						Image:      "python:3.9-slim",
					},
				},
			},
			filename: "test_output.txt",
			expected: "test-org/repo1, main, ./Dockerfile, node:18-alpine\ntest-org/repo2, develop, docker/Dockerfile, python:3.9-slim\n",
		},
		{
			name: "No images",
			result: Result{
				Organization: "test-org",
				Images:       []DockerImage{},
			},
			filename: "test_output.txt",
			expected: "",
		},
		{
			name: "Image in subfolder",
			result: Result{
				Organization: "test-org",
				Images: []DockerImage{
					{
						Repository: "repo1",
						Branch:     "main",
						File:       "services/api/Dockerfile",
						Image:      "golang:1.21-alpine",
					},
				},
			},
			filename: "test_output.txt",
			expected: "test-org/repo1, main, services/api/Dockerfile, golang:1.21-alpine\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up any existing test file
			os.Remove(tt.filename)

			// Run the function
			err := saveResults(tt.result, tt.filename)
			if err != nil {
				t.Errorf("saveResults() error = %v", err)
				return
			}

			// Read the file and check content
			content, err := os.ReadFile(tt.filename)
			if err != nil {
				t.Errorf("Failed to read output file: %v", err)
				return
			}

			if string(content) != tt.expected {
				t.Errorf("saveResults() content = %q, want %q", string(content), tt.expected)
			}

			// Clean up
			os.Remove(tt.filename)
		})
	}
}

func TestParseDockerStages(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []DockerStage
	}{
		{
			name: "Simple stages",
			content: `FROM node:18-alpine AS base
RUN npm install
FROM base AS test
RUN npm install --dev`,
			expected: []DockerStage{
				{
					Name:      "base",
					BaseImage: "node:18-alpine",
					Line:      1,
					IsTarget:  false,
				},
				{
					Name:      "test",
					BaseImage: "base",
					Line:      3,
					IsTarget:  true,
				},
			},
		},
		{
			name: "Stages without AS",
			content: `FROM node:18-alpine
RUN npm install
FROM python:3.9-slim`,
			expected: []DockerStage{
				{
					Name:      "",
					BaseImage: "node:18-alpine",
					Line:      1,
					IsTarget:  false,
				},
				{
					Name:      "",
					BaseImage: "python:3.9-slim",
					Line:      3,
					IsTarget:  false,
				},
			},
		},
		{
			name: "Mixed stages",
			content: `FROM golang:1.24-bullseye AS base
RUN go mod download
FROM base AS builder
RUN go build -o app
FROM alpine AS test
RUN apk add --no-cache curl`,
			expected: []DockerStage{
				{
					Name:      "base",
					BaseImage: "golang:1.24-bullseye",
					Line:      1,
					IsTarget:  false,
				},
				{
					Name:      "builder",
					BaseImage: "base",
					Line:      3,
					IsTarget:  false,
				},
				{
					Name:      "test",
					BaseImage: "alpine",
					Line:      5,
					IsTarget:  true,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := strings.Split(tt.content, "\n")
			result := parseDockerStages(lines)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("parseDockerStages() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIsStageReference(t *testing.T) {
	tests := []struct {
		name     string
		image    string
		expected bool
	}{
		{"Stage reference", "base", true},
		{"Stage reference", "builder", true},
		{"Registry image", "node:18-alpine", false},
		{"Registry image with slash", "gcr.io/project/image:tag", false},
		{"Registry image with dot", "my-registry.com/image:latest", false},
		{"Local image with tag", "myimage:latest", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isStageReference(tt.image)
			if result != tt.expected {
				t.Errorf("isStageReference(%s) = %v, want %v", tt.image, result, tt.expected)
			}
		})
	}
}

func TestUpdateUniqueImages(t *testing.T) {
	filename := "unique-images-test.txt"
	defer os.Remove(filename)

	// Create initial file with some images
	os.WriteFile(filename, []byte("node:18-alpine\npython:3.9-slim\n"), 0644)

	// Images to add (one new, one repeated)
	images := []DockerImage{
		{Image: "node:18-alpine"},     // repeated
		{Image: "golang:1.21-alpine"}, // new
	}

	err := updateUniqueImages(images, filename)
	if err != nil {
		t.Fatalf("updateUniqueImages() error = %v", err)
	}

	// Read file and check content
	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	want := map[string]bool{
		"node:18-alpine":     true,
		"python:3.9-slim":    true,
		"golang:1.21-alpine": true,
	}
	if len(lines) != 3 {
		t.Errorf("Expected 3 unique images, got %d: %v", len(lines), lines)
	}
	for _, l := range lines {
		if !want[l] {
			t.Errorf("Unexpected image in file: %s", l)
		}
	}
}

func TestCheckKilonovaFile(t *testing.T) {
	tests := []struct {
		name         string
		repoFullName string
		branch       string
		fileData     map[string]string
		errors       map[string]error
		expected     bool
		expectError  bool
	}{
		{
			name:         "Repository with kilonova.yaml",
			repoFullName: "org/test-repo",
			branch:       "main",
			fileData: map[string]string{
				"org/test-repo/kilonova.yaml": "some content",
			},
			expected:    true,
			expectError: false,
		},
		{
			name:         "Repository without kilonova.yaml",
			repoFullName: "org/test-repo",
			branch:       "main",
			fileData:     map[string]string{},
			errors: map[string]error{
				"org/test-repo/kilonova.yaml": fmt.Errorf("404 not found"),
			},
			expected:    false,
			expectError: false,
		},
		{
			name:         "API error",
			repoFullName: "org/test-repo",
			branch:       "main",
			fileData:     map[string]string{},
			errors: map[string]error{
				"org/test-repo/kilonova.yaml": fmt.Errorf("500 server error"),
			},
			expected:    false,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockGitHubClient{
				fileData: tt.fileData,
				errors:   tt.errors,
			}

			result, err := checkKilonovaFile(mock, tt.repoFullName, tt.branch)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("checkKilonovaFile() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// Mock function for testing
var osExit = os.Exit

func TestMain(m *testing.M) {
	// Run tests
	code := m.Run()

	// Clean up any test files
	os.Remove("test_output.txt")

	os.Exit(code)
}
