# Terminal Demo Recording (5 min) + Lower Thirds

This folder contains a reproducible terminal demo script plus an ffmpeg renderer that burns in “lower third” captions.

## What you get

- A deterministic demo run (build / attestations / capture / plan visualize / apply UI *when possible*).
- A `lowerthirds.txt` timeline you can tweak.
- A one-shot `ffmpeg` command wrapper to produce `ktl-demo-lowerthirds.mp4`.

## Step 1 — Record the screen (macOS)

Use Apple’s built-in recorder:

```bash
open -a Screenshot
```

Choose “Record Selected Portion”, select your terminal window, click Record.

Then in the terminal, run:

```bash
./scripts/demo/run-terminal-demo.sh
```

Stop recording when it finishes (≈5 minutes).

## Step 2 — Add lower thirds (ffmpeg)

Edit `scripts/demo/lowerthirds.txt` if you need different timestamps/text.

Then:

```bash
./scripts/demo/add-lowerthirds.sh /path/to/ktl-demo.mov
```

Output: `./ktl-demo-lowerthirds.mp4` (in the current working directory).

## Notes

- `apply --ui` and `plan` may require a working kubeconfig/cluster; the demo script prints `SKIP` for steps it can’t run.
- Linux-only sandbox demos are skipped on macOS.

