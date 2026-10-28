// spytify-tap records the audio output of one application with a Core Audio
// process tap (macOS 14.2 and later) and writes it to stdout: one header line
//   SPYTIFY-TAP f32le <rate> <channels>
// then interleaved 32-bit float frames. It is spytifyd's macOS counterpart of
// pw-record. The processes to tap are matched by bundle id (or a bundle id
// prefix), executable name, or parent process, so a player that plays from a
// helper process is found too. It records at the rate of the device the
// player plays to, so nothing is resampled. When the set of matching
// processes, their device or the tap format changes, it exits with status 3
// and spytifyd starts it again.
//
// Usage: spytify-tap [--bundle-id ID]... [--name NAME]... [--pid PID]...
//                    [--mute] [--rate HZ --state FILE] [--list] [--plan]
//        spytify-tap --restore --state FILE
//
// --rate sets the device the recording runs at to the rate the player
// streams at, when the device offers it, so the player does not resample.
// FILE keeps each device's own rate; --restore puts it back and deletes FILE.
//
// --list prints every process Core Audio knows. --plan taps nothing: it
// prints what a recording of the target would use, one tab-separated line
// each, a device being "<uid> <nominal rate> <name>" or "-" for none. With
// --rate, it sets the rate first, as a recording would.
//   process <pid> <outputting> <name>   each matching process
//   playing <device>                    the device they play to
//   default <device>                    the default output device
//   clock <device>                      the device the recording runs at
//   rates <rate>...                     the rates the clock device offers

import AudioToolbox
import CoreAudio
import Darwin
import Foundation

let exitChanged: Int32 = 3
let exitNoPermission: Int32 = 4

struct Options {
    var bundleIDs: [String] = []
    var names: [String] = []
    var pids: [pid_t] = []
    var mute = false
    var list = false
    var plan = false
    var rate: Float64 = 0
    var state = ""
    var restore = false
}

func parseOptions() -> Options {
    var o = Options()
    var args = CommandLine.arguments.dropFirst().makeIterator()
    while let a = args.next() {
        switch a {
        case "--bundle-id": if let v = args.next() { o.bundleIDs.append(v) }
        case "--name": if let v = args.next() { o.names.append(v) }
        case "--pid": if let v = args.next(), let p = pid_t(v) { o.pids.append(p) }
        case "--mute": o.mute = true
        case "--list": o.list = true
        case "--plan": o.plan = true
        case "--rate": if let v = args.next(), let r = Float64(v) { o.rate = r }
        case "--state": if let v = args.next() { o.state = v }
        case "--restore": o.restore = true
        default:
            fputs("spytify-tap: unknown argument \(a)\n", stderr)
            exit(2)
        }
    }
    if (o.rate > 0 || o.restore) && o.state.isEmpty {
        fputs("spytify-tap: --rate and --restore need --state\n", stderr)
        exit(2)
    }
    return o
}

func log(_ s: String) { fputs("spytify-tap: \(s)\n", stderr) }

// MARK: - Core Audio property helpers

func address(_ selector: AudioObjectPropertySelector, _ scope: AudioObjectPropertyScope = kAudioObjectPropertyScopeGlobal) -> AudioObjectPropertyAddress {
    AudioObjectPropertyAddress(mSelector: selector, mScope: scope, mElement: kAudioObjectPropertyElementMain)
}

func objectList(_ object: AudioObjectID, _ selector: AudioObjectPropertySelector, _ scope: AudioObjectPropertyScope = kAudioObjectPropertyScopeGlobal) -> [AudioObjectID] {
    var addr = address(selector, scope)
    var size: UInt32 = 0
    guard AudioObjectGetPropertyDataSize(object, &addr, 0, nil, &size) == noErr, size > 0 else { return [] }
    var ids = [AudioObjectID](repeating: 0, count: Int(size) / MemoryLayout<AudioObjectID>.size)
    guard AudioObjectGetPropertyData(object, &addr, 0, nil, &size, &ids) == noErr else { return [] }
    return ids
}

