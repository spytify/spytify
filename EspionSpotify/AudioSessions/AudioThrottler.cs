using System;
using System.Collections.Concurrent;
using System.Collections.Generic;
using System.Linq;
using System.Threading;
using System.Threading.Tasks;
using EspionSpotify.Models;
using EspionSpotify.AudioSessions;
using EspionSpotify.Enums;
using EspionSpotify.Extensions;
using NAudio.CoreAudioApi;
using NAudio.Utils;
using NAudio.Wave;

namespace EspionSpotify.AudioSessions
{
    public sealed class AudioThrottler: IAudioThrottler, IDisposable
    {
        private bool _disposed;
        private const int DETECTED_SILENCE_MS = 500;
        private readonly object _lockObject;

        private const int BUFFER_TOTAL_SIZE_IN_SECOND = 4;
        private const int BYTES_PER_SAMPLE = 2;
        private const short SILENCE_RMS_THRESHOLD = 8;
        
        private readonly IMainAudioSession _audioSession;

        private CancellationTokenSource _cancellationTokenSource;
        private WasapiLoopbackCapture _waveIn;
        private IAudioCircularBuffer _buffer;

        private int WaveAverageShortPerSecond => (int) (_waveIn.WaveFormat.AverageBytesPerSecond / 2.0);
        private int BufferReadOffset => (int)(_waveIn.WaveFormat.AverageBytesPerSecond * (BUFFER_TOTAL_SIZE_IN_SECOND / 4.0));
        private int BufferMaxLength => _waveIn.WaveFormat.AverageBytesPerSecond * BUFFER_TOTAL_SIZE_IN_SECOND;

        // Length in bytes of the analysis window used to decide whether a region is silent.
        private int SilenceAverageShortLength =>
            Math.Max(BYTES_PER_SAMPLE, (int)(DETECTED_SILENCE_MS / 1_000.0 * _waveIn.WaveFormat.AverageBytesPerSecond) -
                                    (BYTES_PER_SAMPLE - 1));
        
        public bool Running { get; set; }
        public WaveFormat WaveFormat => _waveIn.WaveFormat;

        public bool BufferIsHalfFull
        {
            get
            {
                lock (_lockObject)
                {
                    return _buffer.Count > (int)(BufferMaxLength / 2.0);
                }
            }
        }

        public bool BufferIsReady
        {
            get
            {
                lock (_lockObject)
                {
                    return _buffer.Count > BufferReadOffset;
                }
            }
        }

        public AudioThrottler(IMainAudioSession audioSession)
        {
            _audioSession = audioSession;
            _lockObject = new object();
        }
        
        public async Task Run(CancellationTokenSource cancellationTokenSource)
        {
            _cancellationTokenSource = cancellationTokenSource;

            if (_audioSession.AudioMMDevicesManager.AudioEndPointDevice == null) return;

            Running = true;

            _waveIn = new WasapiLoopbackCapture(_audioSession.AudioMMDevicesManager.AudioEndPointDevice);
            _waveIn.ShareMode = AudioClientShareMode.Shared;
            _waveIn.DataAvailable += WaveIn_DataAvailable;

            var silencer = CreateSilencer();
            silencer.Play();

            await Task.Delay(50);
            _waveIn.StartRecording();

            while (Running)
            {
                if (_cancellationTokenSource.IsCancellationRequested) return;
                await Task.Delay(100);
            }

            silencer.Stop();
            silencer.Dispose();
            
            _waveIn.StopRecording();
        }

        private WaveOutEvent CreateSilencer()
        {

            var silenceProvider = new SilenceProvider(_waveIn.WaveFormat).ToSampleProvider();
            var wo = new WaveOutEvent();
            wo.Init(silenceProvider);
            return wo;
        }

        public async Task<AudioWaveBuffer> Read(SilenceAnalyzer silence = SilenceAnalyzer.None)
        {
            AudioWaveBuffer result = null;

            await WaitBufferReady();
            
            switch (silence)
            {
                case SilenceAnalyzer.TrimStart:
                {
                    lock (_lockObject)
                    {
                        var readStart = TrimLeadingBufferUntilNewTrack(out var data);
                        if (readStart > 0)
                        {
                            result = ToAudioWaveBuffer(data, readStart);
                        }
                    }

                    break;
                }
                case SilenceAnalyzer.TrimEnd:
                {
                    lock (_lockObject)
                    {
                        // var readPeek = _buffer.Peek(out var dataPeek, 0, _buffer.Count);
                        // var validRead = BufferPositionWithoutSilence(dataPeek, readPeek, recursive: false);
                        // var read = _buffer.Read(out var data, 0, validRead);
                        var read = _buffer.Read(out var data, 0, _buffer.Count);
                        if (read > 0)
                        {
                            result = ToAudioWaveBuffer(data, read);
                        }
                    }

                    break;
                }
                case SilenceAnalyzer.None:
                {
                    lock (_lockObject)
                    {
                        if (_buffer.Count >= BufferReadOffset)
                        {
                            var read = _buffer.Read(out var data, 0,
                            _waveIn.WaveFormat.AverageBytesPerSecond);
                            if (read > 0)
                            {
                                result = ToAudioWaveBuffer(data, read);
                            }
                        }
                    }

                    break;
                }
                default:
                {
                    throw new ArgumentOutOfRangeException(nameof(silence), silence,
                        @"Cannot cast to Silence Analyzer. Not supported.");
                }
            }

            return result;
        }

