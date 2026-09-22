set quiet := true
set shell := ["bash", "-cu", "-o", "pipefail"]

SESSION   := "io-rig"
OH        := "/tmp/io-rig.oh"
SIM       := "/tmp/io-rig.sim"
SCENARIO  := "internal/sim/scenarios/success.toml"
MODEL     := "codex/gpt-5.6-sol@high"
WORKSPACE := "tmp/workspace"
STATE     := justfile_directory() / "tmp/rig-state"
SESSIONS  := STATE / "org.crdx/oh/sessions"
ADDRESS   := "localhost:8117"
ENDPOINT  := "http://" + ADDRESS + "/v1/codex/responses"
WIDTH     := "100"
HEIGHT    := "24"
TEST_INPUTS := justfile_directory() / "testdata/input"

# what the scenarios read, write and search, laid out again for every capture and every replay
WORKSPACE_FIXTURE := justfile_directory() / "testdata/input/workspace"

# build the harness and the stand-in endpoint
harness:
    go build -o {{ OH }} ./
    go build -o {{ SIM }} ./cmd/simulate/

# run the harness for real against a scenario, in a workspace of its own
live scenario=SCENARIO workspace=WORKSPACE opening="" caps="rxw": harness down
    mkdir -p {{ workspace }}
    tmux new-session -d -s {{ SESSION }} -x {{ WIDTH }} -y {{ HEIGHT }} \
        {{ SIM }} --scenario {{ scenario }} --address {{ ADDRESS }}
    tmux set-option -t {{ SESSION }} remain-on-exit on
    sleep 0.3
    OH_ENDPOINT_URL={{ ENDPOINT }} {{ OH }} -u >/dev/null
    tmux split-window -t {{ SESSION }} -v -c {{ justfile_directory() }}/{{ workspace }} \
        -e OH_ENDPOINT_URL={{ ENDPOINT }} \
        {{ OH }} --caps {{ caps }} -m {{ MODEL }} {{ opening }}
    tmux select-pane -t {{ SESSION }}.{bottom}

# start an isolated session whose read snapshot can be resumed
resume-up scenario="internal/sim/scenarios/resume-read.toml": harness down
    rm -rf {{ STATE }} {{ WORKSPACE }}
    mkdir -p {{ STATE }} {{ WORKSPACE }}
    printf 'one\n' > {{ WORKSPACE }}/target.txt
    tmux new-session -d -s {{ SESSION }} -x {{ WIDTH }} -y {{ HEIGHT }} \
        {{ SIM }} --scenario {{ scenario }} --address {{ ADDRESS }}
    tmux set-option -t {{ SESSION }} remain-on-exit on
    sleep 0.3
    OH_ENDPOINT_URL={{ ENDPOINT }} XDG_STATE_HOME={{ STATE }} {{ OH }} -u >/dev/null
    tmux split-window -t {{ SESSION }} -v -c {{ justfile_directory() }}/{{ WORKSPACE }} \
        -e OH_ENDPOINT_URL={{ ENDPOINT }} -e XDG_STATE_HOME={{ STATE }} \
        {{ OH }} --caps rw -m {{ MODEL }}
    tmux select-pane -t {{ SESSION }}.{bottom}

# restart the fake endpoint and resume the isolated session
resume scenario="internal/sim/scenarios/resume-edit.toml": harness
    #!/bin/bash
    set -euo pipefail
    ID="$(find {{ SESSIONS }} -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | head -n 1)"
    test -n "$ID"
    tmux respawn-pane -k -t {{ SESSION }}.{top} \
        {{ SIM }} --scenario {{ scenario }} --address {{ ADDRESS }}
    sleep 0.3
    tmux respawn-pane -k -t {{ SESSION }}.{bottom} -c {{ justfile_directory() }}/{{ WORKSPACE }} \
        -e OH_ENDPOINT_URL={{ ENDPOINT }} -e XDG_STATE_HOME={{ STATE }} \
        {{ OH }} --caps rw --resume "$ID"
    tmux select-pane -t {{ SESSION }}.{bottom}

# stop the harness while leaving its fake endpoint available
resume-stop:
    tmux respawn-pane -k -t {{ SESSION }}.{bottom} /bin/true

# replace the resume target while the harness is stopped
resume-target content:
    printf '%s\n' '{{ content }}' > {{ WORKSPACE }}/target.txt

# print the isolated session journal
resume-journal:
    #!/bin/bash
    set -euo pipefail
    ID="$(find {{ SESSIONS }} -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | head -n 1)"
    test -n "$ID"
    cat {{ SESSIONS }}/$ID/session.jsonl

# show the current resume target contents
resume-target-show:
    cat {{ WORKSPACE }}/target.txt

