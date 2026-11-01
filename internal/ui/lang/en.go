// Package lang is the lexicon: every text the window shows, in English, each
// written once under the name the code uses. A text with %s or %d takes its
// values through fmt.Sprintf, in the order its comment gives.
//
// Not here: the settings' labels and notes, which settings.Table keeps by
// their key; the checks' names and details, and the errors, which come from
// the recorder or are written where they are made; and log lines, telemetry
// names and preference keys, which the window never shows.
package lang

// The app and its tabs.
const (
	Spytify  = "Spytify"
	Record   = "Record"
	Library  = "Library"
	Settings = "Settings"
	Logs     = "Logs"
	About    = "About"
)

// The app bar. RecordCount is the Record tab with the tracks saved this
// session.
const (
	RecordCount      = "Record · %d"
	Stop             = "Stop"
	StopNow          = "Stop now"
	Donate           = "Donate"
	UpdateTo         = "Update to %s"
	NotConnected     = "Not connected"
	Connecting       = "Connecting…"
	PlayerRunning    = "%s is running"
	PlayerNotRunning = "%s is not running"
	StartingRecorder = "Starting the recorder…"
)

// The window while the recorder is not running.
const (
	RecorderNotRunning = "The recorder is not running"
	RecorderExplained  = "Spytify records through spytifyd, which runs on its own so recording goes on with this window closed. It starts with the app; choose the player it records if none is set."
	StartRecorder      = "Start the recorder"
	Player             = "Player"
)

// The Record screen. ReadyText takes the player, then the output folder.
const (
	ReadyToRecord             = "Ready to record"
	ReadyText                 = "Press Record, then play music in %s. Each track becomes its own file in %s."
	RecordIsOff               = "Record is off"
	RecordIsOffText           = "Spytify cannot write to the output folder, %s. The diagnostics say why."
	AlreadyPlaying            = "A track that is already playing when you press Record is skipped: recording starts with the next one, or replay it from the start."
	YourPlayer                = "your player"
	Open                      = "Open %s"
	OpenThePlayer             = "Open the player"
	OpenDiagnostics           = "Open diagnostics"
	Play                      = "Play"
	Skip                      = "Skip"
	Length                    = "Length"
	Level                     = "Level"
	RecordFromStart           = "Record this track from the start"
	KeepAnyway                = "Keep anyway"
	OpenFolder                = "Open folder"
	ClearList                 = "Clear list"
	NoFilesYet                = "No files yet"
	NoFilesYetText            = "Each track becomes a file here when it ends."
	SessionEnded              = "Session ended."
	SessionEndedBlocked       = "Session ended: Spytify cannot write to the output folder, so Record is off."
	FinishingLastFiles        = "Finishing the last files…"
	WaitingNextTrack          = "Waiting for the next track to start."
	NothingRecordedNotRunning = "Nothing is recorded while %s is not running."
	NothingRecordedPaused     = "Nothing is recorded while %s is paused: press play there."
	SessionTotals             = "%d saved · %d skipped · %d ads · %s"
)

// The track being recorded, over its title. StateNotRecorded takes why, in
// capitals.
const (
	StateRecording   = "● RECORDING"
	StatePaused      = "PAUSED"
	StateStopping    = "RECORDING · STOPS AFTER THIS TRACK"
	StateAdvert      = "ADVERT, NOT RECORDED"
	StateNotRecorded = "PLAYING, NOT RECORDED: %s"
)

// The captured stream after its rate, then the file's rate when lower:
// "44.1 kHz stereo", "96 kHz stereo, 48 kHz file".
const (
	Mono     = "%s mono"
	Stereo   = "%s stereo"
	Channels = "%s %d channels"
	FileRate = "%s, %s file"
)

// A track's chip in the session list, and why a track was skipped.
const (
	Recording        = "Recording"
	Saving           = "Saving"
	Saved            = "Saved"
	Skipped          = "Skipped"
	SkippedFor       = "Skipped: %s"
	SavedLowQuality  = "Saved: low quality"
	SavedPartial     = "Saved: partial"
	ReasonTooShort   = "too short"
	ReasonIncomplete = "incomplete"
	ReasonSeeked     = "track bar moved"
	ReasonLowQuality = "low quality"
)

// The Library: its sorts, the kinds of file its filter ticks, and what it
// says when it shows nothing.
const (
	SearchLibrary   = "Search artist, title or album"
	Newest          = "Newest"
	Name            = "Name"
	Album           = "Album"
	NoAlbum         = "No album"
	Complete        = "Complete"
	Partial         = "Partial"
	LowQuality      = "Low quality"
	NoFiles         = "No files"
	AllFiles        = "All files"
	NoRecordingsYet = "No recordings yet"
	LibraryEmpty    = "Recorded tracks land in %s."
	TickAKind       = "Tick a kind of file to show."
	NoTakesYet      = "None yet: Keep anyway saves a take cut short or in low quality."
	OnlyTakes       = "Every file here is a take cut short or in low quality."
	NothingMatches  = `Nothing matches "%s".`
	OneFile         = "1 file"
	Files           = "%d files"
	FilesIn         = "%d files in %s"
	SomeFilesIn     = "%d of %d files in %s"
)

// Settings, and the note under Minimize to tray when closing quits.
const (
	SearchSettings  = "Search settings"
	Advanced        = "Advanced"
	Reset           = "Reset"
	Example         = "e.g. %s"
	NotAPlaceholder = "Not a placeholder, left out when saved: %s"
	NotPlaceholders = "Not placeholders, left out when saved: %s"
	NoTrayIcons     = "This desktop shows no tray icons: closing quits. On GNOME, the AppIndicator and KStatusNotifierItem Support extension adds them."
	TrayIconLater   = "The tray icon comes when Spytify restarts: until then, closing quits."
)

