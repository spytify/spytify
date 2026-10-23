using System;
using System.IO;
using System.Text;
using EspionSpotify.Models;
using EspionSpotify.Properties;
namespace EspionSpotify
{
    internal static class M3uPlaylist
    {
        private static readonly object SyncRoot = new object();
        public static void AppendCompletedRecording(string recordingPath, Track track, int durationSeconds)
        {
            if (!Settings.Default.settings_playlist_generation_enabled) return;
            var folder = Settings.Default.settings_playlist_output_path;
            var filename = Settings.Default.settings_playlist_filename;
            if (string.IsNullOrWhiteSpace(folder) || !Directory.Exists(folder)) throw new DirectoryNotFoundException("Playlist folder does not exist.");
            if (string.IsNullOrWhiteSpace(filename) || Path.GetFileName(filename) != filename || filename.IndexOfAny(Path.GetInvalidFileNameChars()) >= 0) throw new InvalidOperationException("Playlist filename is invalid.");
            if (!File.Exists(recordingPath)) throw new FileNotFoundException("Recording not found.", recordingPath);
            var playlist = Path.Combine(folder, filename);
            var title = track == null ? Path.GetFileNameWithoutExtension(recordingPath) : (string.IsNullOrWhiteSpace(track.Artists) ? track.Title : track.Artists + " - " + track.Title);
            if (string.IsNullOrWhiteSpace(title)) title = Path.GetFileNameWithoutExtension(recordingPath);
            title = title.Replace("\r", " ").Replace("\n", " ");
            lock (SyncRoot)
            {
                var header = !File.Exists(playlist) || new FileInfo(playlist).Length == 0;
                using (var writer = new StreamWriter(playlist, true, new UTF8Encoding(false)))
                {
                    if (header) writer.WriteLine("#EXTM3U");
                    writer.WriteLine("#EXTINF:" + Math.Max(0, durationSeconds) + "," + title);
                    writer.WriteLine(Path.GetFullPath(recordingPath));
                }
            }
        }
    }
}
