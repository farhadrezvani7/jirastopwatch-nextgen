package lockwatch

import "syscall"

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	procOpenInputDesk = user32.NewProc("OpenInputDesktop")
	procSwitchDesktop = user32.NewProc("SwitchDesktop")
	procCloseDesktop  = user32.NewProc("CloseDesktop")
)

const desktopSwitchDesktop = 0x0100

// While the workstation is locked the input desktop is the secure "Winlogon"
// desktop, which a normal process can't open or switch to.
func probe() (locked, ok bool) {
	h, _, _ := procOpenInputDesk.Call(0, 0, desktopSwitchDesktop)
	if h == 0 {
		return true, true
	}
	defer procCloseDesktop.Call(h)
	r, _, _ := procSwitchDesktop.Call(h)
	return r == 0, true
}
