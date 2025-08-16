#!/bin/bash
# Lokey TRNG Service - Endpoint Test Script
#
# This script tests all API endpoints for the Lokey TRNG service including
# the Controller, Fortuna, and main API endpoints.

# Text formatting
BOLD="\033[1m"
RED="\033[31m"
GREEN="\033[32m"
YELLOW="\033[33m"
BLUE="\033[34m"
RESET="\033[0m"

# Service ports and hosts (modify as needed)
API_HOST="localhost"
API_PORT="8080"
CONTROLLER_HOST="localhost"
CONTROLLER_PORT="8081"
FORTUNA_HOST="localhost"
FORTUNA_PORT="8082"

# Set API base URLs
API_BASE="http://${API_HOST}:${API_PORT}"
CONTROLLER_BASE="http://${CONTROLLER_HOST}:${CONTROLLER_PORT}"
FORTUNA_BASE="http://${FORTUNA_HOST}:${FORTUNA_PORT}"

# Test variables
FAILED_TESTS=0
PASSED_TESTS=0
TOTAL_TESTS=0
SKIPPED_TESTS=0
WARNINGS=0

# Function to print section header
print_header() {
  echo
  echo -e "${BOLD}${BLUE}$1${RESET}"
  echo -e "${BLUE}$(printf '=%.0s' {1..50})${RESET}"
}

# Function to test an endpoint
test_endpoint() {
  local name=$1
  local url=$2
  local method=${3:-GET}
  local data=$4
  local expected_status=${5:-200}
  local optional=${6:-false}

  echo -e "\n${BOLD}Testing: ${name}${RESET}"
  echo -e "URL: ${url}"
  echo -e "Method: ${method}"

  TOTAL_TESTS=$((TOTAL_TESTS + 1))

  if [ "$method" == "GET" ]; then
    response=$(curl -s -w "\n%{http_code}" -X GET "${url}")
  elif [ "$method" == "POST" ]; then
    response=$(curl -s -w "\n%{http_code}" -X POST -H "Content-Type: application/json" -d "${data}" "${url}")
  elif [ "$method" == "PUT" ]; then
    response=$(curl -s -w "\n%{http_code}" -X PUT -H "Content-Type: application/json" -d "${data}" "${url}")
  else
    echo -e "${RED}Unsupported method: ${method}${RESET}"
    FAILED_TESTS=$((FAILED_TESTS + 1))
    return
  fi

  # Extract HTTP status code
  status_code=$(echo "$response" | tail -n1)
  # Extract response body
  body=$(echo "$response" | sed '$d')

  echo -e "Status: ${status_code}"
  echo -e "Response preview: ${body:0:120}..."

  # Special handling for 404 "No data available" which might be expected for data endpoints
  if [ "$status_code" -eq 404 ] && [[ "$body" == *"No data available"* ]] && [ "$optional" == "true" ]; then
    echo -e "${YELLOW}⚠ WARNING: No data available yet. This is expected if the system just started.${RESET}"
    WARNINGS=$((WARNINGS + 1))
    SKIPPED_TESTS=$((SKIPPED_TESTS + 1))
    return
  fi

  if [ "$status_code" -eq "$expected_status" ]; then
    echo -e "${GREEN}✓ PASSED${RESET}"
    PASSED_TESTS=$((PASSED_TESTS + 1))
  else
    echo -e "${RED}✗ FAILED (Expected ${expected_status}, got ${status_code})${RESET}"
    FAILED_TESTS=$((FAILED_TESTS + 1))
  fi
}

# Function to check if a service is available
check_service() {
  local name=$1
  local url=$2

  echo -e "\n${BOLD}Checking if ${name} is available...${RESET}"
  if curl -s --connect-timeout 3 "${url}" > /dev/null; then
    echo -e "${GREEN}✓ ${name} is available${RESET}"
    return 0
  else
    echo -e "${RED}✗ ${name} is not available at ${url}${RESET}"
    return 1
  fi
}

# Function to wait for data generation
wait_for_data_generation() {
  local attempts=$1
  local interval=$2
  local success=false

  print_header "Warming up - Waiting for data generation"
  echo "Triggering data generation and waiting for it to be available..."

  # Trigger data generation from controller and fortuna services
  curl -s "${CONTROLLER_BASE}/generate" > /dev/null
  curl -s "${FORTUNA_BASE}/generate" > /dev/null

  for ((i=1; i<=attempts; i++)); do
    echo "Attempt $i of $attempts - Waiting ${interval}s for data generation..."
    sleep $interval

    # Check if data is available by calling the status endpoint
    status_response=$(curl -s "${API_BASE}/api/v1/status")

    # Check if there's any unconsumed data
    if [[ "$status_response" == *"\"trng_unconsumed\":0"* ]] && [[ "$status_response" == *"\"fortuna_unconsumed\":0"* ]]; then
      echo -e "${YELLOW}No unconsumed data available yet...${RESET}"
    else
      echo -e "${GREEN}Data generation detected!${RESET}"
      success=true
      break
    fi
  done

  if [ "$success" = true ]; then
    echo -e "${GREEN}✓ Data generation successful${RESET}"
    return 0
  else
    echo -e "${YELLOW}⚠ WARNING: Data generation timeout - some tests may fail${RESET}"
    WARNINGS=$((WARNINGS + 1))
    return 1
  fi
}

