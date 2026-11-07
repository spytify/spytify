// Package lang is the lexicon: every text the window shows, in English, each
// written once under the name the code uses. A text with %s or %d takes its
// values through fmt.Sprintf, in the order its comment gives.
//
// The settings' labels, notes and choices are in settings.go, by their key.
//
// Not here: the facts the recorder sends, as a path, a version or an
// error, which the window shows as they come, inside these texts when they
// word them: the checks by their ID (model.CheckName, model.CheckDetail),
// the recorder's warnings by their code (model.WarningText); and log
// lines, telemetry names and preference keys, which the window never shows.
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
	RecorderExplained  = "Spytify records through its recorder, which runs on its own so recording goes on with this window closed. It starts with the app; choose the player it records if none is set."
	StartRecorder      = "Start the recorder"
	Player             = "Player"
)

// The connection to the recorder, and why it failed. NoRecorder takes the
// recorder's file name; RecorderStopped the last line it wrote.
const (
	NotConnectedToRecorder = "not connected to the recorder"
	InArchive              = "Spytify runs from inside the downloaded archive, without the recorder next to it. Extract the whole archive to a folder, then start Spytify from there."
	NoRecorder             = "%s, the recorder, is not next to Spytify. Extract the whole download again and start Spytify from there; if it is still missing, an antivirus may have removed it."
	RecorderClosed         = "the recorder closed the connection"
	RecorderOtherProtocol  = "the recorder speaks another protocol version; update Spytify"
	RecorderStopped        = "the recorder stopped: %s"
	RecorderNoAnswer       = "the recorder started but does not answer; see its log in Logs"
)

// The Record screen. ReadyText takes the player, then the output folder.
const (
	ReadyToRecord             = "Ready to record"
	ReadyText                 = "Press Record, then play music in %s. Each track becomes its own file in %s."
	RecordIsOff               = "Record is off"
	RecordIsOffText           = "Spytify cannot write to the output folder, %s. The diagnostics say why."
	RecordNeedsRecorder       = "The recorder is not running. Start it to record."
	AlreadyPlaying            = "A track that is already playing when you press Record is skipped: recording starts with the next one, or replay it from the start."
	YourPlayer                = "your player"
	OpenPlayer                = "Open %s"
	OpenThePlayer             = "Open the player"
	OpenDiagnostics           = "Open diagnostics"
	SetFullVolume             = "Set to 100%"
	Play                      = "Play"
	Skip                      = "Skip"
	MeterLeft                 = "L"
	MeterRight                = "R"
	RecordFromStart           = "Record this track from the start"
	KeepAnyway                = "Keep anyway"
	OpenFolder                = "Open folder"
	ClearList                 = "Clear list"
	NoFilesYet                = "No files yet"
	NoFilesYetText            = "Each track becomes a file here when it ends."
	SessionEnded              = "Session ended."
	SessionEndedPlaylist      = "Session ended. Playlist: %s"
	ShowPlaylist              = "Show playlist"
	SessionEndedBlocked       = "Session ended: Spytify cannot write to the output folder, so Record is off."
	FinishingLastFiles        = "Finishing the last files…"
	WaitingNextTrack          = "Waiting for the next track to start."
	NothingRecordedNotRunning = "Nothing is recorded while %s is not running."
	NothingRecordedPaused     = "Nothing is recorded while %s is paused: press play there."
	OnlySilenceHeard          = "Spytify hears only silence from %s."
	SessionTotals             = "%d saved · %d skipped · %d ads · %s"
)

// The badge over the track being recorded's title, when it does not say
// Recording. StateNotRecorded takes why.
const (
	StatePaused      = "Paused"
	StateStopping    = "Recording · stops after this track"
	StateAdvert      = "Advert, not recorded"
	StateNotRecorded = "Playing, not recorded: %s"
)

