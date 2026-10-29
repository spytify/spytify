# CI probe only (temporary): logs, every 20 ms for -Seconds, each audio
# session on every active output (owner pid, state, peak meter) and each
# output's own peak meter, one line per change, with UTC times.
param([int]$Seconds = 30)
$ErrorActionPreference = "Stop"
Add-Type -TypeDefinition @"
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;
using System.Text;

[ComImport, Guid("BCDE0395-E52F-467C-8E3D-C4579291692E")]
class MMDeviceEnumerator {}

[ComImport, Guid("A95664D2-9614-4F35-A746-DE8DB63617E6"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IMMDeviceEnumerator {
  int EnumAudioEndpoints(int flow, int mask, out IMMDeviceCollection devices);
}

[ComImport, Guid("0BD7A1BE-7A1A-44DB-8397-CC5392387B5E"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IMMDeviceCollection {
  int GetCount(out int n);
  int Item(int i, out IMMDevice device);
}

[ComImport, Guid("D666063F-1587-4E43-81F1-B948E807363F"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IMMDevice {
  int Activate(ref Guid iid, int context, IntPtr parameters, [MarshalAs(UnmanagedType.IUnknown)] out object client);
  int OpenPropertyStore();
  int GetId([MarshalAs(UnmanagedType.LPWStr)] out string id);
}

[ComImport, Guid("77AA99A0-1BD6-484F-8BC7-2C654C9A9B6F"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IAudioSessionManager2 {
  int GetAudioSessionControl();
  int GetSimpleAudioVolume();
  int GetSessionEnumerator(out IAudioSessionEnumerator sessions);
}

[ComImport, Guid("E2F5BB11-0570-40CA-ACDD-3AA01277DEE8"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IAudioSessionEnumerator {
  int GetCount(out int n);
  int GetSession(int i, [MarshalAs(UnmanagedType.IUnknown)] out object session);
}

[ComImport, Guid("bfb7ff88-7239-4fc9-8fa2-07c950be9c6d"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IAudioSessionControl2 {
  int GetState(out int state);
  int GetDisplayName();
  int SetDisplayName();
  int GetIconPath();
  int SetIconPath();
  int GetGroupingParam();
  int SetGroupingParam();
  int RegisterAudioSessionNotification();
  int UnregisterAudioSessionNotification();
  int GetSessionIdentifier();
  int GetSessionInstanceIdentifier();
  int GetProcessId(out uint pid);
}

[ComImport, Guid("C02216F6-8C67-4B5B-9D00-D008E73E0064"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IAudioMeterInformation {
  int GetPeakValue(out float peak);
}

public static class Probe {
  static string Level(float p) {
    if (p <= 0) return "silent";
    double db = 20 * Math.Log10(p);
    return db > -20 ? "loud" : db > -60 ? "low" : "faint";
  }

  public static void Run(int seconds) {
    var devices = (IMMDeviceEnumerator)new MMDeviceEnumerator();
    var meterIid = typeof(IAudioMeterInformation).GUID;
    var managerIid = typeof(IAudioSessionManager2).GUID;
    string last = null;
    var end = DateTime.UtcNow.AddSeconds(seconds);
    while (DateTime.UtcNow < end) {
      var sb = new StringBuilder();
      IMMDeviceCollection list;
      // eRender, DEVICE_STATE_ACTIVE
      if (devices.EnumAudioEndpoints(0, 1, out list) == 0) {
        int n;
        list.GetCount(out n);
        for (int d = 0; d < n; d++) {
          IMMDevice device;
          if (list.Item(d, out device) != 0) continue;
          object o;
          float peak = 0;
          if (device.Activate(ref meterIid, 23, IntPtr.Zero, out o) == 0) ((IAudioMeterInformation)o).GetPeakValue(out peak);
          sb.AppendFormat("output{0}={1}", d, Level(peak));
          if (device.Activate(ref managerIid, 23, IntPtr.Zero, out o) == 0) {
            IAudioSessionEnumerator sessions;
            if (((IAudioSessionManager2)o).GetSessionEnumerator(out sessions) == 0) {
              int count;
              sessions.GetCount(out count);
              for (int i = 0; i < count; i++) {
                object s;
                if (sessions.GetSession(i, out s) != 0) continue;
                uint pid = 0;
                int state = -1;
                float sp = 0;
                string meter;
                ((IAudioSessionControl2)s).GetProcessId(out pid);
                ((IAudioSessionControl2)s).GetState(out state);
                try { ((IAudioMeterInformation)s).GetPeakValue(out sp); meter = Level(sp); } catch (Exception e) { meter = "no-meter " + e.GetType().Name; }
                string[] states = { "inactive", "active", "expired" };
                sb.AppendFormat(" [pid {0} {1} {2}]", pid, state >= 0 && state < 3 ? states[state] : state.ToString(), meter);
              }
            }
          }
          sb.Append("; ");
        }
      }
      string now = sb.ToString();
      if (now != last) {
        Console.WriteLine(DateTime.UtcNow.ToString("HH:mm:ss.fff") + " " + now);
        last = now;
      }
      System.Threading.Thread.Sleep(20);
    }
  }
}
"@
[Probe]::Run($Seconds)
