function _oh {
    local LINE WORD PREVIOUS KIND TOKEN
    local FLAG_GROUP='^-[fpluUIJvh]*[rmL]?[fpluUIJvh]*$'
    local -a TOKENS LINE_TOKENS

    LINE=${COMP_LINE:0:COMP_POINT}
    read -ra TOKENS <<< "$LINE"
    read -ra LINE_TOKENS <<< "$COMP_LINE"

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
        elif [[ $WORD == -* ]]; then
            KIND=option
        elif [[ $COMMAND == analyse || $COMMAND == regenerate || $COMMAND == migrate ]]; then
            KIND=session
        elif [[ $COMMAND == sessions && ( $PREVIOUS == -d || $PREVIOUS == --delete ) ]]; then
            KIND=deletable
        else
            return
        fi

        mapfile -t COMPREPLY < <("${COMP_WORDS[0]}" --ctl --complete "$KIND" "$WORD" "$COMMAND")
        return
    fi

    if [[ $PREVIOUS =~ $FLAG_GROUP && $PREVIOUS == *r* ]]; then
        PREVIOUS=-r
    fi

    case $PREVIOUS in
        -r | --resume)
            KIND=stored-session
            for TOKEN in "${LINE_TOKENS[@]}"; do
                if [[ $TOKEN == --force || ( $TOKEN =~ $FLAG_GROUP && $TOKEN == *f* ) ]]; then
                    KIND=any-session
                fi
            done
            ;;
        --from) KIND=stored-session ;;
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