// The captured stream's badges after its rate, then the file's rate when
// lower: "44.1 kHz", "Stereo", "48 kHz file".
const (
	Mono     = "Mono"
	Stereo   = "Stereo"
	Channels = "%d channels"
	FileRate = "%s file"
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
	SavedPauseFade   = "Saved: pause fade"
	SavedLowVolume   = "Saved: low volume"
	SavedBothIssues  = "Saved: pause fade, low volume"
	ReasonDuplicate  = "already recorded"
	ReasonTooShort   = "too short"
	ReasonAd         = "advert"
	ReasonIncomplete = "incomplete"
	ReasonSeeked     = "track bar moved"
	ReasonLowQuality = "low quality"
	ReasonSilent     = "only silence"
)

// An ad break in the session list. AdBreak, its pill, takes the ads'
// length; AdNumber an ad's number in the break, and AdTitled then its
// title.
const (
	AdBreak  = "Advertisements %s"
	AdNumber = "Ad #%d"
	AdTitled = "Ad #%d · %s"
)

// The Library: its search, whose ✕ Settings' search shares, its sorts, the
// kinds of file its filter ticks, and what it says when it shows nothing.
const (
	SearchLibrary   = "Search artist, title or album"
	ClearSearch     = "Clear the search"
	Newest          = "Newest"
	Name            = "Name"
	Album           = "Album"
	NoAlbum         = "No album"
	Complete        = "Complete"
	Partial         = "Partial"
	LowQuality      = "Low quality"
	PauseFade       = "Pause fade"
	LowVolume       = "Low volume"
	WithIssues      = "With issues"
	NoFiles         = "No files"
	Rescan          = "Rescan"
	AllFiles        = "All files"
	NoRecordingsYet = "No recordings yet"
	LibraryEmpty    = "Recorded tracks land in %s."
	TickAKind       = "Tick a kind of file to show."
	NoTakesYet      = "None yet: Keep anyway saves takes cut short or in low quality; takes with a pause fade or a low volume are marked."
	OnlyTakes       = "Every file here is a take cut short, in low quality, or with a pause fade or a low volume."
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
	PlayerSaved         = "Saved. The recorder records this player once it restarts."
	RestartRecorder     = "Restart the recorder"
	RestartRecorderAsk  = "Restart the recorder?"
	RestartStopsSession = "A session is recording; restarting stops it now."
	PlayerRequired      = "the player process name is required"
)

// Logs and the diagnostics. LevelError, LevelInfo and LevelDebug are the
// log's filter; RecorderRunning is the window's own check, that the
// recorder answers. DetailNotRunning and DetailPID are the player running
// check's detail, kept current between two runs of the checks.
const (
	LevelError          = "error"
	LevelInfo           = "info"
	LevelDebug          = "debug"
	RecorderRunning     = "recorder running"
	ShowLevels          = "Show"
	Copy                = "Copy"
	OpenLogFile         = "Open log file"
	Diagnostics         = "Diagnostics"
	RunChecksAgain      = "Run the checks again"
	ChecksPass          = "All %d checks pass."
	ChecksNeedAttention = "%d of %d checks need attention."
	NothingElseChecked  = "The recorder is not running, so nothing else can be checked."
	ChooseAnotherFolder = "Choose another folder"
	DetailNotRunning    = "not running"
	DetailPID           = "pid %s"
	SendToDevelopers    = "Send to the developers"
	SendChecksText      = "Send the checks that fail and the end of the recorder's log to the developers? The log can name tracks you recorded."
	SendChecksPicture   = "Send the checks that fail, the end of the recorder's log and a picture of the window to the developers? They can name tracks you recorded; the picture covers your folders."
	SendPicture         = "Send the picture of the window"
	WhatWillBeSent      = "What will be sent"
	CommentHint         = "Comment (optional)"
	Send                = "Send"
	Sent                = "Sent"
	NotSent             = "Not sent, try again"
)

