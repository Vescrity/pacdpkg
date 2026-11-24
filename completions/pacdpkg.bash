# bash completion for pacdpkg
#
# Install as /usr/share/bash-completion/completions/pacdpkg

_pacdpkg_installed()
{
    pacdpkg -Qq 2>/dev/null
}

_pacdpkg_repos()
{
    pacdpkg -Sl 2>/dev/null | awk '{print $1}' | sort -u
}

# Available package names are expensive to read from apt lists; cache them and
# rebuild when any Packages index is newer.
_pacdpkg_available()
{
    local cache dir tmp
    cache="${XDG_CACHE_HOME:-$HOME/.cache}/pacdpkg/avail-names"
    dir=/var/lib/apt/lists

    if [[ -s $cache ]] && [[ -z $(find "$dir" -maxdepth 1 -name '*_Packages*' -newer "$cache" -print -quit 2>/dev/null) ]]; then
        cat "$cache"
        return
    fi

    mkdir -p "${cache%/*}" 2>/dev/null
    tmp=$(mktemp "${cache}.XXXXXX" 2>/dev/null) || {
        pacdpkg -Slq 2>/dev/null
        return
    }
    pacdpkg -Slq 2>/dev/null | sort -u > "$tmp"
    if [[ -s $tmp ]]; then
        mv -f "$tmp" "$cache"
    else
        rm -f "$tmp"
    fi
    [[ -s $cache ]] && cat "$cache"
}

_pacdpkg_groups()
{
    pacdpkg -Qg 2>/dev/null | awk '{print $1}' | sort -u
}

