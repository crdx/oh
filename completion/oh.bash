function _oh {
    local LINE WORD PREVIOUS KIND
    local -a TOKENS

    LINE=${COMP_LINE:0:COMP_POINT}
    read -ra TOKENS <<< "$LINE"

    if [[ -z $LINE || $LINE == *[[:space:]] ]]; then
        WORD=
        PREVIOUS=${TOKENS[-1]-}
    else
        WORD=${TOKENS[-1]-}
        PREVIOUS=${TOKENS[-2]-}
    fi

    if [[ ${TOKENS[1]-} == --ctl ]]; then
        local COMMAND
        COMMAND=${TOKENS[2]-}

        if [[ -z $COMMAND || ( ${#TOKENS[@]} -eq 3 && -n $WORD ) ]]; then
            KIND=command
        elif [[ $COMMAND == analyse || $COMMAND == regenerate || $COMMAND == migrate ]]; then
            KIND=session
        else
            return
        fi

        mapfile -t COMPREPLY < <("${COMP_WORDS[0]}" --ctl --complete "$KIND" "$WORD")
        return
    fi

    case $PREVIOUS in
        -r | --resume) KIND=session ;;
        -f | --from) KIND=session ;;
        -L | --login) KIND=provider ;;
        -c | --caps) KIND=caps ;;
        -t | --tool) KIND=tool ;;
        -m | --model)
            if [[ $WORD == *+* ]]; then
                KIND=model
            elif [[ $WORD == *@* && $COMP_WORDBREAKS == *@* ]]; then
                KIND=effort
            else
                KIND=model
            fi
            ;;
        *)
            if [[ -n $WORD && $WORD != -* ]]; then
                return
            fi
            KIND=option
            ;;
    esac

    mapfile -t COMPREPLY < <("${COMP_WORDS[0]}" --complete "$KIND" "$WORD")

    if [[ $KIND == effort && ${#COMPREPLY[@]} -gt 0 ]]; then
        COMPREPLY=("${COMPREPLY[@]/#/@}")
    fi
}

complete -o default -F _oh oh
