//go:build windows

package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// volumeOf is the kind of drive dir is on and its file system.
func volumeOf(dir string) (drive, fs string) {
	vol := filepath.VolumeName(dir)
	if vol == "" {
		return "", ""
	}
	root := vol + `\`
	drive = driveKind(root)
	// A network drive out of reach can hang; the others answer at once.
	if drive == "fixed" || drive == "removable" {
		fs = fileSystem(root)
	}
	return drive, fs
}

// systemFacts adds Controlled folder access, which keeps unknown apps out
// of Music and Documents.
func systemFacts(facts map[string]string) {
	facts["output.cfa"] = cfaState()
}

func driveKind(root string) string {
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return "unknown"
	}
	switch windows.GetDriveType(p) {
	case windows.DRIVE_FIXED:
		return "fixed"
	case windows.DRIVE_REMOVABLE:
		return "removable"
	case windows.DRIVE_REMOTE:
		return "network"
	case windows.DRIVE_CDROM:
		return "cdrom"
	case windows.DRIVE_RAMDISK:
		return "ramdisk"
	case windows.DRIVE_NO_ROOT_DIR:
		return "missing"
	}
	return "unknown"
}

// fileSystem is the drive's file system, such as NTFS or exFAT.
func fileSystem(root string) string {
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return ""
	}
	var name [windows.MAX_PATH + 1]uint16
	if windows.GetVolumeInformation(p, nil, 0, nil, nil, nil, &name[0], uint32(len(name))) != nil {
		return ""
	}
	return windows.UTF16ToString(name[:])
}

var procCfGetSyncRootInfoByPath = windows.NewLazySystemDLL("cldapi.dll").NewProc("CfGetSyncRootInfoByPath")

// cfProviderInfo is CF_SYNC_ROOT_PROVIDER_INFO.
type cfProviderInfo struct {
	Status  uint32
	Name    [256]uint16
	Version [256]uint16
}

const cfSyncRootInfoProvider = 2

// cloudOf names the cloud provider that syncs dir, such as OneDrive, or
// says "none".
func cloudOf(dir string) string {
	if onOneDrive(dir) {
		return "OneDrive"
	}
	if c := homeCloud(dir); c != "" {
		return c
	}
	if procCfGetSyncRootInfoByPath.Find() != nil {
		return "none"
	}
	p, err := windows.UTF16PtrFromString(existingFolder(dir))
	if err != nil {
		return "unknown"
	}
	var info cfProviderInfo
	var n uint32
	hr, _, _ := procCfGetSyncRootInfoByPath.Call(uintptr(unsafe.Pointer(p)), cfSyncRootInfoProvider,
		uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info), uintptr(unsafe.Pointer(&n)))
	switch uint32(hr) {
	case 0:
		return cloudName(windows.UTF16ToString(info.Name[:]))
	case 0x80070000 | uint32(windows.ERROR_CLOUD_FILE_NOT_UNDER_SYNC_ROOT):
		return "none"
	}
	// The error number says whether this call is wrong.
	return fmt.Sprintf("unknown 0x%08x", uint32(hr))
}

// onOneDrive says whether dir is in a OneDrive folder that OneDrive's
// own variables name.
func onOneDrive(dir string) bool {
	for _, v := range []string{"OneDrive", "OneDriveConsumer", "OneDriveCommercial"} {
		if root := os.Getenv(v); root != "" && inFolder(dir, root) {
			return true
		}
	}
	return false
}

// knownFolders are the user's folders Windows names, the home folder
// last.
func knownFolders() []namedFolder {
	var out []namedFolder
	for _, f := range []struct {
		name string
		id   *windows.KNOWNFOLDERID
	}{
		{"Music", windows.FOLDERID_Music}, {"Documents", windows.FOLDERID_Documents}, {"Pictures", windows.FOLDERID_Pictures},
		{"Videos", windows.FOLDERID_Videos}, {"Desktop", windows.FOLDERID_Desktop}, {"Downloads", windows.FOLDERID_Downloads},
		{"Favorites", windows.FOLDERID_Favorites},
	} {
		// Moved or deleted, a folder still has its path.
		if p, err := windows.KnownFolderPath(f.id, windows.KF_FLAG_DONT_VERIFY); err == nil {
			out = append(out, namedFolder{f.name, p})
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, namedFolder{"home", home})
	}
	return out
}

// cfaState is Controlled folder access as Windows Security sets it, or as
// a policy forces it.
func cfaState() string {
	for _, path := range []string{
		`SOFTWARE\Policies\Microsoft\Windows Defender\Windows Defender Exploit Guard\Controlled Folder Access`,
		`SOFTWARE\Microsoft\Windows Defender\Windows Defender Exploit Guard\Controlled Folder Access`,
	} {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
		if errors.Is(err, registry.ErrNotExist) {
			continue
		}
		if err != nil {
			return "unreadable"
		}
		v, _, err := k.GetIntegerValue("EnableControlledFolderAccess")
		k.Close()
		if errors.Is(err, registry.ErrNotExist) {
			continue
		}
		if err != nil {
			return "unreadable"
		}
		return cfaMode(v)
	}
	return "off"
}
