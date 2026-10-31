"""Synthesises the voice-over with Kokoro (local neural TTS): one WAV per segment
of the timeline, listed in voice.json with its duration (read by render.py).
    voice.py TIMELINE.json MODEL_DIR OUT_DIR [VOICE]"""
import json, pathlib, sys
import soundfile as sf
from kokoro_onnx import Kokoro
from narration import NARRATION, seg_key

tl = json.loads(pathlib.Path(sys.argv[1]).read_text())
models, out = pathlib.Path(sys.argv[2]), pathlib.Path(sys.argv[3])
voice = sys.argv[4] if len(sys.argv) > 4 else "af_heart"
out.mkdir(parents=True, exist_ok=True)
k = Kokoro(str(models / "kokoro-v1.0.onnx"), str(models / "voices-v1.0.bin"))

keys = list(dict.fromkeys(seg_key(f) for f in tl["frames"]))
missing = [x for x in keys if x and x not in NARRATION]
if missing:
    sys.exit("no narration for:\n  " + "\n  ".join(missing))
index = {}
for i, key in enumerate(keys):
    if not key:
        continue
    samples, rate = k.create(NARRATION[key], voice=voice, speed=1.0, lang="en-us")
    wav = out / f"{i:03d}.wav"
    sf.write(wav, samples, rate, subtype="PCM_16")
    index[key] = {"wav": str(wav.resolve()), "dur": len(samples) / rate}
    print(f"{index[key]['dur']:5.1f}s  {NARRATION[key][:70]}")
(out / "voice.json").write_text(json.dumps(index, indent=1))
print(f"{sum(v['dur'] for v in index.values()) / 60:.1f} min of voice")