_pacdpkg_has()
{
    local i w
    for ((i = 1; i < COMP_CWORD; i++)); do
        w=${COMP_WORDS[i]}
        [[ $w == "$1" ]] && return 0
        # short options are often clustered, e.g. -Sl or -Qo
        if [[ $1 == -[[:alpha:]] && $w == -[!-]* ]]; then
            case ${w#-} in
                *${1#-}*) return 0 ;;
            esac
        fi
    done
    return 1
}

_pacdpkg_opts_global="-b --dbpath -r --root --sysroot --arch --cachedir --color --config --confirm --debug --disable-download-timeout --disable-sandbox --disable-sandbox-filesystem --disable-sandbox-syscalls --gpgdir --hookdir --logfile --noconfirm -v --verbose -h --help -V --version"
_pacdpkg_opts_trans="--dbonly --noscriptlet --noprogressbar --print-format --assume-installed"
_pacdpkg_opts_q="-c --changelog -d --deps -e --explicit -g --groups -i --info -k --check -l --list -m --foreign -n --native -o --owns -p --file -q --quiet -s --search -t --unrequired -u --upgrades"
_pacdpkg_opts_s="-c --clean -d --nodeps -g --groups -i --info -l --list -p --print -q --quiet -s --search -u --sysupgrade -w --downloadonly -y --refresh --asdeps --asexplicit --needed --ignore --ignoregroup --overwrite"
_pacdpkg_opts_r="-c --cascade -d --nodeps -n --nosave -p --print -s --recursive -u --unneeded"
_pacdpkg_opts_u="-d --nodeps -p --print -w --downloadonly --asdeps --asexplicit --needed --ignore --ignoregroup --overwrite"
_pacdpkg_opts_d="-k --check -q --quiet --asdeps --asexplicit"
_pacdpkg_opts_f="-l --list -q --quiet -x --regex -y --refresh --machinereadable"

_pacdpkg()
{
    local cur prev op i w cluster
    COMPREPLY=()
    cur=${COMP_WORDS[COMP_CWORD]}
    prev=${COMP_WORDS[COMP_CWORD-1]}
    op=

    for ((i = 1; i < COMP_CWORD; i++)); do
        w=${COMP_WORDS[i]}
        case $w in
            -b|-r|--dbpath|--root|--sysroot|--config|--logfile|--gpgdir|--hookdir|--cachedir|--arch|--color|--ignore|--ignoregroup|--overwrite|--print-format|--assume-installed|--ask)
                ((i++))
                continue
                ;;
            -D|--database) op=D ;;
            -F|--files) op=F ;;
            -Q|--query) op=Q ;;
            -R|--remove) op=R ;;
            -S|--sync) op=S ;;
            -T|--deptest) op=T ;;
            -U|--upgrade) op=U ;;
            -[DFQRSTU]*)
                cluster=${w#-}
                case $cluster in *D*) op=D ;; *F*) op=F ;; *Q*) op=Q ;; *R*) op=R ;; *S*) op=S ;; *T*) op=T ;; *U*) op=U ;; esac
                ;;
        esac
    done

    case $prev in
        --color)
            COMPREPLY=($(compgen -W "always never auto" -- "$cur"))
            return
            ;;
        -b|--dbpath|-r|--root|--sysroot|--cachedir|--hookdir)
            COMPREPLY=($(compgen -d -- "$cur"))
            return
            ;;
        --config|--logfile|--gpgdir)
            COMPREPLY=($(compgen -f -- "$cur"))
            return
            ;;
        --arch)
            COMPREPLY=($(compgen -W "auto amd64 arm64 i386 x86_64 aarch64" -- "$cur"))
            return
            ;;
        --ignore|--ignoregroup|--overwrite|--print-format|--assume-installed|--ask)
            return
            ;;
    esac

    if [[ $cur == -* ]]; then
        local opts="$_pacdpkg_opts_global"
        case $op in
            Q) opts="$opts $_pacdpkg_opts_q" ;;
            S) opts="$opts $_pacdpkg_opts_s $_pacdpkg_opts_trans" ;;
            R) opts="$opts $_pacdpkg_opts_r $_pacdpkg_opts_trans" ;;
            U) opts="$opts $_pacdpkg_opts_u $_pacdpkg_opts_trans" ;;
            D) opts="$opts $_pacdpkg_opts_d" ;;
            F) opts="$opts $_pacdpkg_opts_f $_pacdpkg_opts_trans" ;;
            T) opts="$opts" ;;
            *)
                opts="$opts -D --database -F --files -Q --query -R --remove -S --sync -T --deptest -U --upgrade"
                ;;
        esac
        COMPREPLY=($(compgen -W "$opts" -- "$cur"))
        return
    fi

    case $op in
        Q)
            if _pacdpkg_has -p || _pacdpkg_has --file || _pacdpkg_has -o || _pacdpkg_has --owns; then
                COMPREPLY=($(compgen -f -- "$cur"))
            elif _pacdpkg_has -g || _pacdpkg_has --groups; then
                COMPREPLY=($(compgen -W "$(_pacdpkg_groups)" -- "$cur"))
            else
                COMPREPLY=($(compgen -W "$(_pacdpkg_installed)" -- "$cur"))
            fi
            ;;
        S)
            if _pacdpkg_has -l || _pacdpkg_has --list || _pacdpkg_has -i || _pacdpkg_has --info; then
                COMPREPLY=($(compgen -W "$(_pacdpkg_repos)" -- "$cur"))
            else
                COMPREPLY=($(compgen -W "$(_pacdpkg_available)" -- "$cur"))
            fi
            ;;
        R|D|T)
            COMPREPLY=($(compgen -W "$(_pacdpkg_installed)" -- "$cur"))
            ;;
        U)
            COMPREPLY=($(compgen -f -- "$cur"))
            ;;
        F)
            if _pacdpkg_has -l || _pacdpkg_has --list; then
                COMPREPLY=($(compgen -W "$(_pacdpkg_available)" -- "$cur"))
            else
                COMPREPLY=($(compgen -f -- "$cur"))
            fi
            ;;
        *)
            COMPREPLY=($(compgen -W "-D --database -F --files -Q --query -R --remove -S --sync -T --deptest -U --upgrade -h --help -V --version" -- "$cur"))
            ;;
    esac
}

complete -F _pacdpkg pacdpkg
