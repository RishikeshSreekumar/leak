package tui

// Key strings matched in Update. Kept in one place so the footer help stays in
// sync with the handled keys.
const (
	keyQuit       = "q"
	keyCtrlC      = "ctrl+c"
	keyTab        = "tab"
	keyShiftTab   = "shift+tab"
	keyUp         = "up"
	keyDown       = "down"
	keyK          = "k"
	keyJ          = "j"
	keyFilter     = "/"
	keyEsc        = "esc"
	keyEnter      = "enter"
	keyDetail     = "enter"
	keyAdd        = "a"
	keyEdit       = "e"
	keyMark       = "m"
	keyCancel     = "c"
	keyReactivate = "r"
	keyDelete     = "d"
	keySort       = "s"
	keyHelp       = "?"
)

// actionHelp is the footer hint shown on tabs that carry a row selection.
const actionHelp = "enter details · [a]dd [e]dit [m]ark [c]ancel [r]eactivate [d]elete [s]ort"

// navHelp is the always-present footer hint.
const navHelp = "1-5/tab switch · j/k move · / filter · ? help · q quit"