func scalar<T>(_ object: AudioObjectID, _ selector: AudioObjectPropertySelector, _ initial: T) -> T? {
    var addr = address(selector)
    var value = initial
    var size = UInt32(MemoryLayout<T>.size)
    let status = withUnsafeMutableBytes(of: &value) { AudioObjectGetPropertyData(object, &addr, 0, nil, &size, $0.baseAddress!) }
    return status == noErr ? value : nil
}

func string(_ object: AudioObjectID, _ selector: AudioObjectPropertySelector) -> String? {
    var addr = address(selector)
    var value: Unmanaged<CFString>?
    var size = UInt32(MemoryLayout<Unmanaged<CFString>?>.size)
    guard AudioObjectGetPropertyData(object, &addr, 0, nil, &size, &value) == noErr, let v = value else { return nil }
    return v.takeRetainedValue() as String
}

// MARK: - Process matching

struct AudioProcess {
    let object: AudioObjectID
    let pid: pid_t
    let bundleID: String
    let name: String
    let outputting: Bool
}

func processName(_ pid: pid_t) -> String {
    var buf = [CChar](repeating: 0, count: Int(MAXPATHLEN))
    if proc_pidpath(pid, &buf, UInt32(buf.count)) > 0 {
        return URL(fileURLWithPath: String(cString: buf)).lastPathComponent
    }
    return ""
}

func parent(_ pid: pid_t) -> pid_t {
    var info = kinfo_proc()
    var size = MemoryLayout<kinfo_proc>.size
    var mib: [Int32] = [CTL_KERN, KERN_PROC, KERN_PROC_PID, pid]
    guard sysctl(&mib, 4, &info, &size, nil, 0) == 0, size > 0 else { return 0 }
    return info.kp_eproc.e_ppid
}

func audioProcesses() -> [AudioProcess] {
    objectList(AudioObjectID(kAudioObjectSystemObject), kAudioHardwarePropertyProcessObjectList).compactMap { obj in
        guard let pid: pid_t = scalar(obj, kAudioProcessPropertyPID, pid_t(0)) else { return nil }
        let running: UInt32 = scalar(obj, kAudioProcessPropertyIsRunningOutput, UInt32(0)) ?? 0
        return AudioProcess(object: obj, pid: pid, bundleID: string(obj, kAudioProcessPropertyBundleID) ?? "",
                            name: processName(pid), outputting: running != 0)
    }
}

func normalized(_ s: String) -> String {
    var n = s.lowercased()
    for suffix in [".app", ".exe"] where n.hasSuffix(suffix) { n.removeLast(suffix.count) }
    return n
}

let ownPID = getpid()

func matches(_ p: AudioProcess, _ o: Options) -> Bool {
    // The recorder is an audio client too; never tap it.
    if p.pid == ownPID || ["spytify-tap", "spytifyd"].contains(p.name) { return false }
    for id in o.bundleIDs where !id.isEmpty {
        if p.bundleID == id || p.bundleID.hasPrefix(id + ".") { return true }
    }
    let names = o.names.map(normalized)
    var pid = p.pid
    for _ in 0..<16 where pid > 1 {
        if o.pids.contains(pid) || names.contains(normalized(processName(pid))) { return true }
        pid = parent(pid)
    }
    return false
}

func targets(_ o: Options) -> [AudioProcess] { audioProcesses().filter { matches($0, o) } }

// MARK: - Tap

// The aggregate device runs at the rate of its main device, and Core Audio
// resamples the tap to it. So the main device is the one the player plays to,
// whose rate the tap already has: nothing is resampled. Another device, such
// as the one for alert sounds (a Bluetooth headset in call mode runs at
// 16 kHz), would resample the music.
func playerOutputDevice(_ processes: [AudioObjectID]) -> AudioObjectID? {
    for p in processes {
        if let dev = objectList(p, kAudioProcessPropertyDevices, kAudioObjectPropertyScopeOutput).first { return dev }
    }
    return nil
}