# capture the journal fixtures the golden replay test draws from
capture: harness
    #!/bin/bash
    set -euo pipefail

    function ask {
        echo "have a look at that" |
            (cd "$CAPTURE_WORKSPACE" && {{ OH }} "$@" >/dev/null 2>&1)
    }

    declare -A UNDER=(
        [error]="rxw"
        [failure]="rxw"
        [markdown]="rxw"
        [parallel]="rxw r"
        [paths]="rxw r"
        [resume-edit]="rxw r rw"
        [resume-grep]="rxw"
        [resume-read]="rxw"
        [retry]="rxw"
        [serial]="rxw r rw rx rxwgb"
        [success]="rxw rxwgb"
    )

    declare -A ASKED=(
        [error]=1
        [failure]=3
        [markdown]=1
        [parallel]=1
        [paths]=1
        [resume-edit]=1
        [resume-grep]=1
        [resume-read]=1
        [retry]=2
        [serial]=1
        [success]=1
    )

    CAPTURE_STATE={{ justfile_directory() }}/tmp/capture-state
    CAPTURE_WORKSPACE={{ justfile_directory() }}/tmp/capture-workspace

    export OH_ENDPOINT_URL={{ ENDPOINT }}
    export XDG_STATE_HOME=$CAPTURE_STATE

    mkdir -p {{ TEST_INPUTS }}

    for SCENARIO in internal/sim/scenarios/*.toml; do
      NAME=$(basename "$SCENARIO" .toml)

      for CAPS in ${UNDER[$NAME]}; do
        rm -rf "$CAPTURE_STATE" "$CAPTURE_WORKSPACE"
        mkdir -p "$CAPTURE_STATE"
        cp -r {{ WORKSPACE_FIXTURE }} "$CAPTURE_WORKSPACE"

        {{ SIM }} --scenario "$SCENARIO" --address {{ ADDRESS }} >/dev/null 2>&1 &
        SIM_PID=$!
        trap 'kill $SIM_PID 2>/dev/null || true' EXIT
        sleep 0.3

        {{ OH }} -u >/dev/null 2>&1

        ask -c "$CAPS" -m {{ MODEL }}

        JOURNAL=$(find "$CAPTURE_STATE" -name session.jsonl | head -n 1)
        SESSION_ID=$(basename "$(dirname "$JOURNAL")")

        for _ in $(seq 2 "${ASKED[$NAME]}"); do
            ask -c "$CAPS" --resume "$SESSION_ID"
        done

        kill $SIM_PID 2>/dev/null || true
        trap - EXIT

        jq -c --arg blank 000000000000000000000000 'select(.kind == "event") |
               {event: (.event
                        | if .took then .took = 0 else . end
                        | if .state.session then .state.session = $blank else . end)}' \
           "$JOURNAL" > {{ TEST_INPUTS }}/$NAME@$CAPS.jsonl

        echo "captured $NAME@$CAPS ($(wc -l < {{ TEST_INPUTS }}/$NAME@$CAPS.jsonl) events)"
      done
    done

    rm -rf "$CAPTURE_STATE" "$CAPTURE_WORKSPACE"

    echo "now write the goldens: just -f rig.just goldens"

# draw every captured journal again and write what it drew to the golden files
goldens:
    go test ./ -update -count=1

# attach to the interactive rig session
attach:
    tmux attach-session -t {{ SESSION }}

# print process status and scrollback for both resume panes
resume-status:
    #!/bin/bash
    set -euo pipefail
    FORMAT='#{pane_index} #{pane_dead} #{pane_dead_status} #{pane_current_command}'
    tmux list-panes -t {{ SESSION }} -F "$FORMAT"
    for POSITION in top bottom; do
        echo "--- $POSITION ---"
        tmux capture-pane -p -S - -t {{ SESSION }}.$POSITION
    done

# type into the harness pane and press return
say text:
    tmux send-keys -t {{ SESSION }}.{bottom} -l '{{ text }}'
    tmux send-keys -t {{ SESSION }}.{bottom} Enter

# resize the rig terminal to trigger a full conversation repaint
resize width=WIDTH height=HEIGHT:
    tmux resize-window -t {{ SESSION }} -x {{ width }} -y {{ height }}

# settle and capture the pane -> tmp/<date>-<name>.txt
shot name: settle
    mkdir -p tmp
    tmux capture-pane -p -t {{ SESSION }} > tmp/$(date +%F)-{{ name }}.txt
    cat tmp/$(date +%F)-{{ name }}.txt

# as shot, but with the escape sequences left in, for checking a colour
sgr name: settle
    mkdir -p tmp
    tmux capture-pane -p -e -t {{ SESSION }} > tmp/$(date +%F)-{{ name }}.txt
    cat -v tmp/$(date +%F)-{{ name }}.txt

# as shot, numbered and with visible line ends, for spotting a stray blank line
lines name: settle
    mkdir -p tmp
    tmux capture-pane -p -t {{ SESSION }} > tmp/$(date +%F)-{{ name }}.txt
    cat -nA tmp/$(date +%F)-{{ name }}.txt

# type text into the pane, without pressing return
send text:
    tmux send-keys -t {{ SESSION }} -l '{{ text }}'

# press a key by name (Enter, Escape, C-c)
press key:
    tmux send-keys -t {{ SESSION }} '{{ key }}'

# send a raw escape sequence, for the keys tmux has no name for (e.g. '\e[1;5D' for ctrl+left)
raw sequence:
    tmux send-keys -t {{ SESSION }} -l "$(printf '{{ sequence }}')"

# wait for the app to finish redrawing before anything captures it
[private]
settle:
    sleep 0.4

# kill the rig
down:
    tmux kill-session -t {{ SESSION }} 2>/dev/null || true
