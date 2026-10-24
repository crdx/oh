mod release

set quiet := true
set shell := ["bash", "-cu", "-o", "pipefail"]

import? 'local.just'

set positional-arguments

LINT_CACHE := env('GOLANGCI_LINT_CACHE', home_directory() / '.cache' / 'golangci-lint')
TEST_CACHE := env('XDG_CACHE_HOME', home_directory() / '.cache') / 'oh' / 'test'
TEST_BINARY := TEST_CACHE / sha256(justfile_directory()) / 'oh'
HARNESS_SHARD_COUNT := "8"

export GOLANGCI_LINT_CACHE := LINT_CACHE / sha256(justfile_directory())

[private]
help:
    just --list --unsorted --list-submodules

dev:
    watchexec --debounce 500ms just check

fmt:
    go fmt ./...

fix:
    golangci-lint run --color never --fix

# run every test, naming the failures last
test:
    #!/bin/bash
    set -euo pipefail
    RED='\e[31m'
    YELLOW='\e[33m'
    NC='\e[0m'
    SHARD_COUNT={{ HARNESS_SHARD_COUNT }}
    LOG_DIRECTORY=$(mktemp -d -t oh-test.XXXXXX)
    trap 'rm -rf "$LOG_DIRECTORY"' EXIT
    PIDS=()
    LOGS=()
    ALL_LOGS=()
    STATUS=0

    function run_tests {
        local NAME=$1
        shift
        local LOG="$LOG_DIRECTORY/$NAME.log"
        go test "$@" >"$LOG" 2>&1 &
        PIDS+=("$!")
        LOGS+=("$LOG")
        ALL_LOGS+=("$LOG")
    }

    function wait_for_tests {
        for I in "${!PIDS[@]}"; do
            if ! wait "${PIDS[$I]}"; then
                STATUS=1
            fi
            cat "${LOGS[$I]}"
        done
        PIDS=()
        LOGS=()
    }

    echo -e "${YELLOW}Preparing tests…${NC}"
    PACKAGE_LIST=$(go list ./...)
    mapfile -t PACKAGES < <(
        printf '%s\n' "$PACKAGE_LIST" |
            grep -vx 'crdx.org/oh/internal/app/harness'
    )
    HARNESS_TEST_LIST=$(go test ./internal/app/harness -list '.')
    mapfile -t HARNESS_TESTS < <(
        printf '%s\n' "$HARNESS_TEST_LIST" |
            grep -E '^(Test|Example|Fuzz)' |
            grep -vx 'TestGoldenScenariosProduceCanonicalOutputs'
    )

    export OH_TEST_BINARY='{{ TEST_BINARY }}'
    mkdir -p "$(dirname "$OH_TEST_BINARY")"
    go build -tags=oh_grep_test -o "$OH_TEST_BINARY" crdx.org/oh
    export OH_TEST_BINARY_BUILD_ID
    OH_TEST_BINARY_BUILD_ID=$(go tool buildid "$OH_TEST_BINARY")

    echo -e "${YELLOW}Running tests in parallel…${NC}"
    run_tests packages "${PACKAGES[@]}"
    for ((SHARD = 0; SHARD < SHARD_COUNT; SHARD++)); do
        PATTERN=$(
            printf '%s\n' "${HARNESS_TESTS[@]}" |
                awk -v shard="$SHARD" -v count="$SHARD_COUNT" '(NR - 1) % count == shard' |
                paste -sd '|' -
        )
        run_tests "harness-$SHARD" \
            ./internal/app/harness \
            -run "^(${PATTERN})$"
    done
    wait_for_tests

    echo -e "${YELLOW}Running scenario tests in parallel…${NC}"
    for ((SHARD = 0; SHARD < SHARD_COUNT; SHARD++)); do
        run_tests "scenarios-$SHARD" \
            ./internal/app/harness \
            -run '^TestGoldenScenariosProduceCanonicalOutputs$' \
            -session-golden-shard "$SHARD" \
            -session-golden-shard-count "$SHARD_COUNT"
    done
    wait_for_tests

    if [[ $STATUS -ne 0 ]]; then
        echo -e "${RED}Failed:${NC}"
        grep -hE '^ *--- FAIL|^FAIL\s|^panic: ' "${ALL_LOGS[@]}" || true
    fi
    exit "$STATUS"

