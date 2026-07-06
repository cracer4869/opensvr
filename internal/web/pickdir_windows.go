//go:build windows

package web

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 本文件用 Win32 COM 的 IFileOpenDialog 弹出"现代资源管理器样式"的文件夹选择框，
// 取代旧的 PowerShell FolderBrowserDialog：
//   - 无 PowerShell 控制台闪窗；
//   - 现代 Explorer 外观（与"另存为"一致）；
//   - 路径以 UTF-16 取回后转 UTF-8，无编码问题；
//   - 以前台窗口(通常是浏览器)为 owner，对话框随浏览器置顶，切走再切回也能找回。

// COM GUID
var (
	clsidFileOpenDialog = windows.GUID{Data1: 0xDC1C5A9C, Data2: 0xE88A, Data3: 0x4DDE, Data4: [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	iidFileOpenDialog   = windows.GUID{Data1: 0xD57C7288, Data2: 0xD4AD, Data3: 0x4768, Data4: [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
)

const (
	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1
	rpcEChangedMode         = 0x80010106 // 线程已用别的并发模型初始化过 COM

	fosPickFolders     = 0x00000020
	fosForceFilesystem = 0x00000040
	fosPathMustExist   = 0x00000800

	sigdnFilesysPath = 0x80058000
	errorCancelled   = 0x800704C7 // HRESULT_FROM_WIN32(ERROR_CANCELLED)，即用户点了取消

	// IFileOpenDialog 虚表方法序号（继承 IModalWindow←IFileDialog）。
	mIUnknownRelease = 2
	mShow            = 3  // IModalWindow::Show(hwndOwner)
	mSetOptions      = 9  // IFileDialog::SetOptions
	mGetOptions      = 10 // IFileDialog::GetOptions
	mSetTitle        = 17 // IFileDialog::SetTitle
	mGetResult       = 20 // IFileDialog::GetResult
	// IShellItem 虚表方法序号。
	mGetDisplayName = 5 // IShellItem::GetDisplayName
)

var (
	ole32                   = windows.NewLazySystemDLL("ole32.dll")
	procCoInitializeEx      = ole32.NewProc("CoInitializeEx")
	procCoUninitialize      = ole32.NewProc("CoUninitialize")
	procCoCreateInstance    = ole32.NewProc("CoCreateInstance")
	procCoTaskMemFree       = ole32.NewProc("CoTaskMemFree")
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
)

// comCall 调用 COM 对象 this 虚表第 method 个方法（this 作为隐式首参传入）。
func comCall(this uintptr, method int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(unsafe.Pointer(this))
	fn := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(method)*unsafe.Sizeof(uintptr(0))))
	ret, _, _ := syscall.SyscallN(fn, append([]uintptr{this}, args...)...)
	return ret
}

// failed 判定 HRESULT 是否失败（signed 负值即 FAILED）。
func failed(hr uintptr) bool { return int32(hr) < 0 }

// pickFolder 弹出原生文件夹选择框，返回所选绝对路径（UTF-8）；用户取消时返回空字符串。
func pickFolder() (string, error) {
	// COM 单线程套间(STA)要求在固定 OS 线程上初始化/使用。
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded)
	switch {
	case int32(hr) == 0 || int32(hr) == 1: // S_OK / S_FALSE：本次拥有初始化，需配对 CoUninitialize
		defer procCoUninitialize.Call()
	case uint32(hr) == rpcEChangedMode: // 线程已初始化为其它模型：不拥有、也不反初始化
	default:
		return "", fmt.Errorf("CoInitializeEx 失败: 0x%08x", uint32(hr))
	}

	var dialog uintptr
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidFileOpenDialog)),
		0,
		clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidFileOpenDialog)),
		uintptr(unsafe.Pointer(&dialog)),
	)
	if failed(hr) || dialog == 0 {
		return "", fmt.Errorf("创建文件对话框失败: 0x%08x", uint32(hr))
	}
	defer comCall(dialog, mIUnknownRelease)

	// 追加"选文件夹 + 必须是真实文件系统路径 + 路径必须存在"选项。
	var opts uint32
	comCall(dialog, mGetOptions, uintptr(unsafe.Pointer(&opts)))
	opts |= fosPickFolders | fosForceFilesystem | fosPathMustExist
	comCall(dialog, mSetOptions, uintptr(opts))

	if title, err := windows.UTF16PtrFromString("选择开局文件根目录"); err == nil {
		comCall(dialog, mSetTitle, uintptr(unsafe.Pointer(title)))
	}

	// 以前台窗口(通常是浏览器)为 owner：对话框归属它、随它一起置顶与切换，避免"切走找不到"。
	owner, _, _ := procGetForegroundWindow.Call()

	hr = comCall(dialog, mShow, owner)
	if uint32(hr) == errorCancelled {
		return "", nil // 用户取消
	}
	if failed(hr) {
		return "", fmt.Errorf("打开对话框失败: 0x%08x", uint32(hr))
	}

	var item uintptr
	hr = comCall(dialog, mGetResult, uintptr(unsafe.Pointer(&item)))
	if failed(hr) || item == 0 {
		return "", fmt.Errorf("读取选择结果失败: 0x%08x", uint32(hr))
	}
	defer comCall(item, mIUnknownRelease)

	var psz uintptr
	hr = comCall(item, mGetDisplayName, sigdnFilesysPath, uintptr(unsafe.Pointer(&psz)))
	if failed(hr) || psz == 0 {
		return "", fmt.Errorf("读取路径失败: 0x%08x", uint32(hr))
	}
	defer procCoTaskMemFree.Call(psz)

	return windows.UTF16PtrToString((*uint16)(unsafe.Pointer(psz))), nil
}