// Before the player outputs, the device it will most likely play to.
func defaultOutputDevice() -> AudioObjectID? {
    scalar(AudioObjectID(kAudioObjectSystemObject), kAudioHardwarePropertyDefaultOutputDevice, AudioObjectID(0))
}

// The aggregate's main device. A recording and --plan both choose here.
func clockDevice(_ processes: [AudioObjectID]) -> AudioObjectID? {
    playerOutputDevice(processes) ?? defaultOutputDevice()
}

// Whether the tap must start again on another device. A paused player plays
// to no device; then only a new default output counts.
func moved(playing: AudioObjectID?, clock: AudioObjectID?, defaultNow: AudioObjectID?, defaultAtStart: AudioObjectID?) -> Bool {
    if let playing { return playing != clock }
    return defaultNow != defaultAtStart
}

// A --plan device: uid, nominal rate and name, or "-".
func describe(_ dev: AudioObjectID?) -> String {
    guard let dev, let uid = string(dev, kAudioDevicePropertyDeviceUID) else { return "-" }
    return "\(uid)\t\(Int(nominalRate(dev)))\t\(string(dev, kAudioObjectPropertyName) ?? "")"
}

// The --plan rates of a device, a range as "<min>-<max>".
func describeRates(_ dev: AudioObjectID?) -> String {
    guard let dev else { return "-" }
    return offeredRates(dev).map { $0.mMinimum == $0.mMaximum ? "\(Int($0.mMinimum))" : "\(Int($0.mMinimum))-\(Int($0.mMaximum))" }.joined(separator: " ")
}

// MARK: - Device rate

// The state file lists the devices a recording set to the player's rate.
// The helper starts again during a session, so it sets a device once, and
// leaves it for good when someone else changes its rate in between.
struct RateChange: Codable {
    let uid: String
    let own: Float64 // the device's rate before
    let set: Float64
    var released: Bool // never put back: someone else changed it, or it refused the rate
}

func nominalRate(_ dev: AudioObjectID) -> Float64 {
    scalar(dev, kAudioDevicePropertyNominalSampleRate, Float64(0)) ?? 0
}

func offeredRates(_ dev: AudioObjectID) -> [AudioValueRange] {
    var addr = address(kAudioDevicePropertyAvailableNominalSampleRates)
    var size: UInt32 = 0
    guard AudioObjectGetPropertyDataSize(dev, &addr, 0, nil, &size) == noErr, size > 0 else { return [] }
    var ranges = [AudioValueRange](repeating: AudioValueRange(), count: Int(size) / MemoryLayout<AudioValueRange>.size)
    guard AudioObjectGetPropertyData(dev, &addr, 0, nil, &size, &ranges) == noErr else { return [] }
    return ranges
}

// Sets the rate and waits until the device runs at it.
func setRate(_ dev: AudioObjectID, _ rate: Float64) -> Bool {
    var addr = address(kAudioDevicePropertyNominalSampleRate)
    var r = rate
    guard AudioObjectSetPropertyData(dev, &addr, 0, nil, UInt32(MemoryLayout<Float64>.size), &r) == noErr else { return false }
    for _ in 0..<40 where nominalRate(dev) != rate { pause(0.05) }
    return nominalRate(dev) == rate
}

func readChanges(_ path: String) -> [RateChange] {
    guard let data = FileManager.default.contents(atPath: path) else { return [] }
    return (try? JSONDecoder().decode([RateChange].self, from: data)) ?? []
}

func writeChanges(_ changes: [RateChange], _ path: String) -> Bool {
    let url = URL(fileURLWithPath: path)
    do {
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try JSONEncoder().encode(changes).write(to: url, options: .atomic)
        return true
    } catch {
        log("cannot write \(path): \(error.localizedDescription)")
        return false
    }
}

