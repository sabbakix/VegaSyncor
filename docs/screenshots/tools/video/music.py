"""Adds quiet background music under the voice-over of a rendered video (the
video stream is copied). The pieces are joined with a crossfade, brought to the
loudness of the voice and then lowered by GAIN_DB.
    music.py IN.mp4 OUT.mp4 MUSIC1 [MUSIC2 ...]"""
import re, subprocess, sys
import imageio_ffmpeg

GAIN_DB = -20  # music level under the voice
FADE = 4.0

src, out, music = sys.argv[1], sys.argv[2], sys.argv[3:]
ff = imageio_ffmpeg.get_ffmpeg_exe()
probe = subprocess.run([ff, "-i", src], capture_output=True, text=True).stderr
h, m, s = re.search(r"Duration: (\d+):(\d+):([\d.]+)", probe).groups()
total = int(h) * 3600 + int(m) * 60 + float(s)

inputs = [x for p in music for x in ("-i", p)]
chain = "[1:a]aformat=channel_layouts=stereo:sample_rates=48000[m0];"
for i in range(1, len(music)):
    chain += (f"[{i + 1}:a]aformat=channel_layouts=stereo:sample_rates=48000[n{i}];"
              f"[m{i - 1}][n{i}]acrossfade=d={FADE}[m{i}];")
chain += (f"[m{len(music) - 1}]loudnorm=I=-16:TP=-1.5,volume={GAIN_DB}dB,"
          f"afade=t=in:d=2,afade=t=out:st={total - FADE:.2f}:d={FADE}[bg];"
          "[0:a]aformat=channel_layouts=stereo:sample_rates=48000[v];"
          "[v][bg]amix=inputs=2:duration=first:normalize=0,alimiter=limit=0.95[a]")
subprocess.run([ff, "-y", "-loglevel", "error", "-i", src, *inputs, "-filter_complex", chain,
                "-map", "0:v", "-map", "[a]", "-c:v", "copy", "-c:a", "aac", "-b:a", "192k",
                "-movflags", "+faststart", out], check=True)
print(f"{out}: {total:.0f}s, music {GAIN_DB} dB under the voice")
