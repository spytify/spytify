// spytify-tap.exe records one process tree with WASAPI process loopback
// (Windows 10 version 2004 and later) and writes, like the macOS helper,
// a header line "SPYTIFY-TAP f32le <rate> <channels>" then interleaved
// 32-bit float frames to stdout.
//
// The stream follows the clock: when the player renders nothing, Windows
// delivers no packets, so silence is written in their place, and each packet
// lands at the frame its timestamp gives. spytifyd then sees one continuous
// stream, as from PipeWire or Core Audio.
//
// Usage: spytify-tap.exe --pid <pid> [--rate 48000]
// Exit status: 0 when spytifyd closes the pipe, 2 for bad arguments, 3 when
// the process ended (spytifyd looks for the player again), 1 otherwise.
//
// spytify-tap.exe --pid <pid> --get-volume | --set-volume <0..1> reads or
// sets the volume of the process tree's audio sessions, the player's slider
// in the Volume Mixer, and exits 4 when the tree has no session yet.

#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <audioclient.h>
#include <audiopolicy.h>
#include <fcntl.h>
#include <io.h>
#include <mmdeviceapi.h>
#include <tlhelp32.h>

#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cwchar>
#include <map>
#include <set>
#include <vector>

#include "placement.h"

// From audioclientactivationparams.h of the Windows SDK, which MinGW lacks.
enum AUDIOCLIENT_ACTIVATION_TYPE {
  AUDIOCLIENT_ACTIVATION_TYPE_DEFAULT = 0,
  AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK = 1,
};
enum PROCESS_LOOPBACK_MODE {
  PROCESS_LOOPBACK_MODE_INCLUDE_TARGET_PROCESS_TREE = 0,
  PROCESS_LOOPBACK_MODE_EXCLUDE_TARGET_PROCESS_TREE = 1,
};
struct AUDIOCLIENT_PROCESS_LOOPBACK_PARAMS {
  DWORD TargetProcessId;
  PROCESS_LOOPBACK_MODE ProcessLoopbackMode;
};
struct AUDIOCLIENT_ACTIVATION_PARAMS {
  AUDIOCLIENT_ACTIVATION_TYPE ActivationType;
  union {
    AUDIOCLIENT_PROCESS_LOOPBACK_PARAMS ProcessLoopbackParams;
  };
};
static const wchar_t kLoopbackDevice[] = L"VAD\\Process_Loopback";

static const GUID kIAudioClient = {0x1CB9AD4C, 0xDBFA, 0x4c32, {0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}};
static const GUID kIAudioCaptureClient = {0xC8ADBD64, 0xE71E, 0x48a0, {0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}};
static const GUID kICompletionHandler = {0x41D949AB, 0x9862, 0x444A, {0x80, 0xF6, 0xC2, 0x61, 0x33, 0x4D, 0xA5, 0xEB}};
static const GUID kIAgileObject = {0x94ea2b94, 0xe9cc, 0x49e0, {0xc0, 0xff, 0xee, 0x64, 0xca, 0x8f, 0x5b, 0x90}};
static const GUID kMMDeviceEnumerator = {0xBCDE0395, 0xE52F, 0x467C, {0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}};
static const GUID kIMMDeviceEnumerator = {0xA95664D2, 0x9614, 0x4F35, {0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}};
static const GUID kIAudioSessionManager2 = {0x77AA99A0, 0x1BD6, 0x484F, {0x8B, 0xC7, 0x2C, 0x65, 0x4C, 0x9A, 0x9B, 0x6F}};
static const GUID kIAudioSessionControl2 = {0xbfb7ff88, 0x7239, 0x4fc9, {0x8f, 0xa2, 0x07, 0xc9, 0x50, 0xbe, 0x9c, 0x6d}};
static const GUID kISimpleAudioVolume = {0x87CE5498, 0x68D6, 0x44E5, {0x92, 0x15, 0x6D, 0xA4, 0x7E, 0xF8, 0x83, 0xD8}};

typedef HRESULT(WINAPI *ActivateFn)(LPCWSTR, REFIID, PROPVARIANT *, IActivateAudioInterfaceCompletionHandler *,
                                    IActivateAudioInterfaceAsyncOperation **);

static int fail(const char *what, HRESULT hr) {
  fprintf(stderr, "spytify-tap: %s failed (0x%08lx)\n", what, (unsigned long)hr);
  return 1;
}

// Completion receives the audio client; it must be agile, as Windows calls
// it on a thread of its own.
class Completion : public IActivateAudioInterfaceCompletionHandler {
 public:
  HANDLE done = CreateEventW(nullptr, TRUE, FALSE, nullptr);
  HRESULT result = E_FAIL;
  IAudioClient *client = nullptr;

