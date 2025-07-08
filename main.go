package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"math"

	"github.com/google/go-github/v62/github"
	"github.com/joho/godotenv"
	"golang.org/x/oauth2"
)

// Config holds the application configuration
type Config struct {
	GitHubToken       string
	Organization      string
	OutputFile        string
	Limit             int
	Offset            int
	DelayMs           int    // Delay between API calls in milliseconds
	MaxRetries        int    // Maximum number of retries for failed requests
	ClearCache        bool   // Whether to clear the cache before starting
	OutputPath        string // Directory to store all output .txt files
	AccumulateResults bool   // Whether to accumulate all results in a single file
}

// Repository represents a GitHub repository
type Repository struct {
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	URL           string `json:"html_url"`
	DefaultBranch string `json:"default_branch"`
}

// DockerImage represents a Docker image found in a repository
type DockerImage struct {
	Repository string `json:"repository"`
	Branch     string `json:"branch"`
	File       string `json:"file"`
	Image      string `json:"image"`
	Line       int    `json:"line"`
	Context    string `json:"context"`
	Stage      string `json:"stage"`
	Resolved   bool   `json:"resolved"`
}

// DockerStage represents a stage in a Dockerfile
type DockerStage struct {
	Name      string
	BaseImage string
	Line      int
	IsTarget  bool
}

// Result holds the analysis results
type Result struct {
	Organization    string        `json:"organization"`
	TotalRepos      int           `json:"total_repos"`
	ReposWithDocker int           `json:"repos_with_docker"`
	Images          []DockerImage `json:"images"`
	Timestamp       time.Time     `json:"timestamp"`
}

// GitHubClient is an interface to facilitate testing
// Only includes the methods actually used
// (ListByOrg, GetContents)
type GitHubClient interface {
	ListByOrg(ctx context.Context, org string, opt *github.RepositoryListByOrgOptions) ([]*github.Repository, *github.Response, error)
	GetContents(ctx context.Context, owner, repo, path string, opt *github.RepositoryContentGetOptions) (*github.RepositoryContent, []*github.RepositoryContent, *github.Response, error)
}

// githubClientWrapper implements GitHubClient using the real client
type githubClientWrapper struct {
	client *github.Client
}

func (g *githubClientWrapper) ListByOrg(ctx context.Context, org string, opt *github.RepositoryListByOrgOptions) ([]*github.Repository, *github.Response, error) {
	return g.client.Repositories.ListByOrg(ctx, org, opt)
}

func (g *githubClientWrapper) GetContents(ctx context.Context, owner, repo, path string, opt *github.RepositoryContentGetOptions) (*github.RepositoryContent, []*github.RepositoryContent, *github.Response, error) {
	return g.client.Repositories.GetContents(ctx, owner, repo, path, opt)
}

// rateLimitedGitHubClient implements GitHubClient with rate limiting and retry
type rateLimitedGitHubClient struct {
	client     GitHubClient
	delayMs    int
	maxRetries int
}

func newRateLimitedGitHubClient(client GitHubClient, delayMs, maxRetries int) *rateLimitedGitHubClient {
	return &rateLimitedGitHubClient{
		client:     client,
		delayMs:    delayMs,
		maxRetries: maxRetries,
	}
}

func (r *rateLimitedGitHubClient) ListByOrg(ctx context.Context, org string, opt *github.RepositoryListByOrgOptions) ([]*github.Repository, *github.Response, error) {
	var repositories []*github.Repository
	var resp *github.Response
	var err error

	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		// Add delay before API call (except for first attempt)
		if attempt > 0 {
			time.Sleep(time.Duration(r.delayMs) * time.Millisecond)
		}

		repositories, resp, err = r.client.ListByOrg(ctx, org, opt)

		if err == nil {
			return repositories, resp, nil
		}

		// Check if it's a rate limit error
		if isRateLimitError(err) {
			if resp != nil && resp.Rate.Reset.Time.After(time.Now()) {
				waitTime := time.Until(resp.Rate.Reset.Time) + time.Second
				fmt.Printf("Rate limit exceeded. Waiting %v until reset...\n", waitTime)
				time.Sleep(waitTime)
				continue
			}
		}

		// For other errors, use exponential backoff only if retryable
		if attempt < r.maxRetries && isRetryableError(err) {
			backoffTime := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			fmt.Printf("API call failed, retrying in %v... (attempt %d/%d)\n", backoffTime, attempt+1, r.maxRetries)
			time.Sleep(backoffTime)
			continue
		}
	}

	return nil, nil, fmt.Errorf("failed after %d retries: %w", r.maxRetries, err)
}

