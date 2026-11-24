# This file is in the public domain.
#
# Adapted from pacman's contrib/bash_completion for pacdpkg.

_arch_compgen() {
  local i r
  COMPREPLY=($(compgen -W '$*' -- "$cur"))
  for ((i=1; i < ${#COMP_WORDS[@]}-1; i++)); do
    for r in ${!COMPREPLY[@]}; do
      if [[ ${COMP_WORDS[i]} = ${COMPREPLY[r]} ]]; then
        unset 'COMPREPLY[r]'; break
      fi
    done
  done
}

_arch_ptr2comp() {
  local list= x y
  for x; do
    for y in '0 --' '1 -'; do
      eval 'set -- ${'$x'[${y% *}]}'
      list+=\ ${@/#/${y#* }}
    done
  done
  _arch_compgen $list
}

_arch_incomp() {
  local r="[[:space:]]-(-${1#* }[[:space:]]|[[:alnum:]_]*${1% *})"; [[ $COMP_LINE =~ $r ]]
}

_pacdpkg_keyids() {
  \pacman-key --list-keys 2>/dev/null | awk '
    $1 == "pub" {
      # key id
      split($2, a, "/"); print a[2]
    }
    $1 == "uid" {
      # email
      if (match($NF, /<[^>]+>/))
        print substr($NF, RSTART + 1, RLENGTH - 2)
    }'
}

_pacdpkg_key() {
  local o opts wantfiles
  local cur prev words cword
  _init_completion || return
  opts=('add config delete edit-key export finger gpgdir
         help import import-trustdb init keyserver list-keys list-sigs
         lsign-key nocolor populate recv-keys refresh-keys updatedb
         verify version'
        'a d e f h l r u v V')

  # operations for which we want to complete keyids
  for o in 'd delete' 'e export' 'f finger' 'l list-keys' 'r recv-keys' \
      'edit-key' 'list-sigs' 'lsign-key' 'refresh-keys'; do
    _arch_incomp "$o" && break
    unset o
  done

  # options for which we want file completion
  wantfiles='-@(c|-config|g|-gpgdir)'

  if [[ $prev = 'pacman-key' || ( $cur = -* && $prev != $wantfiles ) ]]; then
    _arch_ptr2comp opts
  elif [[ $prev = @(-k|--keyserver) ]]; then
    return
  elif [[ $prev != $wantfiles && $o ]]; then
    COMPREPLY=($(compgen -W '$(_pacdpkg_keyids)' -- "$cur"))
  fi
  true
}

_makepkg() {
  compopt +o default
  local opts
  local cur prev words cword
  _init_completion || return
  if [[ $prev = @(-p|--config) ]]; then
    compopt -o default
  elif [[ ! $prev =~ ^-(-(config|help|key|version)$|[[:alnum:]_]*[Vh]) ]]; then
    opts=('allsource asdeps check clean cleanbuild config force geninteg help
           holdver ignorearch install key log needed noarchive nobuild nocheck
           nocolor noconfirm nodeps noextract noprepare noprogressbar nosign
           packagelist printsrcinfo repackage rmdeps sign skipchecksums
           skipinteg skippgpcheck source syncdeps verifysource version'
          'A C L R S c d e f g h i m o p r s')
    _arch_ptr2comp opts
  fi
  true
}

_pacdpkg_cache_file() {
  printf '%s\n' "${XDG_CACHE_HOME:-$HOME/.cache}/pacdpkg/$1"
}

_pacdpkg_cache_fresh() {
  local cache=$1
  [[ -s $cache ]] && [[ -z $(find /var/lib/apt/lists -maxdepth 1 \
    -name '*_Packages*' -newer "$cache" -print -quit 2>/dev/null) ]]
}

_pacdpkg_available_names() {
  local cache tmp
  cache=$(_pacdpkg_cache_file avail-names)
  if _pacdpkg_cache_fresh "$cache"; then
    cat "$cache"
    return
  fi
  mkdir -p "${cache%/*}" 2>/dev/null
  tmp=$(mktemp "${cache}.XXXXXX" 2>/dev/null) || {
    \pacdpkg -Slq 2>/dev/null
    return
  }
  \pacdpkg -Slq 2>/dev/null | sort -u > "$tmp"
  if [[ -s $tmp ]]; then
    mv -f "$tmp" "$cache"
  else
    rm -f "$tmp"
  fi
  [[ -s $cache ]] && cat "$cache"
}

_pacdpkg_group_names() {
  local cache tmp
  cache=$(_pacdpkg_cache_file groups)
  if _pacdpkg_cache_fresh "$cache"; then
    cat "$cache"
    return
  fi
  mkdir -p "${cache%/*}" 2>/dev/null
  tmp=$(mktemp "${cache}.XXXXXX" 2>/dev/null) || {
    \pacdpkg -Sg 2>/dev/null | cut -d' ' -f1 | sort -u
    return
  }
  \pacdpkg -Sg 2>/dev/null | cut -d' ' -f1 | sort -u > "$tmp"
  if [[ -s $tmp ]]; then
    mv -f "$tmp" "$cache"
  else
    rm -f "$tmp"
  fi
  [[ -s $cache ]] && cat "$cache"
}

_pacdpkg_repo_names() {
  local cache tmp
  cache=$(_pacdpkg_cache_file repos)
  if _pacdpkg_cache_fresh "$cache"; then
    cat "$cache"
    return
  fi
  mkdir -p "${cache%/*}" 2>/dev/null
  tmp=$(mktemp "${cache}.XXXXXX" 2>/dev/null) || {
    \pacdpkg -Sl 2>/dev/null | cut -d' ' -f1 | sort -u
    return
  }
  \pacdpkg -Sl 2>/dev/null | cut -d' ' -f1 | sort -u > "$tmp"
  if [[ -s $tmp ]]; then
    mv -f "$tmp" "$cache"
  else
    rm -f "$tmp"
  fi
  [[ -s $cache ]] && cat "$cache"
}

_pacdpkg_pkg() {
  if [[ $1 == Slq ]]; then
    _arch_compgen "$(_pacdpkg_available_names)"
    return
  fi
  if [[ $1 == Sg ]]; then
    _arch_compgen "$(_pacdpkg_group_names)"
    return
  fi
  _arch_compgen "$(
    if [[ $2 ]]; then
      \pacdpkg -$1 2>/dev/null | \cut -d' ' -f1 | \sort -u
    else
      \pacdpkg -$1 2>/dev/null
    fi
  )"
}

_pacdpkg_repo_list() {
  _arch_compgen "$(_pacdpkg_repo_names)"
}

_pacdpkg() {
  compopt -o default
  local common core database files query remove sync upgrade o
  local cur prev words cword
  _init_completion || return
  database=('asdeps asexplicit')
  files=('list machinereadable refresh regex' 'l x y')
  query=('changelog check deps explicit file foreign groups info list native owns
          search unrequired upgrades' 'c e g i k l m n o p s t u')
  remove=('cascade dbonly nodeps assume-installed nosave print recursive unneeded' 'c n p s u')
  sync=('asdeps asexplicit clean dbonly downloadonly groups ignore ignoregroup
         info list needed nodeps assume-installed print refresh recursive search sysupgrade'
        'c g i l p s u w y')
  upgrade=('asdeps asexplicit needed nodeps assume-installed print recursive' 'p')
  common=('arch cachedir color config confirm dbpath debug gpgdir help hookdir logfile
           noconfirm noprogressbar noscriptlet quiet root verbose' 'b d h q r v')
  core=('database files help query remove sync upgrade version' 'D F Q R S U V h')

  for o in 'D database' 'F files' 'Q query' 'R remove' 'S sync' 'U upgrade'; do
    _arch_incomp "$o" && break
  done

  if [[ $? != 0 ]]; then
    _arch_ptr2comp core
  elif [[ ! $prev =~ ^-[[:alnum:]_]*[Vbhr] &&
    ! $prev = --@(cachedir|color|config|dbpath|help|hookdir|gpgdir|logfile|root|version) ]]
  then
    [[ $cur = -* ]] && _arch_ptr2comp ${o#* } common ||
      case ${o% *} in
      D|R)
          _pacdpkg_pkg Qq;;
      F)
        { _arch_incomp 'l list'   && _pacdpkg_pkg Slq ; }       ||
          compopt +o default;;
      Q)
        { _arch_incomp 'g groups' && _pacdpkg_pkg Qg sort; }    ||
        { _arch_incomp 'p file'   && _pacdpkg_file; }           ||
        { _arch_incomp 's search' && compopt +o default; }     ||
        { _arch_incomp 'u upgrades' && compopt +o default; }   ||
          _arch_incomp 'o owns'   ||
          _pacdpkg_pkg Qq;;
      S)
        { _arch_incomp 'g groups' && _pacdpkg_pkg Sg; }      ||
        { _arch_incomp 'l list'   && _pacdpkg_repo_list; } ||
        { _arch_incomp 's search' && compopt +o default; }  ||
          _pacdpkg_pkg Slq;;
      U)
          _pacdpkg_file;;
      esac
  fi
  true
}

_pacdpkg_file() {
  compopt -o filenames; _filedir 'pkg.tar*'
}

complete -F _pacdpkg pacdpkg
complete -F _makepkg makepkg

# ex:et ts=2 sw=2 ft=sh