  virtual ~Completion() { CloseHandle(done); }
  STDMETHODIMP QueryInterface(REFIID riid, void **out) override {
    if (IsEqualGUID(riid, IID_IUnknown) || IsEqualGUID(riid, kICompletionHandler) || IsEqualGUID(riid, kIAgileObject)) {
      *out = static_cast<IActivateAudioInterfaceCompletionHandler *>(this);
      AddRef();
      return S_OK;
    }
    *out = nullptr;
    return E_NOINTERFACE;
  }
  STDMETHODIMP_(ULONG) AddRef() override { return InterlockedIncrement(&refs_); }
  STDMETHODIMP_(ULONG) Release() override {
    ULONG n = InterlockedDecrement(&refs_);
    if (n == 0) delete this;
    return n;
  }
  STDMETHODIMP ActivateCompleted(IActivateAudioInterfaceAsyncOperation *op) override {
    HRESULT activated = E_FAIL;
    IUnknown *unk = nullptr;
    result = op->GetActivateResult(&activated, &unk);
    if (SUCCEEDED(result)) result = activated;
    if (SUCCEEDED(result)) result = unk->QueryInterface(kIAudioClient, reinterpret_cast<void **>(&client));
    if (unk) unk->Release();
    SetEvent(done);
    return S_OK;
  }

 private:
  LONG refs_ = 1;
};

// Out buffers the frames of one wake-up and writes them in one call.
struct Out {
  HANDLE h = GetStdHandle(STD_OUTPUT_HANDLE);
  std::vector<char> buf;
  bool flush() {
    const char *p = buf.data();
    size_t left = buf.size();
    while (left > 0) {
      DWORD n = 0;
      if (!WriteFile(h, p, left > (1u << 30) ? (1u << 30) : (DWORD)left, &n, nullptr) || n == 0) return false;
      p += n;
      left -= n;
    }
    buf.clear();
    return true;
  }
  void zeros(uint64_t frames, UINT32 frameBytes) { buf.resize(buf.size() + frames * frameBytes, 0); }
  void bytes(const BYTE *data, uint64_t n) { buf.insert(buf.end(), data, data + n); }
};

// processTree is pid and every process below it: players play from a child.
static std::set<DWORD> processTree(DWORD root) {
  std::map<DWORD, DWORD> parent;
  HANDLE snap = CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0);
  if (snap != INVALID_HANDLE_VALUE) {
    PROCESSENTRY32W e = {};
    e.dwSize = sizeof e;
    for (BOOL ok = Process32FirstW(snap, &e); ok; ok = Process32NextW(snap, &e)) parent[e.th32ProcessID] = e.th32ParentProcessID;
    CloseHandle(snap);
  }
  std::set<DWORD> tree = {root};
  for (bool grew = true; grew;) {
    grew = false;
    for (auto &[child, up] : parent) {
      if (child != up && tree.count(up) && !tree.count(child)) {
        tree.insert(child);
        grew = true;
      }
    }
  }
  return tree;
}

