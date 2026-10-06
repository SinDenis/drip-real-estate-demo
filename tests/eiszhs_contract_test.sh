#!/usr/bin/env bash

set -euo pipefail

repository_root="$(cd "$(dirname "$0")/.." && pwd)"
openapi="$repository_root/specs/eiszhs/openapi-1.22.0.json"
env_example="$repository_root/.env.example"
readonly expected_sha256="0d2a892a10074389dda7c6e903ca85ae6f2206db5423066dff448c3cfc55bc2e"

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

for dependency in git jq; do
  command -v "$dependency" >/dev/null 2>&1 || fail "missing dependency: $dependency"
done

sha256_file() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  elif command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    fail "missing SHA-256 command"
  fi
}

[[ -f "$openapi" ]] || fail "pinned OpenAPI is missing"
actual_sha256="$(sha256_file "$openapi")"
[[ "$actual_sha256" == "$expected_sha256" ]] || fail "pinned OpenAPI checksum changed"

jq -e '
  .openapi == "3.0.1" and
  .info.version == "1.22.0" and
  (.paths | length) == 60 and
  (.components.securitySchemes // null) == null and
  (.paths as $paths | all([
      "/version",
      "/complexes",
      "/construction-objects",
      "/construction-objects/{object-id}/extended",
      "/construction-objects/{object-id}/versions",
      "/developers",
      "/company-groups",
      "/problem-objects",
      "/general-contractor"
    ][]; . as $required | $paths | has($required)))
' "$openapi" >/dev/null || fail "pinned OpenAPI no longer matches the verified contract"

git -C "$repository_root" check-ignore -q -- .env || fail ".env is not ignored by Git"
if git -C "$repository_root" ls-files --error-unmatch .env >/dev/null 2>&1; then
  fail ".env must never be tracked by Git"
fi

[[ -f "$env_example" ]] || fail ".env.example is missing"
expected_env_example="$(printf 'CLIENT_ID=\nCLIENT_SECRET=')"
actual_env_example="$(<"$env_example")"
[[ "$actual_env_example" == "$expected_env_example" ]] || fail ".env.example must contain only empty CLIENT_ID and CLIENT_SECRET entries"

printf 'PASS: eiszhs-contract\n'
