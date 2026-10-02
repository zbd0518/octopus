#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${script_dir}/build.sh"
trap - ERR

fail() {
    printf 'FAIL: %s\n' "$1" >&2
    exit 1
}

for failed_stage in build docker checksums archives none; do
    (
        prepare_environment() { return 0; }
        build_frontend() { return 0; }
        update_price() { return 0; }
        builds=0
        build_standard() {
            builds=$((builds + 1))
            [[ "$failed_stage" != build ]]
        }
        prepare_docker_binaries() { [[ "$failed_stage" != docker ]]; }
        generate_checksums() { [[ "$failed_stage" != checksums ]]; }
        create_archives() { [[ "$failed_stage" != archives ]]; }
        output="$(mktemp)"
        trap 'rm -f "$output"' EXIT
        if main release >"$output" 2>&1; then
            [[ "$failed_stage" == none ]] || fail "release accepted a ${failed_stage} failure"
            [[ "$builds" -eq 8 ]] || fail "release skipped a platform"
            grep -q 'All artifacts ready' "$output" || fail "successful release omitted its completion message"
        else
            [[ "$failed_stage" != none ]] || fail "successful stages returned failure"
            ! grep -q 'All artifacts ready' "$output" || fail "failed release reported success"
            if [[ "$failed_stage" == build ]]; then
                [[ "$builds" -eq 1 ]] || fail "release continued after a platform failure"
            fi
        fi
    )
done

fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
cd "$fixture"
mkdir -p build/bin build/archives build/docker
printf 'fixture documentation\n' >README.md
printf 'fixture license\n' >LICENSE

if generate_checksums >/dev/null 2>&1; then
    fail "checksums accepted an empty binary directory"
fi
if create_archives >/dev/null 2>&1; then
    fail "archiving accepted an empty binary directory"
fi
if prepare_docker_binaries >/dev/null 2>&1; then
    fail "Docker preparation accepted missing binaries"
fi

for arch in x86_64 arm64 armv7 x86; do
    printf 'fixture binary\n' >"build/bin/octopus-linux-${arch}"
done
prepare_docker_binaries >/dev/null
generate_checksums >/dev/null
[[ "$(wc -l <build/bin/md5.txt)" -eq 4 ]] || fail "checksums omitted binaries"

(
    archive_zip() { return 1; }
    if create_archives >/dev/null 2>&1; then
        fail "archiving swallowed a compression failure"
    fi
)
archive_zip() { touch "$1/$2"; }
before="$PWD"
create_archives >/dev/null
[[ "$PWD" == "$before" ]] || fail "archiving changed the working directory"
[[ "$(find build/archives -name '*.zip' -type f | wc -l)" -eq 4 ]] || fail "archiving omitted binaries"
printf 'PASS: release failure handling and artifact helpers\n'