func (r *rateLimitedGitHubClient) GetContents(ctx context.Context, owner, repo, path string, opt *github.RepositoryContentGetOptions) (*github.RepositoryContent, []*github.RepositoryContent, *github.Response, error) {
	var fileContent *github.RepositoryContent
	var directoryContent []*github.RepositoryContent
	var resp *github.Response
	var err error

	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		// Add delay before API call (except for first attempt)
		if attempt > 0 {
			time.Sleep(time.Duration(r.delayMs) * time.Millisecond)
		}

		fileContent, directoryContent, resp, err = r.client.GetContents(ctx, owner, repo, path, opt)

		if err == nil {
			return fileContent, directoryContent, resp, nil
		}

		// Check if it's a rate limit error
		if isRateLimitError(err) {
			if resp != nil && resp.Rate.Reset.Time.After(time.Now()) {
				waitTime := time.Until(resp.Rate.Reset.Time) + time.Second
				fmt.Printf("Rate limit exceeded. Waiting %v until reset...\n", waitTime)
				time.Sleep(waitTime)
				continue
			}
		}

		// For other errors, use exponential backoff only if retryable
		if attempt < r.maxRetries && isRetryableError(err) {
			backoffTime := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			fmt.Printf("API call failed, retrying in %v... (attempt %d/%d)\n", backoffTime, attempt+1, r.maxRetries)
			time.Sleep(backoffTime)
			continue
		}
	}

	return nil, nil, nil, fmt.Errorf("failed after %d retries: %w", r.maxRetries, err)
}

// isRateLimitError checks if the error is a rate limit error
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	// Only retry on actual rate limit errors (403 with rate limit message)
	return strings.Contains(errStr, "403") && strings.Contains(errStr, "rate limit")
}

// isRetryableError checks if the error should be retried
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	// Don't retry on client errors (4xx) except rate limits
	if strings.Contains(errStr, "404") || strings.Contains(errStr, "401") || strings.Contains(errStr, "403") {
		return isRateLimitError(err)
	}

	// Retry on server errors (5xx) and network errors
	return strings.Contains(errStr, "500") || strings.Contains(errStr, "502") || strings.Contains(errStr, "503") ||
		strings.Contains(errStr, "504") || strings.Contains(errStr, "timeout") || strings.Contains(errStr, "connection")
}

// Cache functions for tracking analyzed repositories
var cacheFileName string

// loadAnalyzedRepos loads the list of already analyzed repositories from the cache file
func loadAnalyzedRepos() (map[string]bool, error) {
	analyzed := make(map[string]bool)

	file, err := os.Open(cacheFileName)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, return empty map
			return analyzed, nil
		}
		return nil, fmt.Errorf("error opening cache file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		repoName := strings.TrimSpace(scanner.Text())
		if repoName != "" {
			analyzed[repoName] = true
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading cache file: %w", err)
	}

	return analyzed, nil
}

// saveAnalyzedRepos saves a repository name to the cache file
func saveAnalyzedRepos(repoName string) error {
	file, err := os.OpenFile(cacheFileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("error opening cache file for writing: %w", err)
	}
	defer file.Close()

	_, err = file.WriteString(repoName + "\n")
	if err != nil {
		return fmt.Errorf("error writing to cache file: %w", err)
	}

	return nil
}