// sessionVolume reads (set false) or sets the master volume of every audio
// session of the tree, on every active output device.
static int sessionVolume(DWORD pid, bool set, float value) {
  HRESULT hr = CoInitializeEx(nullptr, COINIT_MULTITHREADED);
  if (FAILED(hr)) return fail("CoInitializeEx", hr);
  std::set<DWORD> tree = processTree(pid);
  IMMDeviceEnumerator *devices = nullptr;
  hr = CoCreateInstance(kMMDeviceEnumerator, nullptr, CLSCTX_ALL, kIMMDeviceEnumerator, reinterpret_cast<void **>(&devices));
  if (FAILED(hr)) return fail("MMDeviceEnumerator", hr);
  IMMDeviceCollection *list = nullptr;
  hr = devices->EnumAudioEndpoints(eRender, DEVICE_STATE_ACTIVE, &list);
  if (FAILED(hr)) return fail("EnumAudioEndpoints", hr);
  UINT count = 0;
  list->GetCount(&count);
  int found = 0;
  float first = 0;
  for (UINT d = 0; d < count; d++) {
    IMMDevice *device = nullptr;
    IAudioSessionManager2 *manager = nullptr;
    IAudioSessionEnumerator *sessions = nullptr;
    if (FAILED(list->Item(d, &device))) continue;
    if (SUCCEEDED(device->Activate(kIAudioSessionManager2, CLSCTX_ALL, nullptr, reinterpret_cast<void **>(&manager))) &&
        SUCCEEDED(manager->GetSessionEnumerator(&sessions))) {
      int n = 0;
      sessions->GetCount(&n);
      for (int i = 0; i < n; i++) {
        IAudioSessionControl *control = nullptr;
        IAudioSessionControl2 *control2 = nullptr;
        ISimpleAudioVolume *volume = nullptr;
        DWORD owner = 0;
        if (SUCCEEDED(sessions->GetSession(i, &control)) &&
            SUCCEEDED(control->QueryInterface(kIAudioSessionControl2, reinterpret_cast<void **>(&control2))) &&
            SUCCEEDED(control2->GetProcessId(&owner)) && tree.count(owner) &&
            SUCCEEDED(control->QueryInterface(kISimpleAudioVolume, reinterpret_cast<void **>(&volume)))) {
          if (set) {
            volume->SetMasterVolume(value, nullptr);
          } else if (found == 0) {
            volume->GetMasterVolume(&first);
          }
          found++;
        }
        if (volume) volume->Release();
        if (control2) control2->Release();
        if (control) control->Release();
      }
    }
    if (sessions) sessions->Release();
    if (manager) manager->Release();
    device->Release();
  }
  list->Release();
  devices->Release();
  if (found == 0) {
    fprintf(stderr, "spytify-tap: process %lu has no audio session yet\n", (unsigned long)pid);
    return 4;
  }
  if (!set) printf("%.3f\n", first);
  return 0;
}

