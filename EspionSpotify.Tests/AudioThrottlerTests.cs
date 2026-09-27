using System;
using EspionSpotify.AudioSessions;
using Xunit;

namespace EspionSpotify.Tests
{
    public class AudioThrottlerTests
    {
        // 16-bit stereo at 44100 Hz.
        private const int AverageBytesPerSecond = 44100 * 2 * 2;
        // 500 ms of audio expressed in bytes, matching AudioThrottler's silence window.
        private const int WindowLength = 44100 * 2 * 2 / 2;

        private static byte[] PcmSilence(int byteCount)
        {
            return new byte[byteCount];
        }

        private static byte[] PcmTone(int byteCount, short amplitude)
        {
            var buffer = new byte[byteCount];
            for (var i = 0; i + 1 < byteCount; i += 2)
            {
                BitConverter.GetBytes(amplitude).CopyTo(buffer, i);
            }

            return buffer;
        }

        private static byte[] PcmConcat(params byte[][] parts)
        {
            var result = new byte[0];
            foreach (var part in parts)
            {
                var combined = new byte[result.Length + part.Length];
                Buffer.BlockCopy(result, 0, combined, 0, result.Length);
                Buffer.BlockCopy(part, 0, combined, result.Length, part.Length);
                result = combined;
            }

            return result;
        }

        [Fact]
        public void BufferPositionWithoutSilence_ReturnsMinusOne_WhenBufferHasNoSilence()
        {
            var audio = PcmTone(AverageBytesPerSecond, 3000);

            var result = AudioThrottler.BufferPositionWithoutSilence(audio, audio.Length, WindowLength,
                recursive: true);

            Assert.Equal(-1, result);
        }

        [Fact]
        public void BufferPositionWithoutSilence_ReturnsMinusOne_WhenBufferIsEmpty()
        {
            var audio = new byte[0];

            var result = AudioThrottler.BufferPositionWithoutSilence(audio, 0, WindowLength, recursive: true);

            Assert.Equal(-1, result);
        }

        [Fact]
        public void BufferPositionWithoutSilence_ReturnsMinusOne_WhenBufferIsNull()
        {
            var result = AudioThrottler.BufferPositionWithoutSilence(null, 100, WindowLength, recursive: true);

            Assert.Equal(-1, result);
        }

        [Fact]
        public void BufferPositionWithoutSilence_DoesNotTreatQuietAudioAsSilence()
        {
            // slightly above the RMS threshold, this is audio and not a boundary between two tracks
            var audio = PcmTone(AverageBytesPerSecond, 12);

            var result = AudioThrottler.BufferPositionWithoutSilence(audio, audio.Length, WindowLength,
                recursive: true);

            Assert.Equal(-1, result);
        }

        [Fact]
        public void BufferPositionWithoutSilence_ReturnsFirstSilentRegion_WhenNotRecursive()
        {
            var oldTrack = PcmTone((int) (0.2 * AverageBytesPerSecond), 2000);
            var firstGap = PcmSilence((int) (0.6 * AverageBytesPerSecond));
            var newTrack = PcmTone((int) (0.2 * AverageBytesPerSecond), 2000);
            var audio = PcmConcat(oldTrack, firstGap, newTrack);

            var result = AudioThrottler.BufferPositionWithoutSilence(audio, audio.Length, WindowLength,
                recursive: false);

            Assert.True(result >= oldTrack.Length - WindowLength && result <= oldTrack.Length + WindowLength,
                $"expected the first gap near {oldTrack.Length} but got {result}");
        }

        [Fact]
        public void BufferPositionWithoutSilence_ReturnsLastSilentRegion_WhenRecursive()
        {
            var oldTrack = PcmTone((int) (0.2 * AverageBytesPerSecond), 2000);
            var firstGap = PcmSilence((int) (0.6 * AverageBytesPerSecond));
            var newTrack = PcmTone((int) (0.1 * AverageBytesPerSecond), 2000);
            var secondGap = PcmSilence((int) (0.6 * AverageBytesPerSecond));
            var audio = PcmConcat(oldTrack, firstGap, newTrack, secondGap);

            var result = AudioThrottler.BufferPositionWithoutSilence(audio, audio.Length, WindowLength,
                recursive: true);

            var secondGapStart = oldTrack.Length + firstGap.Length + newTrack.Length;
            Assert.True(result >= secondGapStart - WindowLength && result <= secondGapStart + WindowLength,
                $"expected the last gap near {secondGapStart} but got {result}");
        }

        [Fact]
        public void BufferPositionWithoutSilence_IgnoresSilenceShorterThanTheWindow()
        {
            // a short dropout inside a track is not a track boundary
            var before = PcmTone((int) (0.2 * AverageBytesPerSecond), 2000);
            var dropout = PcmSilence(AverageBytesPerSecond / 20);
            var after = PcmTone((int) (0.2 * AverageBytesPerSecond), 2000);
            var audio = PcmConcat(before, dropout, after);

            var result = AudioThrottler.BufferPositionWithoutSilence(audio, audio.Length, WindowLength,
                recursive: true);

            Assert.Equal(-1, result);
        }
    }
}