// isRepoAnalyzed checks if a repository has already been analyzed
func isRepoAnalyzed(repoName string, analyzedRepos map[string]bool) bool {
	return analyzedRepos[repoName]
}

// clearCache removes the cache file to start fresh
func clearCache() error {
	err := os.Remove(cacheFileName)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, which is fine
			return nil
		}
		return fmt.Errorf("error removing cache file: %w", err)
	}
	return nil
}

func main() {
	config, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Create GitHub client
	client := createGitHubClient(config.GitHubToken)
	// gh := &githubClientWrapper{client: client}
	gh := newRateLimitedGitHubClient(&githubClientWrapper{client: client}, config.DelayMs, config.MaxRetries)

	// Get repositories
	repos, err := getRepositories(gh, config.Organization, config.Limit, config.Offset)
	if err != nil {
		fmt.Printf("Error getting repositories: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Found %d repositories in organization %s\n", len(repos), config.Organization)

	// Handle cache clearing if requested
	if config.ClearCache {
		if err := clearCache(); err != nil {
			fmt.Printf("Warning: Could not clear cache: %v\n", err)
		} else {
			fmt.Println("Cache cleared successfully")
		}
	}

	// Load already analyzed repositories from cache
	analyzedRepos, err := loadAnalyzedRepos()
	if err != nil {
		fmt.Printf("Warning: Could not load analyzed repositories from cache: %v\n", err)
		// Initialize empty map if cache loading fails
		analyzedRepos = make(map[string]bool)
	}

	// Analyze repositories
	result := analyzeRepositories(gh, config.Organization, repos, analyzedRepos, config.OutputFile, config.AccumulateResults, config.OutputPath)

	// Results are saved immediately during analysis, no need to save again

	// Update unique images file
	if err := updateUniqueImages(result.Images, filepath.Join(config.OutputPath, "unique-images.txt")); err != nil {
		fmt.Printf("Error updating unique images: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Analysis complete. Found %d Docker images across %d repositories\n",
		len(result.Images), result.ReposWithDocker)
	if config.AccumulateResults {
		fmt.Printf("Results saved to: %s\n", filepath.Join(config.OutputPath, "docker-images-all.txt"))
	} else {
		fmt.Printf("Results saved to: %s\n", config.OutputFile)
	}
}

func loadConfig() (Config, error) {
	// Load .env file
	err := godotenv.Load()
	if err != nil {
		fmt.Println("Warning: .env file not found, using environment variables")
	}

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return Config{}, fmt.Errorf("GITHUB_TOKEN is required in .env file or environment variable. Please create a .env file with your GitHub token")
	}

	org := os.Getenv("GITHUB_ORG")
	if org == "" {
		return Config{}, fmt.Errorf("GITHUB_ORG is required in .env file or environment variable. Please create a .env file with your GitHub organization name")
	}

	// Parse limit and offset with defaults
	limitStr := os.Getenv("LIMIT")
	limit := 100 // default limit
	if limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil {
			limit = parsedLimit
		}
	}

	offsetStr := os.Getenv("OFFSET")
	offset := 0 // default offset
	if offsetStr != "" {
		if parsedOffset, err := strconv.Atoi(offsetStr); err == nil {
			offset = parsedOffset
		}
	}

	outputFile := os.Getenv("OUTPUT_FILE")
	if outputFile == "" {
		outputFile = fmt.Sprintf("docker-images-%d-%d.txt", offset, offset+limit)
	}

	outputPath := os.Getenv("OUTPUT_PATH")
	if outputPath == "" {
		outputPath = "."
	}
	// Ensure output directory exists
	if err := os.MkdirAll(outputPath, 0755); err != nil {
		return Config{}, fmt.Errorf("failed to create output directory: %w", err)
	}

	delayMsStr := os.Getenv("DELAY_MS")
	delayMs := 500 // default delay (increased from 100ms to 500ms)
	if delayMsStr != "" {
		if parsedDelayMs, err := strconv.Atoi(delayMsStr); err == nil {
			delayMs = parsedDelayMs
		}
	}

	maxRetriesStr := os.Getenv("MAX_RETRIES")
	maxRetries := 2 // default max retries (reduced from 3 to 2)
	if maxRetriesStr != "" {
		if parsedMaxRetries, err := strconv.Atoi(maxRetriesStr); err == nil {
			maxRetries = parsedMaxRetries
		}
	}

	// Parse clear cache option
	clearCacheStr := os.Getenv("CLEAR_CACHE")
	clearCache := false
	if clearCacheStr != "" {
		if parsedClearCache, err := strconv.ParseBool(clearCacheStr); err == nil {
			clearCache = parsedClearCache
		}
	}

	// Parse accumulate results option
	accumulateResultsStr := os.Getenv("ACCUMULATE_RESULTS")
	accumulateResults := false
	if accumulateResultsStr != "" {
		if parsedAccumulateResults, err := strconv.ParseBool(accumulateResultsStr); err == nil {
			accumulateResults = parsedAccumulateResults
		}
	}

	cacheFileName = filepath.Join(outputPath, "analyzed-repos.txt")

	return Config{
		GitHubToken:       token,
		Organization:      org,
		OutputFile:        filepath.Join(outputPath, outputFile),
		Limit:             limit,
		Offset:            offset,
		DelayMs:           delayMs,
		MaxRetries:        maxRetries,
		ClearCache:        clearCache,
		OutputPath:        outputPath,
		AccumulateResults: accumulateResults,
	}, nil
}

