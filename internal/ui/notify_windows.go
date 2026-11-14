//go:build windows

package ui

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"fyne.io/fyne/v2"
	"github.com/go-ole/go-ole"
)

// The Windows Runtime interfaces a toast goes through, and the place of
// each method used in its vtable, after IInspectable's six.
var (
	iidXMLDocument                     = ole.NewGUID("f7f3a506-1e87-42d6-bcfb-b8c809fa5494")
	iidXMLDocumentIO                   = ole.NewGUID("6cd0e74e-ee65-4489-9ebf-ca43e87ba637")
	iidToastNotificationFactory        = ole.NewGUID("04124b20-82c6-4229-b109-fd9ed4662b53")
	iidToastNotificationManagerStatics = ole.NewGUID("50ac103f-d235-4598-bbef-98fe4d1a3ad4")
)

const (
	slotLoadXML                   = 6 // IXmlDocumentIO.LoadXml
	slotCreateToastNotification   = 6 // IToastNotificationFactory.CreateToastNotification
	slotCreateToastNotifierWithID = 7 // IToastNotificationManagerStatics.CreateToastNotifierWithId
	slotShow                      = 6 // IToastNotifier.Show
)

// toastCalls feeds the one thread toasts are shown on, which the first
// toast starts. Each call is given the error of the thread's start.
var (
	toastOnce  sync.Once
	toastCalls chan func(error)
)

// systemToast shows notifications through the Windows Runtime, in this
// process. Fyne's SendNotification shows the same toast from a hidden
// PowerShell that evaluates a script, which security software takes for an
// attack. A toast Windows refuses is passed to failed, on the app's
// goroutine, and nothing else is tried.
func systemToast(fa fyne.App, failed func(error)) func(title, text string) {
	var iconOnce sync.Once
	icon := ""
	return func(title, text string) {
		toastOnce.Do(startToasts)
		show := func(err error) {
			if err == nil {
				iconOnce.Do(func() { icon = toastIcon(fa) })
				err = showToast(fa.UniqueID(), toastXML(title, text, icon))
			}
			if err != nil {
				fyne.Do(func() { failed(err) })
			}
		}
		select {
		case toastCalls <- show:
		default:
			// Eight toasts waiting: Windows is not showing them.
		}
	}
}

// startToasts starts the thread toasts are shown on, in the Windows
// Runtime's multithreaded apartment.
func startToasts() {
	toastCalls = make(chan func(error), 8)
	go func() {
		runtime.LockOSThread()
		// RO_INIT_MULTITHREADED; S_FALSE says the thread already was.
		err := ole.RoInitialize(1)
		var oe *ole.OleError
		if errors.As(err, &oe) && oe.Code() == 1 {
			err = nil
		}
		if err != nil {
			err = fmt.Errorf("the Windows Runtime: %w", err)
		}
		for f := range toastCalls {
			f(err)
		}
	}()
}

