// FakePlayer plays a timeline (the testdata/timelines format) as a scriptable
// macOS app. Its dictionary has the vocabulary Spytify's AppleScript backend
// reads: current track, player state, player position, sound volume, next
// track, play, pause. Each item is a sine tone, so a capture can record it.
//
//	FakePlayer --timeline <file> [--start-delay <ms>] [--silent] [--status <file> --exec <command> [args...]]
//
// With --exec, FakePlayer runs the command as its child and quits when it
// ends, writing its exit status to the --status file. The child's Apple events
// then come from FakePlayer itself, which macOS lets an app send without
// asking for Automation consent: tests can script it with no one at the screen.
import AVFoundation
import Cocoa

struct Item: Decodable {
    let artist: String?
    let title: String
    let album: String?
    let length_ms: Int
    let track_number: Int?
}

struct Timeline: Decodable {
    let items: [Item]
}

func fourCC(_ s: String) -> OSType { s.utf8.reduce(0) { $0 << 8 | OSType($1) } }

func fail(_ msg: String) -> Never {
    FileHandle.standardError.write(("FakePlayer: " + msg + "\n").data(using: .utf8)!)
    exit(2)
}

// Player is the timeline's clock. The main thread changes it; the audio
// thread reads the tone to play.
final class Player {
    private let lock = NSLock()
    private var items: [Item] = []
    private var index = -1
    // started is the wall clock of the current item's 0:00 while it plays;
    // pausedAt is its position while paused.
    private var started = Date()
    private var pausedAt: Double?
    private var volume = 100

    func load(_ items: [Item]) {
        lock.withLock { self.items = items }
    }

    func start() {
        lock.withLock { go(to: 0) }
    }

    // go starts item i, or stops past the last one. The lock is held.
    private func go(to i: Int) {
        index = max(0, min(i, items.count))
        started = Date()
        pausedAt = nil
    }

    private var playing: Bool { index >= 0 && index < items.count }

    private var position: Double {
        pausedAt ?? Date().timeIntervalSince(started)
    }

    func tick() {
        lock.withLock {
            if playing && pausedAt == nil && position >= Double(items[index].length_ms) / 1000 {
                go(to: index + 1)
            }
        }
    }

    var track: FakeTrack? {
        lock.withLock { playing ? FakeTrack(items[index], index) : nil }
    }

    var state: String {
        lock.withLock { !playing ? "stopped" : pausedAt == nil ? "playing" : "paused" }
    }

    var playerPosition: Double {
        get { lock.withLock { playing ? position : 0 } }
        set {
            lock.withLock {
                guard playing else { return }
                let at = max(0, min(newValue, Double(items[index].length_ms) / 1000))
                if pausedAt != nil {
                    pausedAt = at
                } else {
                    started = Date().addingTimeInterval(-at)
                }
            }
        }
    }

    var soundVolume: Int {
        get { lock.withLock { volume } }
        set { lock.withLock { volume = max(0, min(newValue, 100)) } }
    }

    func command(_ name: String) {
        lock.withLock {
            switch name {
            case "next track":
                if playing { go(to: index + 1) }
            case "previous track":
                if playing { go(to: index - 1) }
            case "pause":
                if playing && pausedAt == nil { pausedAt = position }
            case "play":
                if let p = pausedAt { started = Date().addingTimeInterval(-p); pausedAt = nil }
            case "playpause":
                if let p = pausedAt { started = Date().addingTimeInterval(-p); pausedAt = nil } else if playing { pausedAt = position }
            default:
                break
            }
        }
    }

    // tone is what the audio thread plays now: a frequency per item, and
    // the gain, 0 while paused or stopped.
    func tone() -> (Double, Float) {
        lock.withLock {
            guard playing, pausedAt == nil else { return (0, 0) }
            return (220 * Double(1 + index % 7), Float(volume) / 100 * 0.3)
        }
    }
}

let player = Player()

@objc(FakeTrack)
final class FakeTrack: NSObject {
    @objc let trackID: String
    @objc let name: String
    @objc let artist: String
    @objc let album: String
    @objc let albumArtist: String
    @objc let duration: Int
    @objc let trackNumber: Int
    @objc let artworkUrl = ""

    init(_ it: Item, _ i: Int) {
        trackID = "fakeplayer:track:\(i)"
        name = it.title
        artist = it.artist ?? ""
        album = it.album ?? ""
        albumArtist = it.artist ?? ""
        duration = it.length_ms
        trackNumber = it.track_number ?? i + 1
    }

