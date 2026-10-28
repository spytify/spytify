# CI only: prints the mix rate of the default output device, the rate
# Windows mixes it at (its format in Sound settings), to check that
# spytify-tap records at it.
$ErrorActionPreference = "Stop"
Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;

[ComImport, Guid("BCDE0395-E52F-467C-8E3D-C4579291692E")]
class MMDeviceEnumerator {}

// Only the methods called are typed; the others hold their place in the
// vtable.
[ComImport, Guid("A95664D2-9614-4F35-A746-DE8DB63617E6"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IMMDeviceEnumerator {
  int EnumAudioEndpoints();
  int GetDefaultAudioEndpoint(int flow, int role, out IMMDevice device);
}

[ComImport, Guid("D666063F-1587-4E43-81F1-B948E807363F"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IMMDevice {
  int Activate(ref Guid iid, int context, IntPtr parameters, [MarshalAs(UnmanagedType.IUnknown)] out object client);
}

[ComImport, Guid("1CB9AD4C-DBFA-4c32-B178-C2F568A703B2"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IAudioClient {
  int Initialize();
  int GetBufferSize();
  int GetStreamLatency();
  int GetCurrentPadding();
  int IsFormatSupported();
  int GetMixFormat(out IntPtr format);
}

public static class MixRate {
  public static int OfDefaultOutput() {
    var devices = (IMMDeviceEnumerator)new MMDeviceEnumerator();
    IMMDevice device;
    // eRender, eConsole
    Marshal.ThrowExceptionForHR(devices.GetDefaultAudioEndpoint(0, 0, out device));
    var iid = typeof(IAudioClient).GUID;
    object client;
    // CLSCTX_ALL
    Marshal.ThrowExceptionForHR(device.Activate(ref iid, 23, IntPtr.Zero, out client));
    IntPtr format;
    Marshal.ThrowExceptionForHR(((IAudioClient)client).GetMixFormat(out format));
    // WAVEFORMATEX.nSamplesPerSec
    int rate = Marshal.ReadInt32(format, 4);
    Marshal.FreeCoTaskMem(format);
    return rate;
  }
}
"@
[MixRate]::OfDefaultOutput()
