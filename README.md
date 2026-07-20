# sprue

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Zig](https://img.shields.io/badge/Zig-0.16-f7a41d.svg)](https://ziglang.org/)
[![Dependencies](https://img.shields.io/badge/dependencies-0-lightgrey.svg)](build.zig)

sprue is a static site generator for scale model build logs. You write each build as a plain text file with photos alongside, run one command, and get a fast, clean website you can host free on GitHub Pages. It supports an interactive 360 degree spin viewer for finished models, photographed on an ordinary turntable.

It is written in Zig with no dependencies beyond the standard library, and the code follows NASA's Power of 10 rules for safety-critical software. The rules and how they are applied are described in [Code standards](#code-standards).

A sprue, if you are new to the hobby, is the plastic frame that model kit parts come attached to.

## Contents

- [Writing a build log](#writing-a-build-log)
- [The stash](#the-stash)
- [The 360 spin viewer](#the-360-spin-viewer)
- [Photographing a spin](#photographing-a-spin)
- [Installation](#installation)
- [Usage](#usage)
- [Publishing with GitHub Pages](#publishing-with-github-pages)
- [Code standards](#code-standards)
- [License](#license)

## Writing a build log

Each build lives in its own folder inside `content/`, with a `build.txt` file and any photos next to it:

```
content/
└── spitfire-mk1/
    ├── build.txt
    ├── cockpit.jpg
    └── spin/
        ├── spin_01.jpg
        └── ...
```

A `build.txt` starts with header lines, then stages. Header lines are `kit:` (required), and optionally `brand:`, `scale:`, `status:`, `started:`, `finished:`, and `next:`. A stage starts with `## ` and a title. Inside a stage, each line is a paragraph, and a line starting with `photo:` places a photo:

```
kit: Airfix 1/72 Supermarine Spitfire Mk.I
brand: Airfix
scale: 1/72
status: in progress
started: July 2026

## Cockpit
Interior green base coat, then a dark wash to pick out the moulded detail.
photo: cockpit.jpg
```

An unknown header line or content before the first stage is an error, not a guess. sprue tells you what is wrong and where.

Any kind of model works: aircraft, armour, ships, Gunpla, Warhammer miniatures. The `scale:` line is optional, and `brand:` is free text, so a Games Workshop kit is described the same way as an Airfix one.

## The stash

Most modelers buy kits faster than they build them. Buying is immediately rewarding while building is slow, so the pile of unstarted boxes grows, and the hobby has a name for it: the stash. sprue includes three small features aimed at that, each based on behavior change research rather than guesswork.

First, a kit you own but have not started is a valid build log. Give it `status: in stash` and a `build.txt` with just the header lines, no stages yet. Your front page then shows an honest count: finished, in progress, and in the stash. Seeing your own numbers every time you visit your site is self-monitoring with feedback, one of the most consistently effective behavior change techniques in the intervention literature (Michie et al. 2009, Health Psychology).

Second, the `next:` header records the specific next action and when you plan to do it, for example `next: mask the canopy, Sunday evening`. It is shown prominently on the build page. Concrete if-then plans of this kind are called implementation intentions, and a meta-analysis of 94 studies found they roughly double follow-through compared with vague goals (Gollwitzer and Sheeran 2006, Advances in Experimental Social Psychology).

Third, the format nudges without any extra effort from you. A public build log is a commitment device, and a visible stage-by-stage record supplies the goal gradient effect, where progress toward a visible finish accelerates effort (Kivetz, Urminsky and Zheng 2006, Journal of Marketing Research).

These are nudges, not magic. The stash count only helps if you log the stash honestly, and a `next:` line only works if you write one. But the cost of each is a single line of text.

## The 360 spin viewer

If a build folder contains a `spin/` directory with frames named `spin_01.jpg`, `spin_02.jpg`, and so on, the build page gets a drag-to-rotate viewer. It is not a 3D model. It is a sequence of photographs, and dragging swaps between them, the same technique product sites use. The viewer is a small piece of plain JavaScript with no libraries, generated into the site.

The example Spitfire build ships with a synthetic 36 frame spin, so you can clone the repo, generate the site, and try the viewer before you have photographed anything. Replace it with real frames by following the guide below.

## Photographing a spin

You need a turntable, a camera, and patience. The quality of the result depends almost entirely on keeping everything except the model identical between frames.

1. Place the model on a turntable. A cheap rotating cake stand works. Mark 36 evenly spaced positions around its edge, one every 10 degrees.
2. Put your camera on a tripod and lock everything: position, zoom, focus, exposure. If your camera has a manual mode, use it, because auto exposure will flicker between frames.
3. Use a plain background and steady lighting. Two lamps at 45 degrees left and right give fewer moving shadows than one overhead light.
4. Take one photo, rotate the turntable one mark, and repeat until you are back at the start.
5. Name the files `spin_01.jpg` through `spin_36.jpg` in shooting order and put them in the build's `spin/` folder.

36 frames rotates smoothly. 24 is acceptable. Below that the motion looks steppy. The frame count is read automatically from the files present.

## Installation

You need Zig 0.16 or later, from https://ziglang.org/download/ or `brew install zig` on macOS. Then:

```sh
git clone https://github.com/Joe-Speed/sprue.git
cd sprue
zig build
```

The binary lands in `zig-out/bin/sprue`.

## Usage

Run sprue from the folder that contains your `content/` directory:

```sh
zig build run
```

It reads every build under `content/`, validates it, and writes the whole site to `docs/`: an index page listing every build, one page per build, the stylesheet, and the spin viewer. Referenced photos are copied in. A missing photo or a malformed build file stops the run with a message naming the file and the problem.

To restyle the site, edit `docs/style.css` after generation, or change `src/assets/style.css` and rebuild to make it permanent.

## Publishing with GitHub Pages

Push this repository to GitHub, then in the repository settings under Pages, choose deploy from branch, branch `main`, folder `/docs`. Your site appears at `https://<username>.github.io/<repository>/` a minute later. Regenerating and pushing updates the site.

## Code standards

The Zig code follows NASA's Power of 10 rules, adapted for this project: no recursion, a fixed bound on every loop, all memory from one fixed arena allocated at startup, functions of sixty lines or less, at least two assertions per function, minimal scope for all data, every return value checked and every input validated, restrained use of comptime, single-level indirection only, and a clean build with all checks enabled. The full standards live in `.claude/skills/nasa-zig-standards/SKILL.md`.

These rules exist for flight software. A site generator does not need them. Following them anyway is the point: the constraint produces code you can read top to bottom and verify by hand, which suits a public repo whose code is meant to be read.

## License

Released under the [MIT License](LICENSE).