// The checks, by their ID (model.CheckName, model.CheckDetail): a name,
// one when the check fails if it says so, then the details that word what
// the recorder found. CheckEncoder takes the encoder; TargetProcess the
// process names; ForFormat the format; MacOSVersion the version;
// WindowsBuild the build; HelperMacOS and PowerShellReads a path;
// NoCaptureBackend the system; Rates and RatesOnly the allowed rates.
const (
	CheckTarget             = "target configured"
	CheckNoTarget           = "target not configured"
	CheckPlayerRunning      = "player running"
	CheckPlayerNotRunning   = "player not running"
	CheckFFmpeg             = "ffmpeg found"
	CheckNoFFmpeg           = "ffmpeg not found"
	CheckEncoder            = "encoder %s"
	CheckPlayerHeard        = "player heard"
	CheckPlayerNotHeard     = "player not heard"
	CheckOutputWritable     = "output folder writable"
	CheckOutputNotWritable  = "output folder not writable"
	CheckMacOS              = "macOS 14.2 or later"
	CheckWindows            = "Windows 10 2004 or later"
	CheckHelper             = "capture helper"
	CheckAppleScript        = "AppleScript"
	CheckPowerShell         = "Windows PowerShell"
	CheckCapture            = "capture"
	CheckSessionBus         = "D-Bus session bus"
	CheckSampleRates        = "PipeWire sample rates"
	CheckFakePlayer         = "fake player"
	CheckRecorderNotRunning = "recorder not running"
	TargetProcess           = "SPYTIFY_TARGET_PROCESS=%s"
	ForFormat               = "for encode.format=%s"
	NoSilenceHeard          = "no silence heard"
	MacOSVersion            = "macOS %s"
	WindowsBuild            = "build %s"
	HelperMacOS             = "%s; macOS asks once for System Audio Recording permission"
	AppleScriptAsks         = "macOS asks once to let spytify control the player"
	PowerShellReads         = "%s; reads what plays through the media session API"
	CapturePipeWire         = "PipeWire (pw-record)"
	CapturePulseAudio       = "PulseAudio (parec)"
	InstallCapture          = "install pipewire-bin (pw-record, pw-dump) or pulseaudio-utils"
	NoCaptureBackend        = "no %s capture backend in the alpha (Linux only)"
	RatesUnknown            = "allowed rates unknown"
	Rates                   = "%s Hz"
	RatesOnly               = "only %s Hz: 44.1 kHz tracks are resampled once. To record them unchanged, put context.properties = { default.clock.allowed-rates = [ 44100 48000 ] } in a .conf file under ~/.config/pipewire/pipewire.conf.d/, then log out and back in"
)

// The recorder's warnings, by their code (model.WarningText), in the Record
// tab's alert and the notifications: what the recorder found, then what to
// do about it on macOS, Windows or Linux. The player heard check says
// OnlySilence when it fails. Its advice follows what the capture hears:
// the player's own volume, and its slider in the Volume mixer on Windows or
// in the sound settings on Linux, but not the output device's; on macOS a
// tap refused System Audio Recording records silence too, not nothing.
// PhoneCall takes the device's format, DeviceRate or DeviceRateMono, which
// take its rate in kHz; DiskFull takes the megabytes left. VolumeLow takes
// the player's own volume in percent; VolumeZero is its 0%.
const (
	NoAudio            = "No audio from the player for 10 s."
	NoAudioMacOS       = "If macOS asks to let Spytify record system audio, allow it; it is in System Settings > Privacy & Security > Screen & System Audio Recording."
	NoAudioWindows     = "Check that the Windows Audio service runs and that the player plays on an output device."
	NoAudioLinux       = "Check that PipeWire runs: systemctl --user restart pipewire."
	OnlySilence        = "Only silence from the player for 30 s while it plays."
	OnlySilenceMacOS   = "Check that the player is not muted in its own volume; if it is not, macOS gives Spytify silence when it may not record system audio: allow it in System Settings > Privacy & Security > Screen & System Audio Recording, then quit and reopen Spytify."
	OnlySilenceWindows = "Check that the player is not muted in its own volume, nor in the Volume mixer."
	OnlySilenceLinux   = "Check that the player is not muted in its own volume, nor in the system's sound settings."
	PhoneCall          = "The player's output device runs at %s, not fit for recording: tracks will sound like a phone call."
	PhoneCallMacOS     = "A Bluetooth headset switches to call mode when an app opens its microphone. In System Settings > Sound > Input, choose another microphone, such as the built-in one, or play to another output."
	PhoneCallWindows   = "A Bluetooth headset does this while an app uses its microphone. Choose another output in Settings > System > Sound, or end what uses the headset's microphone."
	PhoneCallLinux     = "A Bluetooth headset does this while an app uses its microphone. Choose another output device, or end what uses the headset's microphone."
	DeviceRate         = "%g kHz"
	DeviceRateMono     = "%g kHz mono"
	DiskFull           = "Only %d MB free in the output folder."
	VolumeLow          = "The player's volume is at %d%%. Tracks recorded now are quieter and marked low volume."
	VolumeZero         = "The player's volume is at 0%. Nothing is recorded until it's raised."
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
	UpdateCheckFailed    = "update check: %w"
	GitHubAnswered       = "GitHub answered %s"
	Disclaimer           = "For personal use of content you are entitled to listen to; you are responsible for the terms of the services you record."
)

