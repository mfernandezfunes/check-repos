# Example of Cache System Usage

## What does the cache system do?

The cache system optimizes processing time by avoiding re-analyzing repositories that have already been processed. This is especially useful when:

- You have many repositories in your organization
- You want to run the analysis in multiple sessions
- You need to re-run the script after interruptions

## How it works

### First run
```bash
go run main.go
```

**Expected output:**
```
Found 150 repositories in organization my-org
Analyzing repository: repo1 - Found 3 images
Analyzing repository: repo2 - Found 1 images
Analyzing repository: repo3 - Found 0 images
...
Cache statistics: 0 repositories skipped (already analyzed), 150 repositories newly analyzed
```

**Files generated (in OUTPUT_PATH):**
- `docker-images-0-100.txt` - Analysis results
- `analyzed-repos.txt` - List of already analyzed repositories

### Second run (same range)
```bash
go run main.go
```

**Expected output:**
```
Found 150 repositories in organization my-org
Skipping repository repo1 (already analyzed)
Skipping repository repo2 (already analyzed)
Skipping repository repo3 (already analyzed)
...
Cache statistics: 150 repositories skipped (already analyzed), 0 repositories newly analyzed
```

### Run with new range
```bash
LIMIT=50 OFFSET=150 go run main.go
```

**Expected output:**
```
Found 50 repositories in organization my-org
Analyzing repository: repo151 - Found 2 images
Analyzing repository: repo152 - Found 0 images
...
Cache statistics: 0 repositories skipped (already analyzed), 50 repositories newly analyzed
```

### Force full re-analysis
```bash
CLEAR_CACHE=true go run main.go
```

**Expected output:**
```
Found 150 repositories in organization my-org
Cache cleared successfully
Analyzing repository: repo1 - Found 3 images
Analyzing repository: repo2 - Found 1 images
...
Cache statistics: 0 repositories skipped (already analyzed), 150 repositories newly analyzed
```

## Cache file structure

The file `analyzed-repos.txt` (inside `OUTPUT_PATH`) contains one repository per line:

```
my-org/repo1
my-org/repo2
my-org/repo3
my-org/repo4
```

## Benefits

1. **Time saving**: Already processed repositories are not re-analyzed
2. **API call saving**: Reduces GitHub API usage
3. **Resilience to interruptions**: You can re-run the script without losing progress
4. **Incremental analysis**: You can process repositories in batches

## Configuration

### Environment variables related to cache

```bash
# Directory for all output files (default: ./output)
OUTPUT_PATH=./output

# Clear cache before running (default: false)
CLEAR_CACHE=false

# Example: Force re-analysis
CLEAR_CACHE=true go run main.go
```

## Important notes

- The cache is kept between runs (in the directory specified by `OUTPUT_PATH`)
- If a repository changes, you will need to use `CLEAR_CACHE=true` to re-analyze it
- The cache file is created automatically on the first run
- If the cache file is corrupted, the script will continue to work (it will just show a warning) 