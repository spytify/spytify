package hidden

import "golang.org/x/sys/windows"

// hide sets the folder's Hidden attribute, which Explorer goes by.
func hide(path string) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attrs, err := windows.GetFileAttributes(p)
	if err != nil {
		return err
	}
	if attrs&windows.FILE_ATTRIBUTE_HIDDEN != 0 {
		return nil
	}
	return windows.SetFileAttributes(p, attrs|windows.FILE_ATTRIBUTE_HIDDEN)
}