        /// <summary>
        /// Aligns a new recording with the start of the new track.
        /// Spotify updates the window title slightly before the previous track actually ends, so the
        /// audio that is still in the buffer (or still in flight) can belong to the previous track.
        /// We locate the last sustained silence inside the buffer (the gap that separates the outgoing
        /// track from the incoming one) and cut there, so the new track keeps its true beginning.
        /// When no such gap exists the previous audio cannot be told apart from the incoming one, so we
        /// fall back to flushing the whole buffer, which never records leftover audio of the old track.
        /// </summary>
        private int TrimLeadingBufferUntilNewTrack(out byte[] data)
        {
            data = null;

            if (_buffer.Count > 0)
            {
                var readPeek = _buffer.Peek(out var peeked, 0, _buffer.Count);
                if (readPeek > 0)
                {
                    var boundary = BufferPositionWithoutSilence(peeked, readPeek, SilenceAverageShortLength, recursive: true);
                    if (boundary > 0)
                    {
                        _buffer.Advance(boundary);
                    }
                    else
                    {
                        // no gap found: discard everything that is still buffered
                        _buffer.Advance(_buffer.Count);
                    }
                }
                else
                {
                    _buffer.Advance(_buffer.Count);
                }
            }

            var read = _buffer.Read(out var readData, 0, _waveIn.WaveFormat.AverageBytesPerSecond);
            if (read > 0)
            {
                data = readData;
                return read;
            }

            return 0;
        }

        public async Task WaitBufferReady()
        {
            var timeout = BUFFER_TOTAL_SIZE_IN_SECOND;
            var pace = 100;
            while (!BufferIsHalfFull && timeout > 0)
            {
                timeout -= pace;
                await Task.Delay(pace);
            }
        }

        private void WaveIn_DataAvailable(object sender, WaveInEventArgs e)
        {
            if (!Running) return;

            lock (_lockObject)
            {
                if (_buffer == null)
                {
                    _buffer = new AudioCircularBuffer(BufferMaxLength);
                }

                _buffer.Write(e.Buffer, 0, e.BytesRecorded);
            }
        }

        private AudioWaveBuffer ToAudioWaveBuffer(byte[] data, int read)
        {
            return new AudioWaveBuffer
            {
                Buffer = data,
                BytesRecordedCount = read,
                // WithSilence = WithSilence(data, read)
            };
        }
        
        // private bool WithSilence(byte[] buffer, int count)
        // {
        //     var received = new byte[count];
        //     Array.Copy(buffer, 0, received, 0, count);
        //     return received.TakeWhile(x => x == 0).Count() > SilenceAverageByteLength;
        // }

        /// <summary>
        /// Finds the offset (in bytes) of a silent region inside the given audio window.
        /// Audio is handled as 16-bit PCM samples; a region is considered silent when the RMS of
        /// its samples stays at or below <see cref="SILENCE_RMS_THRESHOLD"/>.
        /// With <paramref name="recursive"/> set the last silent region is returned (used to keep the
        /// incoming track intact), otherwise the first one (used to drop trailing silence).
        /// Returns -1 when no silent region is found.
        /// </summary>
        public static int BufferPositionWithoutSilence(byte[] buffer, int count, int windowLength, bool recursive = false)
        {
            if (buffer == null || count <= 0) return -1;

            var window = Math.Max(BYTES_PER_SAMPLE, windowLength);
            var step = Math.Max(BYTES_PER_SAMPLE, window / 2);
            var lastSilence = -1;
            var firstSilence = -1;

            for (var i = 0; i + BYTES_PER_SAMPLE <= count; i += step)
            {
                var windowLengthToRead = Math.Min(window, count - i);
                windowLengthToRead -= windowLengthToRead % BYTES_PER_SAMPLE;
                if (windowLengthToRead < BYTES_PER_SAMPLE) break;

                var sampleCount = windowLengthToRead / BYTES_PER_SAMPLE;
                double sumOfSquares = 0;
                for (var j = 0; j < sampleCount; j++)
                {
                    var sample = BitConverter.ToInt16(buffer, i + j * BYTES_PER_SAMPLE);
                    sumOfSquares += sample * (double)sample;
                }

                var rms = Math.Sqrt(sumOfSquares / sampleCount);
                if (rms > SILENCE_RMS_THRESHOLD) continue;

                if (firstSilence < 0) firstSilence = i;
                lastSilence = i;

                if (!recursive) break;
            }

            return recursive ? lastSilence : firstSilence;
        }

        #region Dispose
        public void Dispose()
        {
            Dispose(true);

            GC.SuppressFinalize(this);
        }

        private void Dispose(bool disposing)
        {
            if (_disposed) return;

            if (disposing)
            {
                _cancellationTokenSource.Cancel();
                _waveIn.Dispose();
            }

            _disposed = true;
        }

        #endregion DisposeRecorder
    }
}