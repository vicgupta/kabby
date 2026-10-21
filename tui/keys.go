package tui

// Key represents a terminal key event.
type Key struct {
	Rune rune   // for printable characters
	Code string // for special keys: "up", "down", "left", "right", "home", "end",
	// "backspace", "delete", "enter", "escape", "tab", "backtab",
	// "ctrl-c", "ctrl-d", "ctrl-u", "ctrl-l", "ctrl-c"
	Ctrl  bool // ctrl modifier
	Shift bool // shift modifier
	Alt   bool // alt modifier
}

// IsCtrlC returns true if the key is Ctrl+C.
func (k Key) IsCtrlC() bool {
	return k.Ctrl && k.Rune == 'c'
}

// IsCtrlD returns true if the key is Ctrl+D.
func (k Key) IsCtrlD() bool {
	return k.Ctrl && k.Rune == 'd'
}

// ParseKey reads a key event from a byte slice (from a terminal in raw mode).
// Returns the key and the remaining bytes.
func ParseKey(data []byte) (Key, []byte, bool) {
	if len(data) == 0 {
		return Key{}, data, false
	}

	// Escape sequences start with 0x1b.
	if data[0] == 0x1b {
		if len(data) == 1 {
			return Key{Code: "escape"}, data[1:], true
		}
		if data[1] == '[' {
			// CSI sequence.
			if len(data) >= 3 {
				switch data[2] {
				case 'A':
					return Key{Code: "up"}, data[3:], true
				case 'B':
					return Key{Code: "down"}, data[3:], true
				case 'C':
					return Key{Code: "right"}, data[3:], true
				case 'D':
					return Key{Code: "left"}, data[3:], true
				case 'H':
					return Key{Code: "home"}, data[3:], true
				case 'F':
					return Key{Code: "end"}, data[3:], true
				case 'Z':
					return Key{Code: "backtab", Shift: true}, data[3:], true
				case '3':
					if len(data) >= 4 && data[3] == '~' {
						return Key{Code: "delete"}, data[4:], true
					}
				}
			}
		}
		if data[1] >= 'a' && data[1] <= 'z' {
			// Alt+key
			return Key{Alt: true, Rune: rune(data[1])}, data[2:], true
		}
		return Key{Code: "escape"}, data[1:], true
	}

	// DEL (0x7f) is the backspace key on most terminals. It is not < 0x20,
	// so it must be checked before the control-character branch.
	if data[0] == 0x7f {
		return Key{Code: "backspace"}, data[1:], true
	}

	// Control characters.
	if data[0] < 0x20 {
		switch data[0] {
		case 0x03:
			return Key{Ctrl: true, Rune: 'c'}, data[1:], true // Ctrl+C
		case 0x04:
			return Key{Ctrl: true, Rune: 'd'}, data[1:], true // Ctrl+D
		case 0x0a, 0x0d:
			return Key{Code: "enter"}, data[1:], true // Enter
		case 0x08, 0x7f:
			return Key{Code: "backspace"}, data[1:], true // Backspace/Delete
		case 0x09:
			return Key{Code: "tab"}, data[1:], true // Tab
		case 0x15:
			return Key{Ctrl: true, Rune: 'u'}, data[1:], true // Ctrl+U
		case 0x0c:
			return Key{Ctrl: true, Rune: 'l'}, data[1:], true // Ctrl+L
		default:
			if data[0] >= 0x01 && data[0] <= 0x1a {
				return Key{Ctrl: true, Rune: rune(data[0] + 0x60)}, data[1:], true
			}
		}
	}

	// Printable character.
	return Key{Rune: rune(data[0])}, data[1:], true
}