// Sets dev to the player's rate, once a session. The change is written down
// before it is made, so a crash leaves what --restore needs.
func playAt(_ rate: Float64, on dev: AudioObjectID, state path: String) {
    guard let uid = string(dev, kAudioDevicePropertyDeviceUID) else { return }
    let name = string(dev, kAudioObjectPropertyName) ?? uid
    let now = nominalRate(dev)
    var changes = readChanges(path)
    if let i = changes.firstIndex(where: { $0.uid == uid }) {
        if !changes[i].released && now != changes[i].set {
            changes[i].released = true
            _ = writeChanges(changes, path)
            log("\(name) was changed to \(Int(now)) Hz since; it stays so")
        }
        return
    }
    if now == rate { return }
    if !offeredRates(dev).contains(where: { $0.mMinimum <= rate && rate <= $0.mMaximum }) {
        log("\(name) does not offer \(Int(rate)) Hz; it stays at \(Int(now)) Hz")
        return
    }
    changes.append(RateChange(uid: uid, own: now, set: rate, released: false))
    guard writeChanges(changes, path) else { return }
    if setRate(dev, rate) {
        log("set \(name) from \(Int(now)) to \(Int(rate)) Hz, the player's rate")
    } else {
        changes[changes.count - 1].released = true
        _ = writeChanges(changes, path)
        log("could not set \(name) to \(Int(rate)) Hz; it stays at \(Int(nominalRate(dev))) Hz")
    }
}

// Puts each device the session set back at its own rate, unless someone else
// changed it since, and deletes the state file. A device gone is forgotten.
func restoreRates(_ path: String) {
    let devices = objectList(AudioObjectID(kAudioObjectSystemObject), kAudioHardwarePropertyDevices)
    for c in readChanges(path) where !c.released {
        guard let dev = devices.first(where: { string($0, kAudioDevicePropertyDeviceUID) == c.uid }) else { continue }
        let name = string(dev, kAudioObjectPropertyName) ?? c.uid
        let now = nominalRate(dev)
        if now != c.set {
            log("\(name) was changed to \(Int(now)) Hz since; it stays so")
        } else if setRate(dev, c.own) {
            log("put \(name) back at \(Int(c.own)) Hz")
        } else {
            log("could not put \(name) back at \(Int(c.own)) Hz")
        }
    }
    try? FileManager.default.removeItem(atPath: path)
}

final class Recorder {
    let tapID: AudioObjectID
    let aggregateID: AudioObjectID
    var procID: AudioDeviceIOProcID?
    let format: AudioStreamBasicDescription
    let queue = DispatchQueue(label: "spytify-tap.io", qos: .userInitiated)
    var interleaved = [Float]()