int wmain(int argc, wchar_t **argv) {
  DWORD pid = 0;
  DWORD rate = 48000;
  bool getVolume = false, setVolume = false;
  float volume = 0;
  for (int i = 1; i < argc; i++) {
    if (wcscmp(argv[i], L"--get-volume") == 0) {
      getVolume = true;
    } else if (i + 1 < argc && wcscmp(argv[i], L"--pid") == 0) {
      pid = wcstoul(argv[++i], nullptr, 10);
    } else if (i + 1 < argc && wcscmp(argv[i], L"--rate") == 0) {
      rate = wcstoul(argv[++i], nullptr, 10);
    } else if (i + 1 < argc && wcscmp(argv[i], L"--set-volume") == 0) {
      setVolume = true;
      volume = wcstof(argv[++i], nullptr);
    }
  }
  if (pid == 0 || rate < 8000 || rate > 384000 || (setVolume && (volume < 0 || volume > 1))) {
    fprintf(stderr, "usage: spytify-tap.exe --pid <pid> [--rate 48000] | --get-volume | --set-volume <0..1>\n");
    return 2;
  }
  if (getVolume || setVolume) return sessionVolume(pid, setVolume, volume);
  _setmode(_fileno(stdout), _O_BINARY);

  HANDLE target = OpenProcess(SYNCHRONIZE, FALSE, pid);
  if (!target) {
    fprintf(stderr, "spytify-tap: process %lu is not running\n", (unsigned long)pid);
    return 3;
  }
  HRESULT hr = CoInitializeEx(nullptr, COINIT_MULTITHREADED);
  if (FAILED(hr)) return fail("CoInitializeEx", hr);

  HMODULE mmdevapi = LoadLibraryW(L"Mmdevapi.dll");
  auto activate = mmdevapi ? reinterpret_cast<ActivateFn>(reinterpret_cast<void *>(GetProcAddress(mmdevapi, "ActivateAudioInterfaceAsync"))) : nullptr;
  if (!activate) {
    fprintf(stderr, "spytify-tap: ActivateAudioInterfaceAsync is missing; Windows 10 version 2004 or later is needed\n");
    return 1;
  }
  AUDIOCLIENT_ACTIVATION_PARAMS params = {};
  params.ActivationType = AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK;
  params.ProcessLoopbackParams.TargetProcessId = pid;
  params.ProcessLoopbackParams.ProcessLoopbackMode = PROCESS_LOOPBACK_MODE_INCLUDE_TARGET_PROCESS_TREE;
  PROPVARIANT pv = {};
  pv.vt = VT_BLOB;
  pv.blob.cbSize = sizeof(params);
  pv.blob.pBlobData = reinterpret_cast<BYTE *>(&params);

  Completion *completion = new Completion();
  IActivateAudioInterfaceAsyncOperation *op = nullptr;
  hr = activate(kLoopbackDevice, kIAudioClient, &pv, completion, &op);
  if (FAILED(hr)) return fail("ActivateAudioInterfaceAsync", hr);
  if (WaitForSingleObject(completion->done, 10000) != WAIT_OBJECT_0) return fail("process loopback activation", HRESULT_FROM_WIN32(WAIT_TIMEOUT));
  if (FAILED(completion->result)) return fail("process loopback activation", completion->result);
  IAudioClient *client = completion->client;

  // Process loopback has no mix format of its own: ask for float stereo at
  // rate and let Windows convert.
  WAVEFORMATEX format = {};
  format.wFormatTag = WAVE_FORMAT_IEEE_FLOAT;
  format.nChannels = 2;
  format.nSamplesPerSec = rate;
  format.wBitsPerSample = 32;
  format.nBlockAlign = format.nChannels * format.wBitsPerSample / 8;
  format.nAvgBytesPerSec = format.nSamplesPerSec * format.nBlockAlign;
  const REFERENCE_TIME buffer = 200000;  // 20 ms, in 100 ns units
  hr = client->Initialize(AUDCLNT_SHAREMODE_SHARED,
                          AUDCLNT_STREAMFLAGS_LOOPBACK | AUDCLNT_STREAMFLAGS_EVENTCALLBACK | AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM,
                          buffer, 0, &format, nullptr);
  if (FAILED(hr)) return fail("IAudioClient::Initialize", hr);
  HANDLE ready = CreateEventW(nullptr, FALSE, FALSE, nullptr);
  hr = client->SetEventHandle(ready);
  if (FAILED(hr)) return fail("IAudioClient::SetEventHandle", hr);
  IAudioCaptureClient *capture = nullptr;
  hr = client->GetService(kIAudioCaptureClient, reinterpret_cast<void **>(&capture));
  if (FAILED(hr)) return fail("IAudioClient::GetService", hr);

  Out out;
  char header[64];
  int n = snprintf(header, sizeof header, "SPYTIFY-TAP f32le %lu %u\n", (unsigned long)rate, (unsigned)format.nChannels);
  out.bytes(reinterpret_cast<BYTE *>(header), n);
  if (!out.flush()) return 0;

  LARGE_INTEGER freq, now;
  QueryPerformanceFrequency(&freq);
  hr = client->Start();
  if (FAILED(hr)) return fail("IAudioClient::Start", hr);
  QueryPerformanceCounter(&now);
  // Packet timestamps are QPC values in 100 ns units.
  auto hundredNs = [&](LONGLONG qpc) { return (uint64_t)((long double)qpc * 10000000.0L / (long double)freq.QuadPart); };
  const uint64_t start = hundredNs(now.QuadPart);
  auto frameAt = [&](uint64_t t) -> uint64_t { return t <= start ? 0 : (uint64_t)((long double)(t - start) * rate / 10000000.0L); };
  // Silence is written this far behind the clock, so packets that arrive
  // late still land after it.
  const uint64_t lag = rate / 10;
  const UINT32 frameBytes = format.nBlockAlign;
  uint64_t written = 0;

  HANDLE waits[2] = {ready, target};
  for (;;) {
    DWORD w = WaitForMultipleObjects(2, waits, FALSE, 20);
    if (w == WAIT_OBJECT_0 + 1) {
      out.flush();
      fprintf(stderr, "spytify-tap: process %lu ended\n", (unsigned long)pid);
      return 3;
    }
    UINT32 next = 0;
    while (SUCCEEDED(capture->GetNextPacketSize(&next)) && next > 0) {
      BYTE *data = nullptr;
      UINT32 frames = 0;
      DWORD flags = 0;
      UINT64 devicePos = 0, qpcPos = 0;
      hr = capture->GetBuffer(&data, &frames, &flags, &devicePos, &qpcPos);
      if (FAILED(hr)) return fail("IAudioCaptureClient::GetBuffer", hr);
      Placement p = place(written, frameAt(qpcPos), frames, (flags & AUDCLNT_BUFFERFLAGS_TIMESTAMP_ERROR) != 0, rate);
      out.zeros(p.zeros, frameBytes);
      if (flags & AUDCLNT_BUFFERFLAGS_SILENT) {
        out.zeros(frames - p.skip, frameBytes);
      } else {
        out.bytes(data + p.skip * frameBytes, (uint64_t)(frames - p.skip) * frameBytes);
      }
      written += p.zeros + frames - p.skip;
      capture->ReleaseBuffer(frames);
    }
    QueryPerformanceCounter(&now);
    uint64_t clock = frameAt(hundredNs(now.QuadPart));
    if (clock > lag && clock - lag > written) {
      out.zeros(clock - lag - written, frameBytes);
      written = clock - lag;
    }
    if (!out.flush()) return 0;
  }
}