func createGitHubClient(token string) *github.Client {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	return github.NewClient(tc)
}

func getRepositories(client GitHubClient, org string, limit, offset int) ([]Repository, error) {
	var repos []Repository
	ctx := context.Background()

	opt := &github.RepositoryListByOrgOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	totalProcessed := 0
	skipped := 0

	for {
		repositories, resp, err := client.ListByOrg(ctx, org, opt)
		if err != nil {
			return nil, fmt.Errorf("error listing repositories: %w", err)
		}

		for _, repo := range repositories {
			// Skip until we reach the offset
			if skipped < offset {
				skipped++
				continue
			}

			// Stop if we've reached the limit
			if limit > 0 && totalProcessed >= limit {
				return repos, nil
			}

			repos = append(repos, Repository{
				Name:          repo.GetName(),
				FullName:      repo.GetFullName(),
				URL:           repo.GetHTMLURL(),
				DefaultBranch: repo.GetDefaultBranch(),
			})
			totalProcessed++
		}

		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	return repos, nil
}

func analyzeRepositories(client GitHubClient, org string, repos []Repository, analyzedRepos map[string]bool, outputFile string, accumulateResults bool, outputPath string) Result {
	result := Result{
		Organization: org,
		TotalRepos:   len(repos),
		Timestamp:    time.Now(),
	}

	skippedCount := 0
	analyzedCount := 0

	for _, repo := range repos {
		// Check if repository has already been analyzed
		if isRepoAnalyzed(repo.FullName, analyzedRepos) {
			fmt.Printf("Skipping repository %s (already analyzed)\n", repo.Name)
			skippedCount++
			continue
		}

		images, err := analyzeRepository(client, repo)
		if err != nil {
			if err.Error() == "skipped - no kilonova.yaml" {
				fmt.Printf("Skipping repository %s due to missing kilonova.yaml\n", repo.Name)
				continue
			}
			fmt.Printf("Error analyzing repository %s: %v\n", repo.Name, err)
			continue
		}
		fmt.Printf("Analyzing repository: %s - Found %d images\n", repo.Name, len(images))

		// Save results immediately for this repository
		if len(images) > 0 {
			if accumulateResults {
				// Save only to the unified file when accumulate is enabled
				unifiedFile := filepath.Join(outputPath, "docker-images-all.txt")
				if err := appendResultToFile(org, repo.Name, images, unifiedFile); err != nil {
					fmt.Printf("Warning: Could not save results to unified file for repository %s: %v\n", repo.Name, err)
				}
			} else {
				// Save to specific output file (with offset/limit) when accumulate is disabled
				if err := appendResultToFile(org, repo.Name, images, outputFile); err != nil {
					fmt.Printf("Warning: Could not save results for repository %s: %v\n", repo.Name, err)
				}
			}

			result.ReposWithDocker++
			result.Images = append(result.Images, images...)
		}

		// Save analyzed repository to cache
		if err := saveAnalyzedRepos(repo.FullName); err != nil {
			fmt.Printf("Warning: Could not save analyzed repository to cache: %v\n", err)
		}
		analyzedCount++
	}

	fmt.Printf("Cache statistics: %d repositories skipped (already analyzed), %d repositories newly analyzed\n", skippedCount, analyzedCount)

	return result
}

func analyzeRepository(client GitHubClient, repo Repository) ([]DockerImage, error) {
	var images []DockerImage

	// Check if kilonova.yaml exists
	hasKilonova, err := checkKilonovaFile(client, repo.FullName, repo.DefaultBranch)
	if err != nil {
		fmt.Printf("Error checking kilonova.yaml for %s: %v\n", repo.Name, err)
		return images, err
	}

	if !hasKilonova {
		return images, fmt.Errorf("skipped - no kilonova.yaml")
	}

	// Get repository contents recursively
	contents, err := getRepositoryContents(client, repo.FullName, repo.DefaultBranch)
	if err != nil {
		fmt.Printf("Error getting contents for %s: %v\n", repo.Name, err)
		return images, err
	}

	// Look for Dockerfile and dockerfile.test files
	for _, content := range contents {
		if isDockerfile(content.GetName()) {
			fileContent, err := getFileContent(client, repo.FullName, content.GetPath(), repo.DefaultBranch)
			if err != nil {
				fmt.Printf("Error getting content for %s: %v\n", content.GetPath(), err)
				continue
			}

			dockerImages := extractDockerImages(fileContent, repo.Name, content.GetPath(), repo.DefaultBranch)
			images = append(images, dockerImages...)
		}
	}

	return images, nil
}

func checkKilonovaFile(client GitHubClient, repoFullName, branch string) (bool, error) {
	ctx := context.Background()
	owner, repo := splitOwnerRepo(repoFullName)

	// Try to get the kilonova.yaml file
	fileContent, _, _, err := client.GetContents(ctx, owner, repo, "kilonova.yaml", &github.RepositoryContentGetOptions{Ref: branch})

	if err != nil {
		// If file not found, return false (no error)
		if strings.Contains(err.Error(), "404") {
			return false, nil
		}
		return false, err
	}

	// If we get here, the file exists
	return fileContent != nil, nil
}

// Helper to split owner and repo from repoFullName
func splitOwnerRepo(repoFullName string) (string, string) {
	parts := strings.SplitN(repoFullName, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return repoFullName, ""
}

func getRepositoryContents(client GitHubClient, repoFullName, branch string) ([]*github.RepositoryContent, error) {
	var allContents []*github.RepositoryContent

	err := getContentsRecursive(client, repoFullName, "", branch, &allContents)
	if err != nil {
		return nil, err
	}

	return allContents, nil
}

func getContentsRecursive(client GitHubClient, repoFullName, path, branch string, contents *[]*github.RepositoryContent) error {
	ctx := context.Background()
	owner, repo := splitOwnerRepo(repoFullName)
	fileContent, directoryContent, _, err := client.GetContents(ctx, owner, repo, path, &github.RepositoryContentGetOptions{Ref: branch})

	if err != nil {
		return err
	}

	if fileContent != nil {
		*contents = append(*contents, fileContent)
		return nil
	}

	if directoryContent != nil {
		for _, content := range directoryContent {
			if content.GetType() == "file" {
				*contents = append(*contents, content)
			} else if content.GetType() == "dir" {
				err := getContentsRecursive(client, repoFullName, content.GetPath(), branch, contents)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func getFileContent(client GitHubClient, repoFullName, path, branch string) (string, error) {
	ctx := context.Background()
	owner, repo := splitOwnerRepo(repoFullName)
	fileContent, _, _, err := client.GetContents(ctx, owner, repo, path, &github.RepositoryContentGetOptions{Ref: branch})

	if err != nil {
		return "", err
	}

	content, err := fileContent.GetContent()
	if err != nil {
		return "", err
	}

	return content, nil
}

func isDockerfile(filename string) bool {
	filename = strings.ToLower(filename)
	return filename == "dockerfile" || filename == "dockerfile.test"
}

func extractDockerImages(content, repoName, filePath, branch string) []DockerImage {
	// Check if this is a dockerfile.test file
	isTestFile := strings.Contains(strings.ToLower(filePath), "test")

	if isTestFile {
		// For dockerfile.test files, extract all images directly
		return extractAllImages(content, repoName, filePath, branch, "test")
	} else {
		// For regular Dockerfiles, resolve target dependencies
		return getTargetBaseImage(content, repoName, filePath, branch)
	}
}

func extractAllImages(content, repoName, filePath, branch, context string) []DockerImage {
	var images []DockerImage
	lines := strings.Split(content, "\n")
	fromRegex := regexp.MustCompile(`(?i)^\s*FROM\s+([^\s#]+)`)

	for i, line := range lines {
		matches := fromRegex.FindStringSubmatch(line)
		if len(matches) > 1 {
			image := matches[1]
			images = append(images, DockerImage{
				Repository: repoName,
				Branch:     branch,
				File:       filePath,
				Image:      image,
				Line:       i + 1,
				Context:    context,
				Stage:      "",
				Resolved:   true,
			})
		}
	}

	return images
}

func getTargetBaseImage(content, repoName, filePath, branch string) []DockerImage {
	lines := strings.Split(content, "\n")

	// Parse all stages
	stages := parseDockerStages(lines)

	// Find test targets
	testTargets := findTestTargets(stages)

	// Resolve base images for test targets
	var images []DockerImage
	for _, target := range testTargets {
		resolvedImages := resolveTargetDependencies(target, stages, repoName, filePath, branch)
		images = append(images, resolvedImages...)
	}

	return images
}

func parseDockerStages(lines []string) []DockerStage {
	var stages []DockerStage
	fromRegex := regexp.MustCompile(`(?i)^\s*FROM\s+([^\s#]+)(?:\s+AS\s+([^\s#]+))?`)

	for i, line := range lines {
		line = strings.TrimSpace(line)
		matches := fromRegex.FindStringSubmatch(line)

		if len(matches) > 1 {
			baseImage := matches[1]
			stageName := ""

			if len(matches) > 2 && matches[2] != "" {
				stageName = strings.ToLower(matches[2])
			}

			stages = append(stages, DockerStage{
				Name:      stageName,
				BaseImage: baseImage,
				Line:      i + 1,
				IsTarget:  stageName == "test",
			})
		}
	}

	return stages
}

func findTestTargets(stages []DockerStage) []DockerStage {
	var testTargets []DockerStage

	for _, stage := range stages {
		if stage.IsTarget {
			testTargets = append(testTargets, stage)
		}
	}

	return testTargets
}

func resolveTargetDependencies(target DockerStage, stages []DockerStage, repoName, filePath, branch string) []DockerImage {
	var images []DockerImage
	visited := make(map[string]bool)

	// Start resolution from the target stage
	resolveStage(target, stages, &images, visited, repoName, filePath, branch)

	return images
}

func resolveStage(stage DockerStage, stages []DockerStage, images *[]DockerImage, visited map[string]bool, repoName, filePath, branch string) {
	// If we've already visited this stage, skip to avoid cycles
	if visited[stage.Name] {
		return
	}

	visited[stage.Name] = true

	// Check if base image is a reference to another stage
	if isStageReference(stage.BaseImage) {
		// Find the referenced stage
		referencedStage := findStageByName(stage.BaseImage, stages)
		if referencedStage.Name != "" {
			// Recursively resolve the referenced stage
			resolveStage(referencedStage, stages, images, visited, repoName, filePath, branch)
		}
	} else {
		// This is a base image (not a stage reference), add it to results
		*images = append(*images, DockerImage{
			Repository: repoName,
			Branch:     branch,
			File:       filePath,
			Image:      stage.BaseImage,
			Line:       stage.Line,
			Context:    "test",
			Stage:      "test", // Always "test" for test targets
			Resolved:   true,
		})
	}
}

func isStageReference(image string) bool {
	// Check if the image is a reference to a stage (not a registry image)
	// Stage references are typically just names without slashes or colons
	return !strings.Contains(image, "/") && !strings.Contains(image, ":") && !strings.Contains(image, ".")
}

func findStageByName(name string, stages []DockerStage) DockerStage {
	for _, stage := range stages {
		if stage.Name == strings.ToLower(name) {
			return stage
		}
	}
	return DockerStage{}
}

func saveResults(result Result, outputFile string) error {
	// Create text file with format: org/repo_name, branch, folder/file, image
	// Use append mode to avoid overwriting existing results
	file, err := os.OpenFile(outputFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("error creating/opening file: %w", err)
	}
	defer file.Close()

	for _, image := range result.Images {
		// Extract folder and filename from the file path
		filePath := image.File
		folder := "."
		filename := filePath

		// If the file is not in the root, extract the folder
		if strings.Contains(filePath, "/") {
			lastSlash := strings.LastIndex(filePath, "/")
			folder = filePath[:lastSlash]
			filename = filePath[lastSlash+1:]
		}

		line := fmt.Sprintf("%s/%s, %s, %s/%s, %s\n", result.Organization, image.Repository, image.Branch, folder, filename, image.Image)
		_, err := file.WriteString(line)
		if err != nil {
			return fmt.Errorf("error writing to file: %w", err)
		}
	}

	return nil
}

// appendResultToFile appends a single repository result to the output file
func appendResultToFile(org, repoName string, images []DockerImage, outputFile string) error {
	if len(images) == 0 {
		return nil // No images to save
	}

	// Open file in append mode
	file, err := os.OpenFile(outputFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("error opening file for append: %w", err)
	}
	defer file.Close()

	for _, image := range images {
		// Extract folder and filename from the file path
		filePath := image.File
		folder := "."
		filename := filePath

		// If the file is not in the root, extract the folder
		if strings.Contains(filePath, "/") {
			lastSlash := strings.LastIndex(filePath, "/")
			folder = filePath[:lastSlash]
			filename = filePath[lastSlash+1:]
		}

		line := fmt.Sprintf("%s/%s, %s, %s/%s, %s\n", org, image.Repository, image.Branch, folder, filename, image.Image)
		_, err := file.WriteString(line)
		if err != nil {
			return fmt.Errorf("error writing to file: %w", err)
		}
	}

	return nil
}

func updateUniqueImages(images []DockerImage, filename string) error {
	// Leer imágenes existentes
	existing := make(map[string]struct{})
	if f, err := os.Open(filename); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			existing[scanner.Text()] = struct{}{}
		}
		f.Close()
	}

	// Abrir archivo en modo append
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	added := 0
	for _, img := range images {
		if _, ok := existing[img.Image]; !ok {
			if _, err := f.WriteString(img.Image + "\n"); err != nil {
				return err
			}
			existing[img.Image] = struct{}{}
			added++
		}
	}
	if added > 0 {
		fmt.Printf("Added %d unique images to %s\n", added, filename)
	}
	return nil
}
