package glfw

import (
	"syscall"
	"unsafe"
)

// hideGLFWHelperWindow hides the internal GLFW helper window that GLFW creates
// on Windows for HID device notifications. GLFW calls ShowWindow(SW_HIDE) once,
// but that call is consumed as a no-op when the parent process passes STARTUPINFO
// (e.g. launched from Explorer). This second call actually hides the window.
func hideGLFWHelperWindow() {
	findWindowW := user32.NewProc("FindWindowW")
	showWindowFn := user32.NewProc("ShowWindow")

	hwnd, _, _ := syscall.SyscallN(findWindowW.Addr(),
		uintptr(unsafe.Pointer(toNativePtr("GLFW30"))),
		uintptr(unsafe.Pointer(toNativePtr("GLFW message window"))),
	)
	if hwnd != 0 {
		const swHide = 0
		syscall.SyscallN(showWindowFn.Addr(), hwnd, swHide)
	}
}
