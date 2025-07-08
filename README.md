# GitHub Docker Image Analyzer

This Go script analyzes all repositories in a GitHub organization and extracts the base Docker images used in `Dockerfile` and `dockerfile.test` files.

## Features

- Lists all repositories in a GitHub organization
- Recursively searches for `Dockerfile` and `dockerfile.test` files in each repository
- Extracts base images used in `FROM` instructions
- Generates a text file with the format: `org/repository_name, image`
- Supports pagination with limit and offset for incremental analysis
- **NEW: Intelligent caching system** - Skips already analyzed repositories to optimize processing time
- Handles GitHub API rate limits with automatic retries and configurable delay
- Logs informative progress and error messages
- Continues analysis even if some repositories fail
- Only analyzes the default branch of each repository

## Requirements

- Go 1.21 or higher
- GitHub token with `repo` and `read:org` permissions

## Installation

1. Clone or download the project files
2. Install dependencies:

```bash
go mod tidy
```

3. Copy the configuration file:

```bash
cp env.example .env
```

## Configuration

### 1. Create the .env file

Copy the example file and configure it:

```bash
cp env.example .env
```

### 2. Set your credentials

Edit the `.env` file with your credentials:

```bash
# GitHub Configuration
GITHUB_TOKEN=your_github_token_here
GITHUB_ORG=your_organization_name
OUTPUT_FILE=docker-images.txt  # optional

# Output directory for all .txt files (results, cache, etc.)
OUTPUT_PATH=./output  # All output files will be saved here

# Pagination Settings (Optional)
# Limit: Maximum number of repositories to analyze (default: 100)
# Offset: Number of repositories to skip (default: 0)
# Output file will be automatically named: docker-images-{OFFSET}-{OFFSET+LIMIT}.txt
LIMIT=100
OFFSET=0

# Rate Limiting (Optional)
DELAY_MS=200     # Delay between GitHub API calls (in milliseconds)
MAX_RETRIES=5    # Maximum number of retries for failed API calls

# Cache Management (Optional)
CLEAR_CACHE=false # Set to true to force re-analysis of all repositories

# Results Accumulation (Optional)
ACCUMULATE_RESULTS=false # Set to true to accumulate all results in docker-images-all.txt

# Cache Kilonova Checked (Optional)
ADD_CACHE_KILONOVA_CHECKED=false # Set to true to cache repositories without kilonova.yaml
```

#### How to get a GitHub Token

1. Go to GitHub Settings > Developer settings > Personal access tokens
2. Generate a new token with `repo` and `read:org` permissions
3. Copy the token and set it in the `GITHUB_TOKEN` variable

## Usage

```bash
go run main.go
```

All output files (`docker-images-*.txt`, `analyzed-repos.txt`, `unique-images.txt`) will be saved in the directory specified by `OUTPUT_PATH` (default: `./output`).

When `ACCUMULATE_RESULTS=true`, all results will be saved to `docker-images-all.txt` instead of the offset/limit specific file.

### Pagination

For organizations with many repositories, you can use pagination:

```bash
# Analyze only the first 50 repositories
LIMIT=50 go run main.go
# Output: docker-images-0-50.txt

# Skip the first 100 repositories and analyze the next 50
LIMIT=50 OFFSET=100 go run main.go
# Output: docker-images-100-150.txt

# Analyze repositories 200 to 250
LIMIT=50 OFFSET=200 go run main.go
# Output: docker-images-200-250.txt

# Force re-analysis of all repositories (clear cache)
CLEAR_CACHE=true go run main.go

# Accumulate all results in a single file
ACCUMULATE_RESULTS=true go run main.go

# Cache repositories without kilonova.yaml
ADD_CACHE_KILONOVA_CHECKED=true go run main.go

## Output

The script will generate a text file with the following format:

```
org/repo1, node:18-alpine
org/repo1, python:3.9-slim
org/repo2, ubuntu:20.04
org/repo3, golang:1.21-alpine
```

All output files will be located in the directory specified by `OUTPUT_PATH`.

## Project Structure

- `main.go`: Main script
- `main_test.go`: Unit tests
- `go.mod`: Project dependencies
- `README.md`: This file
- `env.example`: Example environment configuration
- `output/`: Default directory for all output files (can be changed with `OUTPUT_PATH`)
- `output/analyzed-repos.txt`: Cache file with already analyzed repositories (auto-generated)
- `output/docker-images-all.txt`: Unified results file (when `ACCUMULATE_RESULTS=true`)

## Functionality Details

### Repository Analysis
- Retrieves the complete list of repositories in the organization
- Recursively navigates all folders in each repository
- Searches for `Dockerfile` and `dockerfile.test` files

### Image Extraction
- Uses regular expressions to find `FROM` instructions
- Identifies the context (main/test) based on the file name
- Records the exact line where each image is found

### Error Handling
- Robust handling of GitHub API errors
- Continues analysis even if some repositories fail
- Informative logs about progress and errors

### Cache System
- Automatically tracks analyzed repositories in `analyzed-repos.txt` (inside `OUTPUT_PATH`)
- Skips already processed repositories to save time and API calls
- Provides statistics on skipped vs newly analyzed repositories
- Can be cleared using `CLEAR_CACHE=true` environment variable

### Rate Limit Handling
- Automatically detects GitHub API rate limits (HTTP 403)
- Waits until the rate limit resets, then continues
- Configurable delay between API calls (`DELAY_MS`, default: 500ms)
- Configurable maximum number of retries (`MAX_RETRIES`, default: 2)
- Uses exponential backoff for transient errors (5xx, network issues)
- Does not retry on client errors (404, 401) except rate limits
- Smart error handling to avoid unnecessary retries

## Limitations

- Requires read permissions on the organization
- Limited by GitHub API rate limits
- Only analyzes the default branch of each repository

## Running Unit Tests

To run the unit tests:

```bash
go test -v
```

All main functionalities are covered by unit tests in `main_test.go`. 