    init(processes: [AudioObjectID], device: AudioObjectID?, mute: Bool) throws {
        let desc = CATapDescription(stereoMixdownOfProcesses: processes)
        desc.uuid = UUID()
        desc.name = "spytify-tap"
        desc.isPrivate = true
        desc.muteBehavior = mute ? .muted : .unmuted
        var tap = AudioObjectID(0)
        var status = AudioHardwareCreateProcessTap(desc, &tap)
        guard status == noErr else { throw NSError(domain: "spytify-tap", code: Int(status), userInfo: [NSLocalizedDescriptionKey: "AudioHardwareCreateProcessTap failed (\(status))"]) }
        tapID = tap

        guard let fmt: AudioStreamBasicDescription = scalar(tap, kAudioTapPropertyFormat, AudioStreamBasicDescription()) else {
            AudioHardwareDestroyProcessTap(tap)
            throw NSError(domain: "spytify-tap", code: 1, userInfo: [NSLocalizedDescriptionKey: "tap format unavailable"])
        }
        if fmt.mFormatID != kAudioFormatLinearPCM || fmt.mFormatFlags & kAudioFormatFlagIsFloat == 0 || fmt.mBitsPerChannel != 32 {
            AudioHardwareDestroyProcessTap(tap)
            throw NSError(domain: "spytify-tap", code: 1, userInfo: [NSLocalizedDescriptionKey: "unexpected tap format \(fmt)"])
        }
        format = fmt

        var description: [String: Any] = [
            kAudioAggregateDeviceNameKey: "spytify-tap",
            kAudioAggregateDeviceUIDKey: "org.spytify.tap." + UUID().uuidString,
            kAudioAggregateDeviceIsPrivateKey: true,
            kAudioAggregateDeviceIsStackedKey: false,
            kAudioAggregateDeviceTapAutoStartKey: true,
            kAudioAggregateDeviceTapListKey: [[kAudioSubTapDriftCompensationKey: true, kAudioSubTapUIDKey: desc.uuid.uuidString]],
        ]
        if let dev = device, let out = string(dev, kAudioDevicePropertyDeviceUID) {
            description[kAudioAggregateDeviceMainSubDeviceKey] = out
            description[kAudioAggregateDeviceSubDeviceListKey] = [[kAudioSubDeviceUIDKey: out]]
        }
        var agg = AudioObjectID(0)
        status = AudioHardwareCreateAggregateDevice(description as CFDictionary, &agg)
        guard status == noErr else {
            AudioHardwareDestroyProcessTap(tap)
            throw NSError(domain: "spytify-tap", code: Int(status), userInfo: [NSLocalizedDescriptionKey: "AudioHardwareCreateAggregateDevice failed (\(status))"])
        }
        aggregateID = agg
    }

    // Frames arrive at the aggregate device's rate, which follows its main
    // device, not at the rate the tap format reports.
    var rate: Float64 {
        scalar(aggregateID, kAudioDevicePropertyNominalSampleRate, Float64(0)) ?? format.mSampleRate
    }

    func start() throws {
        let channels = Int(format.mChannelsPerFrame)
        let nonInterleaved = format.mFormatFlags & kAudioFormatFlagIsNonInterleaved != 0
        let stdout = FileHandle.standardOutput
        let status = AudioDeviceCreateIOProcIDWithBlock(&procID, aggregateID, queue) { [self] _, input, _, _, _ in
            let buffers = UnsafeMutableAudioBufferListPointer(UnsafeMutablePointer(mutating: input))
            guard buffers.count > 0 else { return }
            if !nonInterleaved {
                // One buffer, already interleaved.
                let b = buffers[0]
                if let data = b.mData, b.mDataByteSize > 0 {
                    stdout.write(Data(bytes: data, count: Int(b.mDataByteSize)))
                }
                return
            }
            let frames = Int(buffers[0].mDataByteSize) / MemoryLayout<Float>.size
            if interleaved.count != frames * channels { interleaved = [Float](repeating: 0, count: frames * channels) }
            for (c, b) in buffers.prefix(channels).enumerated() {
                guard let p = b.mData?.assumingMemoryBound(to: Float.self) else { continue }
                for i in 0..<frames { interleaved[i * channels + c] = p[i] }
            }
            interleaved.withUnsafeBytes { stdout.write(Data($0)) }
        }
        guard status == noErr, let proc = procID else { throw NSError(domain: "spytify-tap", code: Int(status), userInfo: [NSLocalizedDescriptionKey: "AudioDeviceCreateIOProcIDWithBlock failed (\(status))"]) }
        let header = "SPYTIFY-TAP f32le \(Int(rate)) \(channels)\n"
        stdout.write(header.data(using: .utf8)!)
        let started = AudioDeviceStart(aggregateID, proc)
        guard started == noErr else { throw NSError(domain: "spytify-tap", code: Int(started), userInfo: [NSLocalizedDescriptionKey: "AudioDeviceStart failed (\(started))"]) }
    }

    func stop() {
        if let proc = procID {
            AudioDeviceStop(aggregateID, proc)
            AudioDeviceDestroyIOProcID(aggregateID, proc)
        }
        AudioHardwareDestroyAggregateDevice(aggregateID)
        AudioHardwareDestroyProcessTap(tapID)
    }
}

