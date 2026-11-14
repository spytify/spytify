// Package toast shows Windows notifications through the Windows Runtime, in
// the calling process: Fyne's SendNotification shows them from a hidden
// PowerShell that evaluates a script, which security software takes for an
// attack (#831). It holds no Fyne code, so Windows CI runs its tests
// without OpenGL.
package toast
