#!/usr/bin/env bash

set -euo pipefail

repository_root="$(cd "$(dirname "$0")/.." && pwd)"
probe="$repository_root/scripts/eiszhs-probe"
test_directory="$(mktemp -d "${TMPDIR:-/tmp}/eiszhs-probe-test.XXXXXX")"

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

assert_not_contains() {
  local output="$1"
  local unexpected="$2"
  [[ "$output" != *"$unexpected"* ]] || fail "output contained sensitive value: $unexpected"
}

help_output="$("$probe" --help)"
assert_contains "$help_output" 'flags[5]{name,default,description}:'

home_output="$("$probe")"
assert_contains "$home_output" 'annual_requests: 100000'
assert_contains "$home_output" 'network_requests_sent: 0'

set +e
unknown_output="$("$probe" --unknown 2>&1)"
unknown_status=$?
set -e
[[ $unknown_status -eq 2 ]] || fail "unknown flag should exit with status 2"
assert_contains "$unknown_output" 'code: usage_error'

set +e
insufficient_budget_output="$("$probe" --request-budget 11 --full 2>&1)"
insufficient_budget_status=$?
set -e
[[ $insufficient_budget_status -eq 2 ]] || fail "full probe with an insufficient budget should exit with status 2"
assert_contains "$insufficient_budget_output" '--full requires --request-budget 12'

curl() {
  set -euo pipefail

  local output_file=""
  local url=""
  printf '%s\n' "$@" >>"$FAKE_CURL_ARG_LOG"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --output)
        output_file="$2"
        shift 2
        ;;
      --write-out|--max-time|--header|--data-binary)
        shift 2
        ;;
      --silent|--show-error)
        shift
        ;;
      *)
        url="$1"
        shift
        ;;
    esac
  done

  case "$url" in
    */protocol/openid-connect/token)
      printf '%s' '{"access_token":"test-access-token","expires_in":300,"token_type":"bearer","scope":"objects.read"}' >"$output_file"
      ;;
    */version)
      printf '%s' '{"projectVersion":"1.22.0"}' >"$output_file"
      ;;
    */construction-objects*)
      printf '%s' '{"total":123,"limitItems":1,"elements":{"constructionObjectShortInfo":[{"objectId":42}]}}' >"$output_file"
      ;;
    *)
      printf '%s' '{}' >"$output_file"
      ;;
  esac

  printf '200'
}

sleep() {
  :
}

stat() {
  if [[ "$1" == "-c" && "$2" == "%a" ]]; then
    printf '%s\n' "${FAKE_STAT_MODE:-600}"
    return 0
  fi

  if [[ "$1" == "-f" && "$2" == "%Lp" ]]; then
    printf 'simulated GNU stat filesystem output\n'
    return 1
  fi

  command stat "$@"
}

export -f curl sleep stat
export FAKE_CURL_ARG_LOG="$test_directory/curl-args.log"
test_env_file="$test_directory/credentials.env"
printf 'CLIENT_ID=%s\nCLIENT_SECRET=%s\n' 'test-client-id' 'test-client-secret' >"$test_env_file"
chmod 600 "$test_env_file"

set +e
insecure_mode_output="$(FAKE_STAT_MODE=644 "$probe" --env-file "$test_env_file" --request-budget 2 2>&1)"
insecure_mode_status=$?
set -e
[[ $insecure_mode_status -eq 1 ]] || fail "insecure credential mode should exit with status 1"
assert_contains "$insecure_mode_output" 'code: "insecure_credentials_file"'

set +e
probe_output="$("$probe" --env-file "$test_env_file" --request-budget 2 2>&1)"
probe_status=$?
set -e
[[ $probe_status -eq 0 ]] || fail "minimal probe failed: $probe_output"
assert_contains "$probe_output" 'authentication: ok'
assert_contains "$probe_output" 'api_requests_sent: 2'
assert_contains "$probe_output" 'successful_requests_counted: 2'
assert_contains "$probe_output" '"version","ok",200,null,null,"1.22.0"'
assert_contains "$probe_output" '"construction_objects","ok",200,123,3,""'
assert_not_contains "$probe_output" 'test-client-id'
assert_not_contains "$probe_output" 'test-client-secret'
assert_not_contains "$probe_output" 'test-access-token'

set +e
full_probe_output="$("$probe" --env-file "$test_env_file" --request-budget 12 --full 2>&1)"
full_probe_status=$?
set -e
[[ $full_probe_status -eq 0 ]] || fail "full probe failed: $full_probe_output"
assert_contains "$full_probe_output" 'api_requests_sent: 12'
assert_contains "$full_probe_output" 'successful_requests_counted: 12'
assert_contains "$full_probe_output" 'checks[12]{id,status,http,total,pages,detail}:'
assert_not_contains "$full_probe_output" 'test-client-id'
assert_not_contains "$full_probe_output" 'test-client-secret'
assert_not_contains "$full_probe_output" 'test-access-token'

curl_arguments="$(<"$FAKE_CURL_ARG_LOG")"
assert_not_contains "$curl_arguments" 'test-client-id'
assert_not_contains "$curl_arguments" 'test-client-secret'
assert_not_contains "$curl_arguments" 'test-access-token'

printf 'PASS: eiszhs-probe\n'