# run the suite rounds times, width at once, and report every test that failed
flakes rounds='3' width='4' *packages:
    ./script/flakes {{ rounds }} {{ width }} {{ packages }}

# run the sandbox tests without privs
sandbox *args:
    #!/bin/bash
    set -euo pipefail
    GREEN='\e[32m'
    NC='\e[0m'
    if (exec 3>>/proc/self/uid_map) 2>/dev/null; then
        echo -e "${GREEN}this machine can map a namespace, so the sandbox tests ran for real${NC}"
        exit 0
    fi
    PACKAGES=(
        ./internal/sandbox
        ./internal/sandbox/keeper
        ./internal/app/shell
        ./internal/app/segment/gitStatus
        ./pkg/toolbox/bash
    )
    if [[ $# -gt 0 ]]; then
        PACKAGES=("$@")
    fi
    export IO_SANDBOX_TEST_UNMAPPED=1
    go test -count=1 "${PACKAGES[@]}"

# run a fuzzing campaign against one target, for a minute unless told otherwise
fuzz package target time='1m':
    go test ./{{ package }} -run '^$' -fuzz '^{{ target }}$' -fuzztime {{ time }}

# generate every golden
golden:
    #!/bin/bash
    set -euo pipefail
    RED='\e[31m'
    GREEN='\e[32m'
    YELLOW='\e[33m'
    NC='\e[0m'

    PIDS=()
    LOGS=()
    trap 'rm -f "${LOGS[@]}"' EXIT
    export OH_TEST_BINARY='{{ TEST_BINARY }}'
    mkdir -p "$(dirname "$OH_TEST_BINARY")"
    go build -tags=oh_grep_test -o "$OH_TEST_BINARY" crdx.org/oh
    export OH_TEST_BINARY_BUILD_ID
    OH_TEST_BINARY_BUILD_ID=$(go tool buildid "$OH_TEST_BINARY")

    function generate_goldens {
        local LOG
        LOG=$(mktemp -t io-golden.XXXXXX)
        go test "$@" -count=1 -update >"$LOG" 2>&1 &
        PIDS+=("$!")
        LOGS+=("$LOG")
    }

    echo -e "${YELLOW}Generating goldens…${NC}"

    # isolate shards because the generator harness mutates process-wide state
    for PATTERN in '[A-D]' '[E-L]' '[M-R]' '[S-Z]'; do
        generate_goldens \
            ./internal/app/harness \
            -run "^TestGolden${PATTERN}" \
            -skip-session-goldens
    done
    for ((SHARD = 0; SHARD < {{ HARNESS_SHARD_COUNT }}; SHARD++)); do
        generate_goldens \
            ./internal/app/harness \
            -run '^TestGoldenScenariosProduceCanonicalOutputs$' \
            -session-golden-shard "$SHARD" \
            -session-golden-shard-count {{ HARNESS_SHARD_COUNT }}
    done

    generate_goldens \
        ./internal/app/cli \
        ./internal/app/commands \
        ./internal/app/demo \
        ./internal/app/menu \
        ./internal/app/model \
        ./internal/app/model/picker \
        ./internal/app/onboarding \
        ./internal/app/painter \
        ./internal/app/preview \
        ./internal/app/segment/subUsage \
        ./internal/app/sessions \
        ./internal/app/sessions/picker \
        ./internal/app/shell \
        ./internal/app/toolresult \
        ./internal/app/usage \
        ./internal/app/ctl/... \
        ./pkg/session \
        ./pkg/tool/command \
        ./pkg/toolbox/bash \
        ./pkg/toolbox/read \
        -run '^TestGolden'

    STATUS=0
    for i in "${!PIDS[@]}"; do
        if ! wait "${PIDS[$i]}"; then
            cat "${LOGS[$i]}"
            STATUS=1
        fi
    done
    if [[ $STATUS -eq 0 ]]; then
        echo -e "${GREEN}Goldens generated${NC}"
    else
        echo -e "${RED}Golden generation failed${NC}"
    fi
    exit "$STATUS"

# what every package covers, least covered first
cov *args:
    #!/bin/bash
    set -euo pipefail
    PROFILE=$(mktemp -t io-cover.XXXXXX)
    BINARY=$(mktemp -d -t io-cover-binary.XXXXXX)
    trap 'rm -rf "$PROFILE" "$BINARY"' EXIT
    export OH_TEST_BINARY_COVERAGE="$BINARY"
    go test ./... -coverpkg=./... -coverprofile="$PROFILE" -count=1 > /dev/null
    go tool covdata textfmt -i="$BINARY" -o "$BINARY/profile"
    tail -n +2 "$BINARY/profile" >> "$PROFILE"
    if [[ $# -gt 0 ]]; then
        go tool cover -func="$PROFILE" | grep -E "$1" | grep -v " 100.0%$"
        exit
    fi
    ./script/coverage "$PROFILE"

# open the coverage of one package in a browser
covhtml package:
    #!/bin/bash
    set -euo pipefail
    PROFILE=$(mktemp -t io-cover.XXXXXX)
    BINARY=$(mktemp -d -t io-cover-binary.XXXXXX)
    trap 'rm -rf "$PROFILE" "$BINARY"' EXIT
    export OH_TEST_BINARY_COVERAGE="$BINARY"
    go test ./... -coverpkg=./{{ package }}/... -coverprofile="$PROFILE" -count=1 > /dev/null
    go tool covdata textfmt -i="$BINARY" -pkg=crdx.org/oh/{{ package }}/... -o "$BINARY/profile"
    tail -n +2 "$BINARY/profile" >> "$PROFILE"
    go tool cover -html="$PROFILE"

check:
    steps fmt vet lint1 lint2 lint3 mega test sandbox

# download the API references for each wire format
refs:
    #!/bin/bash
    set -euo pipefail
    GREEN='\e[32m'
    NC='\e[0m'
    for SOURCES in pkg/wire/*/*/reference/sources.txt; do
        DIRECTORY="$(dirname "$SOURCES")"
        while read -r NAME ADDRESS; do
            if [[ -z "$NAME" || "$NAME" == \#* ]]; then
                continue
            fi
            curl -fsSL --retry 2 -o "$DIRECTORY/$NAME" "$ADDRESS"
            echo -e "${GREEN}${DIRECTORY}/${NAME}${NC} $(wc -c < "$DIRECTORY/$NAME")B"
        done < "$SOURCES"
        mega -x "$DIRECTORY"
    done

oh *args:
    go run . "$@"

[private]
mega:
    mega

[private]
vet:
    go vet ./...

[private]
lint1:
    golangci-lint run --color never

[private]
lint2:
    #!/bin/bash
    set -euo pipefail
    STATUS=0
    OUTPUT="$(fd -tf -g '*.go' | xargs gopls check -severity=hint 2>&1)" || STATUS=$?
    if [[ $STATUS -ne 0 || -n "$OUTPUT" ]]; then
        echo "$OUTPUT"
        exit 1
    fi

[private]
lint3:
    fd -tf -e go -X go run ./internal/lint/receivername
    fd -tf -e go -X go run ./internal/lint/stdstream
    fd -tf -e go -X go run ./internal/lint/abbreviation
    fd -tf -e go -X go run ./internal/lint/boolname
    fd -tf -e go -E '*_test.go' -X go run ./internal/lint/adjective
    fd -tf -e go -E '*_test.go' -X go run ./internal/lint/palette