    override var objectSpecifier: NSScriptObjectSpecifier? {
        guard let app = NSApp.classDescription as? NSScriptClassDescription else { return nil }
        return NSPropertySpecifier(containerClassDescription: app, containerSpecifier: nil, key: "currentTrack")
    }
}

@objc(FakeCommand)
final class FakeCommand: NSScriptCommand {
    override func performDefaultImplementation() -> Any? {
        player.command(commandDescription.commandName)
        return nil
    }
}

final class Delegate: NSObject, NSApplicationDelegate {
    var startDelay = 0.5
    var silent = false
    var exec: [String] = []
    var statusPath = ""
    var engine: AVAudioEngine?

    func application(_ sender: NSApplication, delegateHandlesKey key: String) -> Bool {
        ["currentTrack", "playerState", "playerPosition", "soundVolume"].contains(key)
    }

    @objc var currentTrack: FakeTrack? { player.track }

    @objc var playerState: NSNumber {
        NSNumber(value: fourCC(["playing": "FkSp", "paused": "FkSa"][player.state] ?? "FkSs"))
    }

    @objc var playerPosition: Double {
        get { player.playerPosition }
        set { player.playerPosition = newValue }
    }

    @objc var soundVolume: Int {
        get { player.soundVolume }
        set { player.soundVolume = newValue }
    }

    func applicationDidFinishLaunching(_ note: Notification) {
        if !silent {
            startAudio()
        }
        Timer.scheduledTimer(withTimeInterval: 0.02, repeats: true) { _ in player.tick() }
        DispatchQueue.main.asyncAfter(deadline: .now() + startDelay) { player.start() }
        if !exec.isEmpty {
            runChild()
        }
    }

    // startAudio plays the tones; without an output device the timeline
    // still runs, silent.
    func startAudio() {
        let engine = AVAudioEngine()
        let rate = 44100.0
        var phase = 0.0
        let format = AVAudioFormat(standardFormatWithSampleRate: rate, channels: 2)!
        let source = AVAudioSourceNode(format: format) { _, _, frames, buffers in
            let (freq, gain) = player.tone()
            let list = UnsafeMutableAudioBufferListPointer(buffers)
            for f in 0..<Int(frames) {
                let v = Float(sin(phase)) * gain
                phase += 2 * Double.pi * freq / rate
                if phase > 2 * Double.pi { phase -= 2 * Double.pi }
                for b in list {
                    b.mData!.assumingMemoryBound(to: Float.self)[f] = v
                }
            }
            return noErr
        }
        engine.attach(source)
        engine.connect(source, to: engine.mainMixerNode, format: format)
        do {
            try engine.start()
            self.engine = engine
        } catch {
            FileHandle.standardError.write("FakePlayer: no audio output (\(error)); playing silently\n".data(using: .utf8)!)
        }
    }

    func runChild() {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: exec[0])
        p.arguments = Array(exec.dropFirst())
        p.terminationHandler = { child in
            try? "\(child.terminationStatus)\n".write(toFile: self.statusPath, atomically: true, encoding: .utf8)
            DispatchQueue.main.async { NSApp.terminate(nil) }
        }
        do {
            try p.run()
        } catch {
            try? "127\n".write(toFile: statusPath, atomically: true, encoding: .utf8)
            fail("cannot run \(exec[0]): \(error)")
        }
    }
}

let delegate = Delegate()
var args = Array(CommandLine.arguments.dropFirst())
var timelinePath = ""
while !args.isEmpty {
    let a = args.removeFirst()
    switch a {
    case "--timeline":
        timelinePath = args.isEmpty ? "" : args.removeFirst()
    case "--start-delay":
        delegate.startDelay = (Double(args.isEmpty ? "" : args.removeFirst()) ?? 500) / 1000
    case "--silent":
        delegate.silent = true
    case "--status":
        delegate.statusPath = args.isEmpty ? "" : args.removeFirst()
    case "--exec":
        delegate.exec = args
        args = []
    default:
        fail("unknown argument \(a)")
    }
}
if timelinePath.isEmpty {
    fail("usage: FakePlayer --timeline <file> [--start-delay <ms>] [--silent] [--status <file> --exec <command> [args...]]")
}
if !delegate.exec.isEmpty && delegate.statusPath.isEmpty {
    fail("--exec needs --status")
}
guard let data = FileManager.default.contents(atPath: timelinePath),
      let timeline = try? JSONDecoder().decode(Timeline.self, from: data) else {
    fail("cannot read the timeline \(timelinePath)")
}
player.load(timeline.items)

let app = NSApplication.shared
app.delegate = delegate
app.setActivationPolicy(.accessory)
app.run()