// The first launch's dialogs.
const (
	BeforeYouRecord  = "Before you record"
	FirstRunText     = "Spytify records the audio the player sends to your speakers. Streaming services send compressed audio, so a FLAC or WAV file avoids a second lossy pass but adds no quality."
	DontShowAgain    = "Don't show this again"
	HelpFixIssues    = "Help fix issues"
	TrackerExplained = "Spytify can send its crashes, errors with a picture of its window, traces and recording counts to its developers through Sentry, to help them fix issues."
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
	QuitAsk  = "Quit Spytify?"
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

// A shortcut's tooltip: what the key does, then the key, with the names of
// its modifiers outside macOS, which shows their symbols.
const (
	ShortcutTip = "%s (%s)"
	KeyCtrl     = "Ctrl"
	KeyAlt      = "Alt"
	KeyShift    = "Shift"
	KeySuper    = "Super"
)

// Sizes, rates and formats. Separator joins a title and its artist, or
// album, and a stream's badges; FormatBitrate takes a format, then its
// bitrate; BitrateChoice a bitrate, then the megabytes a minute of it
// takes, so an hour's cost shows before picking one.
const (
	SizeGB        = "%.1f GB"
	SizeMB        = "%.1f MB"
	SizeKB        = "%.0f kB"
	SizeB         = "%d B"
	KHz           = "%s kHz"
	FormatBitrate = "%s %v kbps"
	BitrateChoice = "%d kbps · ~ %.1f MB/min"
	Separator     = " · "
)

// The message when no graphics driver offers OpenGL, then what to do on
// Linux or Windows; on Linux, the one when no display answers. WithError
// adds the error that stopped it to a message.
const (
	WithError     = "%s\n\n(%s)"
	NoDisplay     = "Spytify can't open its window: no display answers. Open it from your desktop session, not over SSH or from a text console."
	NoWindow      = "Spytify can't open its window. It draws it with OpenGL, and no graphics driver on this computer offers it."
	NoWindowLinux = "Install Mesa's OpenGL drivers, which draw with the graphics card or, without one, with the processor, then open Spytify again:\n" +
		"Debian, Ubuntu: sudo apt install libgl1-mesa-dri libglx-mesa0 libegl-mesa0\n" +
		"Fedora: sudo dnf install mesa-dri-drivers mesa-libGL mesa-libEGL"
	NoWindowWindows = "This happens when the graphics card is disabled or has no driver (Windows then uses its basic display driver), and in some virtual machines. " +
		"Turn the card back on in Device Manager, or install its driver, then open Spytify again."
)
