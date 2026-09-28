# Leave a picked snippet on the command line. Nothing runs until you
# press Enter. Cancel prints nothing.
#
#   source <(snp widget)
#
# Put that in ~/.zshrc. Two entries:
#   Ctrl-G inserts into the line you are already editing.
#   `snp pick` (typed as a command) pushes the command onto the next
#   prompt. The binary alone can only print it, and Starship then marks
#   that print with % and moves to a fresh prompt.

snp-pick() {
  local out
  out=$(command snp pick "$@") || return
  [[ -z $out ]] && return
  LBUFFER+="$out"
}
zle -N snp-pick
bindkey '^G' snp-pick

snp() {
  if [[ $1 == pick ]]; then
    shift
    local out
    out=$(command snp pick "$@") || return
    [[ -z $out ]] && return
    print -zr -- "$out"
    return
  fi
  command snp "$@"
}
