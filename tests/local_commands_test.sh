#!/usr/bin/env bash

set -euo pipefail

repository_root="$(cd "$(dirname "$0")/.." && pwd)"
docker_probe="$repository_root/tests/fixtures/docker-probe"
test_directory="$(mktemp -d "${TMPDIR:-/tmp}/agent-interface-test.XXXXXX")"
marker="$test_directory/injected"

cleanup() {
  rm -rf -- "$test_directory"
}
trap cleanup EXIT INT TERM

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

assert_contains() {
  local output="$1"
  local expected="$2"
  [[ "$output" == *"$expected"* ]] || fail "expected output to contain: $expected"
}

query_payload='select 1; $(touch '"$marker"')'
query_output="$(make --no-print-directory --directory="$repository_root" \
  db-query DOCKER="$docker_probe" QUERY="$query_payload")"
[[ ! -e "$marker" ]] || fail "db-query evaluated QUERY as shell code"
assert_contains "$query_output" "$query_payload"

set +e
missing_query_output="$(make --no-print-directory --directory="$repository_root" \
  db-query DOCKER="$docker_probe" 2>/dev/null)"
missing_query_status=$?
set -e
[[ $missing_query_status -eq 2 ]] || fail "missing QUERY should exit with status 2"
assert_contains "$missing_query_output" 'code: usage_error'

service_payload='app; touch '"$marker"
set +e
service_output="$(make --no-print-directory --directory="$repository_root" \
  logs-service DOCKER="$docker_probe" SERVICE="$service_payload" 2>/dev/null)"
service_status=$?
set -e
[[ $service_status -eq 2 ]] || fail "invalid SERVICE should exit with status 2"
[[ ! -e "$marker" ]] || fail "logs-service evaluated SERVICE as shell code"
assert_contains "$service_output" 'code: usage_error'

key_payload='object$(touch '"$marker"')'
key_output="$(make --no-print-directory --directory="$repository_root" \
  s3-get DOCKER="$docker_probe" KEY="$key_payload")"
[[ ! -e "$marker" ]] || fail "s3-get evaluated KEY as shell code"
assert_contains "$key_output" "$key_payload"

file_payload='README.md$(touch '"$marker"')'
set +e
file_output="$(make --no-print-directory --directory="$repository_root" \
  s3-put DOCKER="$docker_probe" FILE="$file_payload" KEY=object 2>/dev/null)"
file_status=$?
set -e
[[ $file_status -eq 2 ]] || fail "invalid FILE should exit with status 2"
[[ ! -e "$marker" ]] || fail "s3-put evaluated FILE as shell code"
assert_contains "$file_output" 'code: usage_error'

printf 'PASS: local-commands\n'