# Print script banner
echo -e "${BOLD}${YELLOW}"
echo "╔═══════════════════════════════════════════════════╗"
echo "║       Lokey TRNG Service - Endpoint Tests         ║"
echo "╚═══════════════════════════════════════════════════╝"
echo -e "${RESET}"

# Check if curl is installed
if ! command -v curl &> /dev/null; then
    echo -e "${RED}Error: curl is not installed. Please install it to run this script.${RESET}"
    exit 1
fi

# Check if services are available
services_available=true

if ! check_service "API Service" "${API_BASE}/api/v1/health"; then
  services_available=false
fi

if ! check_service "Controller Service" "${CONTROLLER_BASE}/health"; then
  services_available=false
fi

if ! check_service "Fortuna Service" "${FORTUNA_BASE}/health"; then
  services_available=false
fi

if [ "$services_available" = false ]; then
  echo -e "\n${RED}${BOLD}Some services are not available. Make sure all services are running.${RESET}"
  echo -e "${YELLOW}Hint: Check if the docker containers are running with 'docker ps'${RESET}"
  exit 1
fi

# Try to warm up the system by waiting for data generation
wait_for_data_generation 5 2  # 5 attempts, 2 seconds each

# Test Controller Service Endpoints
print_header "Testing Controller Service Endpoints"

test_endpoint "Controller Health Check" "${CONTROLLER_BASE}/health"
test_endpoint "Controller Info" "${CONTROLLER_BASE}/info"
test_endpoint "Controller Generate Hash" "${CONTROLLER_BASE}/generate"

# Test Fortuna Service Endpoints
print_header "Testing Fortuna Service Endpoints"

test_endpoint "Fortuna Health Check" "${FORTUNA_BASE}/health"
test_endpoint "Fortuna Info" "${FORTUNA_BASE}/info"
test_endpoint "Fortuna Generate Random" "${FORTUNA_BASE}/generate"

# Test Main API Endpoints
print_header "Testing Main API Endpoints"

test_endpoint "API Health Check" "${API_BASE}/api/v1/health"
test_endpoint "API Status" "${API_BASE}/api/v1/status"

# Test API Config Endpoints
test_endpoint "Get Queue Configuration" "${API_BASE}/api/v1/config/queue"
test_endpoint "Update Queue Configuration" "${API_BASE}/api/v1/config/queue" "PUT" '{"trng_queue_size": 100, "fortuna_queue_size": 100}'

test_endpoint "Get Consumption Configuration" "${API_BASE}/api/v1/config/consumption"
test_endpoint "Update Consumption Configuration" "${API_BASE}/api/v1/config/consumption" "PUT" '{"delete_on_read": true}'

# Test Data Retrieval (marking these as optional since they might legitimately return 404)
test_endpoint "Get TRNG Data (int32)" "${API_BASE}/api/v1/data" "POST" '{"format":"int32","chunk_size":10,"limit":5,"offset":0,"source":"trng"}' 200 true
test_endpoint "Get Fortuna Data (binary)" "${API_BASE}/api/v1/data" "POST" '{"format":"binary","chunk_size":32,"limit":1,"offset":0,"source":"fortuna"}' 200 true
test_endpoint "Get Fortuna Data (uint64)" "${API_BASE}/api/v1/data" "POST" '{"format":"uint64","chunk_size":8,"limit":10,"offset":0,"source":"fortuna"}' 200 true

# Print summary
print_header "Test Summary"

echo -e "Total tests run: ${BOLD}${TOTAL_TESTS}${RESET}"
echo -e "Tests passed: ${BOLD}${GREEN}${PASSED_TESTS}${RESET}"
echo -e "Tests failed: ${BOLD}${RED}${FAILED_TESTS}${RESET}"
echo -e "Tests skipped: ${BOLD}${YELLOW}${SKIPPED_TESTS}${RESET}"
echo -e "Warnings: ${BOLD}${YELLOW}${WARNINGS}${RESET}"

if [ $FAILED_TESTS -eq 0 ]; then
  if [ $WARNINGS -eq 0 ]; then
    echo -e "\n${GREEN}${BOLD}All tests passed! The Lokey TRNG service is working correctly.${RESET}"
  else
    echo -e "\n${YELLOW}${BOLD}All critical tests passed with some warnings. The system is functional but some features may not be fully available yet.${RESET}"
  fi
  exit 0
else
  echo -e "\n${RED}${BOLD}Some tests failed. Please check the logs above for details.${RESET}"
  echo -e "\n${YELLOW}Potential issues to investigate:${RESET}"
  echo -e "1. ${YELLOW}API config endpoints returning 500 - Check API service logs for error details${RESET}"
  echo -e "2. ${YELLOW}Data not available - Ensure the controller and fortuna services are generating data${RESET}"
  echo -e "3. ${YELLOW}Database connectivity - Verify all services can access their databases${RESET}"
  exit 1
fi