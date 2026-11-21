//go:build windows

package link

import (
	"fmt"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// recorders is the job that holds the recorders this app starts. Only the
// app holds its handle, so Windows closes it when the app ends, however it
// ends (End task, a crash), and then ends every recorder in it. What a
// recorder starts (the capture helper, ffmpeg, the player through the
// shell) does not join it: the player outlives Spytify, and the capture
// helper and ffmpeg stop on their own once the recorder's pipes close.
var recorders struct {
	sync.Mutex
	h windows.Handle
}

// contain puts the recorder p in the app's job, made at the first call.
func contain(p *os.Process) error {
	recorders.Lock()
	defer recorders.Unlock()
	if recorders.h == 0 {
		h, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			return fmt.Errorf("create the job: %w", err)
		}
		if err := setJobLimits(h, windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE|windows.JOB_OBJECT_LIMIT_SILENT_BREAKAWAY_OK); err != nil {
			windows.CloseHandle(h)
			return err
		}
		recorders.h = h
	}
	ph, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(p.Pid))
	if err != nil {
		return fmt.Errorf("open the recorder: %w", err)
	}
	defer windows.CloseHandle(ph)
	if err := windows.AssignProcessToJobObject(recorders.h, ph); err != nil {
		return fmt.Errorf("put the recorder in the job: %w", err)
	}
	return nil
}

// release lets the recorders outlive the app: a Quit asked them to shut
// down, and they finish saving the takes still being encoded first.
func release() error {
	recorders.Lock()
	defer recorders.Unlock()
	if recorders.h == 0 {
		return nil
	}
	return setJobLimits(recorders.h, windows.JOB_OBJECT_LIMIT_SILENT_BREAKAWAY_OK)
}

func setJobLimits(h windows.Handle, flags uint32) error {
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = flags
	if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return fmt.Errorf("set the job's limits: %w", err)
	}
	return nil
}
