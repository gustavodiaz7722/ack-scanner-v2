# ack-scanner-v2

ACK Scanner v2 — Agentic gap detection for AWS Controllers for Kubernetes (ACK).

ack-scanner-v2 uses AWS Bedrock (Claude) to semantically analyze ACK controller
fields and identify those needing `is_document` or `is_iam_policy` annotations.
It replaces v1's regex-based approach with an agentic architecture that achieves
higher accuracy through semantic understanding of Terraform documentation.

## Prerequisites

- **Go 1.26+**
- **git** (for sparse-cloning Terraform provider repository)
- **GitHub Token** (optional but recommended to avoid rate limits)
- **AWS Credentials** (required for Bedrock agent-powered operations)

### AWS Credentials

The scanner uses AWS Bedrock's Converse API. You need:

- AWS credentials configured via:
  - Environment variables: `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`
  - AWS profiles: `~/.aws/credentials`
  - IAM role (when running on EC2/ECS/Lambda)
- Required IAM permissions: `bedrock:InvokeModel` on the target model resource

### GitHub Token

Set `GITHUB_TOKEN` as an environment variable or pass `--github-token`:

```bash
export GITHUB_TOKEN=ghp_your_token_here
```

Without a token, the scanner uses unauthenticated GitHub API access (60 requests/hour limit).

## Building

```bash
go build -o ack-scanner-v2 .
```

## Usage

### Full Scan

Run a complete gap detection scan across all ACK controllers:

```bash
./ack-scanner-v2 scan --verbose --output markdown
```

### Individual Tools

Each tool can be run independently:

```bash
# Discover ACK controllers
./ack-scanner-v2 discover-controllers --output json

# Discover Terraform resources
./ack-scanner-v2 discover-terraform --output json

# Map controllers to Terraform docs (requires Bedrock)
./ack-scanner-v2 map-controllers --output json

# Analyze Terraform docs for JSON fields (requires Bedrock)
./ack-scanner-v2 analyze-fields --output json

# Match ACK fields against Terraform JSON fields (requires Bedrock)
./ack-scanner-v2 match-fields --output json

# Generate gap report from cached results
./ack-scanner-v2 report --output markdown
```

### Global Flags

| Flag | Environment Variable | Description |
|------|---------------------|-------------|
| `--github-token` | `GITHUB_TOKEN` | GitHub personal access token |
| `--cache-dir` | — | Cache directory (default: `$HOME/.ack-scanner-v2/cache`) |
| `--verbose` | — | Enable detailed progress logging to stderr |
| `--output` | — | Output format: `table`, `json`, `markdown` |
| `--model-id` | `ACK_SCANNER_MODEL_ID` | AWS Bedrock model ID (default: `anthropic.claude-sonnet-4-20250514-v1:0`) |
| `--region` | `AWS_REGION` | AWS region for Bedrock (default: `us-east-1`) |
| `--invalidate` | — | Invalidate cache for a tool (use `all` for everything) |
| `--max-parallel` | — | Max concurrent agent calls (default: 3) |

## Running Tests

### Unit Tests

```bash
go test ./...
```

### Integration Tests

Integration tests exercise real external services (GitHub API, git sparse-clone,
AWS Bedrock). They are gated behind a build tag and are **not** run by default.

#### Prerequisites

| Requirement | Purpose | Required For |
|---|---|---|
| `GITHUB_TOKEN` | GitHub API access without rate limits | `discovery_test.go`, `scan_test.go` |
| AWS credentials | Bedrock model invocation | `bedrock_test.go`, `scan_test.go` |
| `ACK_SCANNER_INTEGRATION=1` | Gate for Bedrock/full-scan tests | `bedrock_test.go`, `scan_test.go` |
| Network access | Clone repos, call APIs | All integration tests |

#### Running Integration Tests

```bash
# Run all integration tests (except Bedrock-gated ones)
go test -tags integration ./test/integration/...

# Run all integration tests including Bedrock
export GITHUB_TOKEN=ghp_...
export AWS_ACCESS_KEY_ID=...
export AWS_SECRET_ACCESS_KEY=...
export AWS_REGION=us-east-1
export ACK_SCANNER_INTEGRATION=1
go test -tags integration -timeout 15m ./test/integration/...

# Skip long-running Terraform sparse clone test
go test -tags integration -short ./test/integration/...

# Run a specific test
go test -tags integration -run TestDiscoverControllers_RealGitHub ./test/integration/...
```

#### Integration Test Descriptions

- **`discovery_test.go`** — Tests real GitHub API discovery of ACK controllers.
  Verifies that known controllers (s3, iam, ec2) are found with correct structure.

- **`terraform_test.go`** — Tests real sparse-clone of terraform-provider-aws.
  Verifies that known TF resources (s3_bucket, iam_role, etc.) are discovered.
  Uses `testing.Short()` skip for the slow clone operation.

- **`bedrock_test.go`** — Tests real AWS Bedrock agent calls.
  Requires `ACK_SCANNER_INTEGRATION=1`. Verifies the agent loop works end-to-end
  with a real model including tool-use interactions.

- **`scan_test.go`** — Full end-to-end scan focused on `sns-controller`.
  Requires `ACK_SCANNER_INTEGRATION=1`. Exercises all 6 phases of the scan pipeline.

## Architecture

The system is structured as discrete **tools** orchestrated by an **agent loop**:

```
CLI Commands → Agent (Bedrock Converse API) → Tools (local execution)
                                           ↓
                                    File-based Cache
```

See the design document for full architectural details.

## License

See [LICENSE](LICENSE) file.
