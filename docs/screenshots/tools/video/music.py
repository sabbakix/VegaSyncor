"""Adds quiet background music under the voice-over of a rendered video (the
video stream is copied). The pieces are joined with a crossfade, brought to the
loudness of the voice and then lowered by GAIN_DB. "hum" instead of the pieces
generates a soft low drone (A1/A2/E3, slightly detuned, slowly swelling).
    music.py [--gain DB] IN.mp4 OUT.mp4 MUSIC1 [MUSIC2 ...]|hum"""
import re, subprocess, sys
import imageio_ffmpeg

GAIN_DB = -20  # music level under the voice
FADE = 4.0

args = sys.argv[1:]
if args[0] == "--gain":
    GAIN_DB, args = float(args[1]), args[2:]
src, out, music = args[0], args[1], args[2:]
ff = imageio_ffmpeg.get_ffmpeg_exe()
probe = subprocess.run([ff, "-i", src], capture_output=True, text=True).stderr
h, m, s = re.search(r"Duration: (\d+):(\d+):([\d.]+)", probe).groups()
total = int(h) * 3600 + int(m) * 60 + float(s)

if music == ["hum"]:
    swell = "(0.75+0.25*sin(2*PI*0.05*t))"
    tone = "0.5*sin(2*PI*55*t)+0.35*sin(2*PI*110*t)+0.25*sin(2*PI*{d}*t)+0.12*sin(2*PI*164.8*t)"
    expr = "|".join(f"{swell}*({tone.format(d=d)})/1.3" for d in (110.3, 109.7))
    inputs = ["-f", "lavfi", "-t", f"{total + 1:.1f}", "-i", f"aevalsrc='{expr}':s=48000,lowpass=f=320"]
else:
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
