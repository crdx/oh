mod release

set quiet := true
set shell := ["bash", "-cu", "-o", "pipefail"]

import? 'local.just'

set positional-arguments

LINT_CACHE := env('GOLANGCI_LINT_CACHE', home_directory() / '.cache' / 'golangci-lint')

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

test:
    go test -cover ./...

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
    PACKAGES=(./internal/sandbox ./internal/jobs ./internal/app/shell ./pkg/toolbox/bash)
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
        generate_goldens ./internal/app/harness -run "^TestGolden${PATTERN}"
    done

    generate_goldens \
        ./internal/app/cli \
        ./internal/app/commands \
        ./internal/app/demo \
        ./internal/app/editor \
        ./internal/app/menu \
        ./internal/app/model \
        ./internal/app/model/picker \
        ./internal/app/onboarding \
        ./internal/app/preview \
        ./internal/app/segment/subUsage \
        ./internal/app/sessions \
        ./internal/app/sessions/picker \
        ./internal/app/shell \
        ./internal/app/toolresult \
        ./internal/app/usage \
        ./internal/app/ctl/... \
        ./pkg/toolbox/bash \
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
    trap 'rm -f "$PROFILE"' EXIT
    go test ./... -coverpkg=./... -coverprofile="$PROFILE" -count=1 > /dev/null
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
    trap 'rm -f "$PROFILE"' EXIT
    go test ./... -coverpkg=./{{ package }}/... -coverprofile="$PROFILE" -count=1 > /dev/null
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

ohm *args:
    go run . -crx "$@"

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
