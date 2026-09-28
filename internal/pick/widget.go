package pick

import _ "embed"

// Widget is the zsh function that binds Ctrl-G to snp pick and appends
// the printed command to the line editor. `snp widget` prints it.
//
//go:embed widget.zsh
var Widget string
