// Package trash moves files to the system's trash, where they can be put
// back: the Trash on macOS, the Recycle Bin on Windows, and on Linux the
// trash of the freedesktop.org specification, which file managers share.
// It never deletes a file: one that cannot go to a trash stays where it
// is, and Move says why.
package trash