// toastXML is the toast Fyne's script builds, from the same template.
func toastXML(title, text, icon string) string {
	esc := func(s string) string {
		var b strings.Builder
		_ = xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	if icon == "" {
		return `<toast><visual><binding template="ToastText02">` +
			`<text id="1">` + esc(title) + `</text><text id="2">` + esc(text) + `</text>` +
			`</binding></visual></toast>`
	}
	return `<toast><visual><binding template="ToastImageAndText02">` +
		`<image id="1" src="` + esc("file:///"+icon) + `"/>` +
		`<text id="1">` + esc(title) + `</text><text id="2">` + esc(text) + `</text>` +
		`</binding></visual></toast>`
}

// toastIcon writes the app's icon where Fyne keeps it for its toasts, and
// says where; "" when there is none.
func toastIcon(fa fyne.App) string {
	res := fa.Icon()
	cache, err := os.UserCacheDir()
	if res == nil || err != nil {
		return ""
	}
	dir := filepath.Join(cache, "fyne", fa.UniqueID())
	path := filepath.Join(dir, "icon.png")
	if os.MkdirAll(dir, 0o700) != nil || os.WriteFile(path, res.Content(), 0o600) != nil {
		return ""
	}
	return path
}

// newToast makes a toast of its XML content. The thread must be in the
// Windows Runtime.
func newToast(content string) (*ole.IUnknown, error) {
	inst, err := ole.RoActivateInstance("Windows.Data.Xml.Dom.XmlDocument")
	if err != nil {
		return nil, fmt.Errorf("XmlDocument: %w", err)
	}
	defer inst.Release()
	io, err := queryInterface(&inst.IUnknown, iidXMLDocumentIO)
	if err != nil {
		return nil, fmt.Errorf("IXmlDocumentIO: %w", err)
	}
	defer io.Release()
	h, err := ole.NewHString(content)
	if err != nil {
		return nil, err
	}
	defer ole.DeleteHString(h)
	if err := comCall(io, slotLoadXML, uintptr(h)); err != nil {
		return nil, fmt.Errorf("IXmlDocumentIO.LoadXml: %w", err)
	}
	doc, err := queryInterface(&inst.IUnknown, iidXMLDocument)
	if err != nil {
		return nil, fmt.Errorf("IXmlDocument: %w", err)
	}
	defer doc.Release()

	factory, err := ole.RoGetActivationFactory("Windows.UI.Notifications.ToastNotification", iidToastNotificationFactory)
	if err != nil {
		return nil, fmt.Errorf("ToastNotification: %w", err)
	}
	defer factory.Release()
	var toast *ole.IUnknown
	if err := comCall(&factory.IUnknown, slotCreateToastNotification, uintptr(unsafe.Pointer(doc)), uintptr(unsafe.Pointer(&toast))); err != nil {
		return nil, fmt.Errorf("IToastNotificationFactory.CreateToastNotification: %w", err)
	}
	return toast, nil
}

// showToast makes a toast of its XML content and shows it under the app ID
// appID. The thread must be in the Windows Runtime.
func showToast(appID, content string) error {
	toast, err := newToast(content)
	if err != nil {
		return err
	}
	defer toast.Release()
	return pushToast(appID, toast)
}

// pushToast shows toast under the app ID appID, which Windows names it by.
// The thread must be in the Windows Runtime.
func pushToast(appID string, toast *ole.IUnknown) error {
	manager, err := ole.RoGetActivationFactory("Windows.UI.Notifications.ToastNotificationManager", iidToastNotificationManagerStatics)
	if err != nil {
		return fmt.Errorf("ToastNotificationManager: %w", err)
	}
	defer manager.Release()
	h, err := ole.NewHString(appID)
	if err != nil {
		return err
	}
	defer ole.DeleteHString(h)
	var notifier *ole.IUnknown
	if err := comCall(&manager.IUnknown, slotCreateToastNotifierWithID, uintptr(h), uintptr(unsafe.Pointer(&notifier))); err != nil {
		return fmt.Errorf("IToastNotificationManagerStatics.CreateToastNotifierWithId: %w", err)
	}
	defer notifier.Release()
	if err := comCall(notifier, slotShow, uintptr(unsafe.Pointer(toast))); err != nil {
		return fmt.Errorf("IToastNotifier.Show: %w", err)
	}
	return nil
}

func queryInterface(obj *ole.IUnknown, iid *ole.GUID) (*ole.IUnknown, error) {
	d, err := obj.QueryInterface(iid)
	if err != nil {
		return nil, err
	}
	return (*ole.IUnknown)(unsafe.Pointer(d)), nil
}

// comCall calls the method at slot of obj's vtable, obj first.
func comCall(obj *ole.IUnknown, slot int, args ...uintptr) error {
	vtbl := (*[slotCreateToastNotifierWithID + 1]uintptr)(unsafe.Pointer(obj.RawVTable))
	hr, _, _ := syscall.SyscallN(vtbl[slot], append([]uintptr{uintptr(unsafe.Pointer(obj))}, args...)...)
	if hr != 0 {
		return ole.NewError(hr)
	}
	return nil
}