// The player the recorder records, in Settings and while the recorder is
// not running.
const (
	PlayerProcess       = "Player process"
	PlayerProcessHint   = "process name, or a comma list"
	BundleID            = "Bundle id"
	BundleIDHint        = "bundle id"
	MPRISName           = "MPRIS name"
	MPRISNameHint       = "org.mpris.MediaPlayer2.<name> (default)"
	LaunchCommand       = "Launch command"
	LaunchCommandHint   = "command that starts the player (optional)"
	PlayerSaved         = "Saved. spytifyd records this player once it restarts."
	RestartRecorder     = "Restart spytifyd"
	RestartRecorderAsk  = "Restart spytifyd?"
	RestartStopsSession = "A session is recording; restarting stops it now."
)

// Logs and the diagnostics. NotRunning and PID are the player running
// check's detail, kept current between two runs of the checks.
const (
	Show                = "Show"
	Copy                = "Copy"
	OpenLogFile         = "Open log file"
	Diagnostics         = "Diagnostics"
	RunChecksAgain      = "Run the checks again"
	ChecksPass          = "All %d checks pass."
	ChecksNeedAttention = "%d of %d checks need attention."
	ChooseAnotherFolder = "Choose another folder"
	NotRunning          = "not running"
	PID                 = "pid %d"
	SendToDevelopers    = "Send to the developers"
	SendChecksText      = "Send the checks that fail and the end of the recorder's log to the developers? The log can name tracks you recorded."
	WhatWillBeSent      = "What will be sent"
	Comment             = "Comment (optional)"
	Send                = "Send"
	Sent                = "Sent"
	NotSent             = "Not sent, try again"
)

// About. Versions takes the app's version, the recorder's, then the
// protocol's; RecorderOn the recorder's version, then its platform.
const (
	Tagline              = "Records any application's audio, splits it into tracks and tags them."
	SourceCode           = "Source code on GitHub"
	ProjectPage          = "Project page"
	DonateHeart          = "♥ Donate"
	CheckForUpdates      = "Check for updates"
	Versions             = "App %s · recorder %s · protocol %d"
	RecorderOn           = "%s on %s"
	RecorderNotConnected = "not connected"
	UpdateAvailable      = "Update available"
	UpdateAvailableText  = "Spytify %s is out. Open the release page?"
	UpToDate             = "Up to date"
	UpToDateText         = "Spytify %s is the latest version."
	Disclaimer           = "For personal use of content you are entitled to listen to; you are responsible for the terms of the services you record."
)

// The first launch's dialogs.
const (
	BeforeYouRecord  = "Before you record"
	FirstRunText     = "Spytify records the audio the player sends to your speakers. Streaming services send compressed audio, so a FLAC or WAV file avoids a second lossy pass but adds no quality."
	DontShowAgain    = "Don't show this again"
	HelpFixIssues    = "Help fix issues"
	TrackerExplained = "Spytify can send its crashes, errors, traces and recording counts to its developers through Sentry, to help them fix issues."
	TrackerSwitch    = "Sentry bug tracker: send crashes, errors, traces and recording counts"
	TrackerOptedOut  = "You opted out of the Sentry bug tracker"
)

// A crash, and the GitHub issue that reports it. StoppedUnexpectedly takes
// Spytify or TheRecorder; CrashNotReported TrackerOff or NoTracker.
const (
	StoppedUnexpectedly = "%s stopped unexpectedly"
	TheRecorder         = "The recorder"
	CrashNotReported    = "No report was sent to the developers: %s. To report it yourself, open an issue on GitHub with the error below."
	TrackerOff          = "the Sentry bug tracker is off"
	NoTracker           = "this build has no bug tracker"
	ErrorAndTrace       = "Error and stack trace"
	ReportOnGitHub      = "Report on GitHub"
	IssueTitle          = "Crash: %s"
	IssueBody           = "What I was doing:\n\n\n```\n%s\n```\n"
	IssueCut            = "\n… (cut: Copy in Spytify gives the rest)"
)

// Dialogs' titles, texts and buttons.
const (
	OK       = "OK"
	Yes      = "Yes"
	No       = "No"
	Cancel   = "Cancel"
	Close    = "Close"
	Error    = "Error"
	Quit     = "Quit Spytify?"
	QuitText = "Recording stops, and the track being recorded is not kept."
)

// Notifications and the tray menu.
const (
	RecordingStopped    = "Recording stopped"
	SavedSkipped        = "%d saved · %d skipped"
	TrackSaved          = "Track saved"
	RecordingLowQuality = "Recording in low quality"
	NothingRecorded     = "Nothing is being recorded"
	StillRunning        = "Spytify is still running"
	StillRunningText    = "It records from the menu bar. Quit it from there, or turn off Minimize to tray in Settings."
	OpenWindow          = "Open window"
)

// The message when no graphics driver offers OpenGL, then what to do on
// Linux or Windows.
const (
	NoWindow      = "Spytify can't open its window. It draws it with OpenGL, and no graphics driver on this computer offers it."
	NoWindowLinux = "Install Mesa's OpenGL drivers, which draw with the graphics card or, without one, with the processor, then open Spytify again:\n" +
		"Debian, Ubuntu: sudo apt install libgl1-mesa-dri libglx-mesa0 libegl-mesa0\n" +
		"Fedora: sudo dnf install mesa-dri-drivers mesa-libGL mesa-libEGL"
	NoWindowWindows = "This happens when the graphics card is disabled or has no driver (Windows then uses its basic display driver), and in some virtual machines. " +
		"Turn the card back on in Device Manager, or install its driver, then open Spytify again."
)
