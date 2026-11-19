# Sandbox media fixtures

These tiny, fictional fixtures are embedded in the binary for `populate-sbx`.
No tenant information, third-party images or external media services are used.

- `demo-tap.png`: a generated diagram of a tap dripping into a sink.
- `demo-tap.mp4`: a one-second H.264/YUV420p clip of the same diagram.
- `voice-note-demo.m4a`: AAC speech generated with macOS's local `say` command:
  “Sandbox example. The kitchen tap is leaking. This recording contains no real
  tenant information.”

The image and clips were encoded once with FFmpeg. Running the sandbox command
does not require FFmpeg, macOS speech tools, Python or network access.
Keep fixtures small and playable when replacing them.