// MARK: - Main

let options = parseOptions()
signal(SIGPIPE, SIG_IGN)

// A command-line tool has no run loop for the HAL's notifications; without
// this the process list it returns never changes.
do {
    var addr = address(kAudioHardwarePropertyRunLoop)
    var loop: UnsafeMutableRawPointer? = nil // a NULL CFRunLoopRef
    _ = AudioObjectSetPropertyData(AudioObjectID(kAudioObjectSystemObject), &addr, 0, nil, UInt32(MemoryLayout<UnsafeMutableRawPointer?>.size), &loop)
}

func pause(_ seconds: TimeInterval) {
    RunLoop.current.run(until: Date().addingTimeInterval(seconds))
}

if options.list {
    for p in audioProcesses() {
        print("pid=\(p.pid) name=\(p.name) bundle=\(p.bundleID) output=\(p.outputting)")
    }
    exit(0)
}
if options.restore {
    restoreRates(options.state)
    exit(0)
}
if options.bundleIDs.isEmpty && options.names.isEmpty && options.pids.isEmpty {
    log("give at least one --bundle-id, --name or --pid")
    exit(2)
}
if options.plan {
    let found = targets(options)
    let objects = found.map(\.object)
    if options.rate > 0, let dev = clockDevice(objects) { playAt(options.rate, on: dev, state: options.state) }
    for p in found { print("process\t\(p.pid)\t\(p.outputting)\t\(p.name)") }
    print("playing\t" + describe(playerOutputDevice(objects)))
    print("default\t" + describe(defaultOutputDevice()))
    print("clock\t" + describe(clockDevice(objects)))
    print("rates\t" + describeRates(clockDevice(objects)))
    exit(0)
}

var recorder: Recorder?
func shutdown(_ code: Int32) -> Never {
    recorder?.stop()
    exit(code)
}
for s in [SIGINT, SIGTERM, SIGHUP] {
    signal(s) { _ in recorder?.stop(); exit(0) }
}

var waiting = false
var current: Set<AudioObjectID> = []
var device: AudioObjectID?
while recorder == nil {
    let found = targets(options)
    if found.isEmpty {
        if !waiting { log("waiting for the target's audio"); waiting = true }
        pause(0.25)
        continue
    }
    let objects = found.map(\.object)
    current = Set(objects)
    device = clockDevice(objects)
    if options.rate > 0, let dev = device { playAt(options.rate, on: dev, state: options.state) }
    do {
        let r = try Recorder(processes: Array(current), device: device, mute: options.mute)
        try r.start()
        recorder = r
        let on = device.flatMap { string($0, kAudioObjectPropertyName) } ?? "no output device"
        log("tapping " + found.map { "\($0.name)[\($0.pid)]" }.joined(separator: ", ") + " on \(on) at \(Int(r.rate)) Hz (tap reports \(Int(r.format.mSampleRate)) Hz)")
    } catch {
        log("\(error.localizedDescription)")
        shutdown(1)
    }
}

// Follow the player: a new helper process, an exit, or a new output device
// means a new tap.
let startFormat = recorder!.format
let startRate = recorder!.rate
let startDefault = defaultOutputDevice()
while true {
    pause(0.5)
    let found = targets(options).map(\.object)
    let now = Set(found)
    let fmt: AudioStreamBasicDescription? = scalar(recorder!.tapID, kAudioTapPropertyFormat, AudioStreamBasicDescription())
    let away = moved(playing: playerOutputDevice(found), clock: device, defaultNow: defaultOutputDevice(), defaultAtStart: startDefault)
    if now != current || recorder!.rate != startRate || fmt?.mSampleRate != startFormat.mSampleRate || fmt?.mChannelsPerFrame != startFormat.mChannelsPerFrame || away {
        log("target processes or output format changed")
        shutdown(exitChanged)
    }
}
