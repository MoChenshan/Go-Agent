#!/bin/bash

# ==============================================================================
# Vector Store with tRPC Examples Runner
# ==============================================================================
#
# This script runs the vector store examples with tRPC integration.
# Before running, you need to set the required environment variables.
#
# ==============================================================================
# REQUIRED ENVIRONMENT VARIABLES
# ==============================================================================
#
# --- LLM Configuration (Required for all examples) ---
# export OPENAI_API_KEY=sk-xxxx                    # Your OpenAI API key
# export OPENAI_BASE_URL=https://api.openai.com/v1 # OpenAI API endpoint (optional)
# export MODEL_NAME=deepseek-chat                  # Model name (optional, default: deepseek-chat)
#
# --- Elasticsearch Configuration ---
# export ELASTICSEARCH_HOSTS=http://localhost:9200  # ES cluster address
# export ELASTICSEARCH_USERNAME=elastic             # ES username
# export ELASTICSEARCH_PASSWORD=your-password       # ES password
#
# --- PostgreSQL Configuration ---
# export PGVECTOR_HOST=127.0.0.1                   # PostgreSQL host
# export PGVECTOR_PORT=5432                        # PostgreSQL port
# export PGVECTOR_USER=postgres                    # PostgreSQL username
# export PGVECTOR_PASSWORD=yourpassword            # PostgreSQL password
# export PGVECTOR_DATABASE=vectordb                # PostgreSQL database name
#
# --- TCVectorDB Configuration ---
# export TCVECTOR_URL=http://your-tcvector-host:80 # TCVectorDB URL
# export TCVECTOR_USERNAME=root                    # TCVectorDB username
# export TCVECTOR_PASSWORD=your-api-key            # TCVectorDB password/key
#
# ==============================================================================
# EXAMPLE CONFIGURATION
# ==============================================================================
#
# Copy and modify the following to your shell or .env file:
#
# # LLM
# export OPENAI_API_KEY=sk-xxxxxxxxxxxxxxxxxxxx
# export OPENAI_BASE_URL=https://api.openai.com/v1
# export MODEL_NAME=gpt-4
#
# # Elasticsearch
# export ELASTICSEARCH_HOSTS=http://9.135.232.12:9200
# export ELASTICSEARCH_USERNAME=elastic
# export ELASTICSEARCH_PASSWORD=your-es-password
#
# # PostgreSQL
# export PGVECTOR_HOST=127.0.0.1
# export PGVECTOR_PORT=5432
# export PGVECTOR_USER=root
# export PGVECTOR_PASSWORD=123
# export PGVECTOR_DATABASE=vector
#
# # TCVectorDB
# export TCVECTOR_URL=http://21.91.108.237:80
# export TCVECTOR_USERNAME=root
# export TCVECTOR_PASSWORD=your-tcvector-key
#
# ==============================================================================

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_header() {
    echo -e "${BLUE}============================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}============================================${NC}"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

check_env_var() {
    local var_name=$1
    local var_value="${!var_name}"
    if [ -z "$var_value" ]; then
        print_warning "$var_name is not set"
        return 1
    fi
    return 0
}

check_llm_config() {
    echo "Checking LLM configuration..."
    local missing=0
    check_env_var "OPENAI_API_KEY" || missing=1
    if [ $missing -eq 1 ]; then
        print_error "LLM configuration is incomplete. Please set OPENAI_API_KEY."
        return 1
    fi
    print_success "LLM configuration is complete"
    return 0
}

check_elasticsearch_config() {
    echo "Checking Elasticsearch configuration..."
    local missing=0
    check_env_var "ELASTICSEARCH_HOSTS" || missing=1
    if [ $missing -eq 1 ]; then
        print_warning "Elasticsearch configuration is incomplete"
        return 1
    fi
    print_success "Elasticsearch configuration is complete"
    return 0
}

check_postgres_config() {
    echo "Checking PostgreSQL configuration..."
    local missing=0
    check_env_var "PGVECTOR_HOST" || missing=1
    check_env_var "PGVECTOR_PORT" || missing=1
    check_env_var "PGVECTOR_USER" || missing=1
    check_env_var "PGVECTOR_PASSWORD" || missing=1
    check_env_var "PGVECTOR_DATABASE" || missing=1
    if [ $missing -eq 1 ]; then
        print_warning "PostgreSQL configuration is incomplete"
        return 1
    fi
    print_success "PostgreSQL configuration is complete"
    return 0
}

check_tcvector_config() {
    echo "Checking TCVectorDB configuration..."
    local missing=0
    check_env_var "TCVECTOR_URL" || missing=1
    check_env_var "TCVECTOR_USERNAME" || missing=1
    check_env_var "TCVECTOR_PASSWORD" || missing=1
    if [ $missing -eq 1 ]; then
        print_warning "TCVectorDB configuration is incomplete"
        return 1
    fi
    print_success "TCVectorDB configuration is complete"
    return 0
}

run_elasticsearch() {
    print_header "Running Elasticsearch Example"
    if ! check_llm_config; then
        return 1
    fi
    if ! check_elasticsearch_config; then
        return 1
    fi
    echo ""
    cd "$SCRIPT_DIR/elasticsearch"
    go run main.go
    cd "$SCRIPT_DIR"
}

run_postgres() {
    print_header "Running PostgreSQL Example"
    if ! check_llm_config; then
        return 1
    fi
    if ! check_postgres_config; then
        return 1
    fi
    echo ""
    cd "$SCRIPT_DIR/postgres"
    go run main.go
    cd "$SCRIPT_DIR"
}

run_tcvector() {
    print_header "Running TCVectorDB Example"
    if ! check_llm_config; then
        return 1
    fi
    if ! check_tcvector_config; then
        return 1
    fi
    echo ""
    cd "$SCRIPT_DIR/tcvector"
    go run main.go
    cd "$SCRIPT_DIR"
}

show_usage() {
    echo "Usage: $0 [OPTION]"
    echo ""
    echo "Options:"
    echo "  elasticsearch, es    Run Elasticsearch example"
    echo "  postgres, pg         Run PostgreSQL example"
    echo "  tcvector, tc         Run TCVectorDB example"
    echo "  all                  Run all examples"
    echo "  check                Check environment variables"
    echo "  help, -h, --help     Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0 es                # Run Elasticsearch example"
    echo "  $0 pg                # Run PostgreSQL example"
    echo "  $0 tc                # Run TCVectorDB example"
    echo "  $0 all               # Run all examples"
    echo "  $0 check             # Check all environment variables"
}

check_all_config() {
    print_header "Checking All Configurations"
    echo ""
    check_llm_config
    echo ""
    check_elasticsearch_config
    echo ""
    check_postgres_config
    echo ""
    check_tcvector_config
    echo ""
}

run_all() {
    print_header "Running All Vector Store Examples"
    echo ""
    
    local success=0
    local failed=0
    
    # Temporarily disable exit on error for run_all
    set +e
    
    echo ">>> Elasticsearch"
    if run_elasticsearch; then
        success=$((success + 1))
    else
        failed=$((failed + 1))
    fi
    echo ""
    
    echo ">>> PostgreSQL"
    if run_postgres; then
        success=$((success + 1))
    else
        failed=$((failed + 1))
    fi
    echo ""
    
    echo ">>> TCVectorDB"
    if run_tcvector; then
        success=$((success + 1))
    else
        failed=$((failed + 1))
    fi
    echo ""
    
    # Re-enable exit on error
    set -e
    
    print_header "Summary"
    echo -e "${GREEN}Successful: $success${NC}"
    echo -e "${RED}Failed: $failed${NC}"
}

# Main entry point
case "${1:-help}" in
    elasticsearch|es)
        run_elasticsearch
        ;;
    postgres|pg)
        run_postgres
        ;;
    tcvector|tc)
        run_tcvector
        ;;
    all)
        run_all
        ;;
    check)
        check_all_config
        ;;
    help|-h|--help)
        show_usage
        ;;
    *)
        print_error "Unknown option: $1"
        show_usage
        exit 1
        ;;
esac

