# lectr

### A local lecture transcription CLI for macOS

[![Version](https://img.shields.io/github/v/release/mamuzad/lectr?logo=github)](https://github.com/mamuzad/lectr/releases)
[![Downloads](https://img.shields.io/github/downloads/mamuzad/lectr/total?logo=github)](https://github.com/mamuzad/lectr/releases)
![Build](https://img.shields.io/github/actions/workflow/status/mamuzad/lectr/release.yml?label=build&logo=github)

`lectr` routes synced Apple Voice Memos into course folders, then transcribes
them on your Mac with `mlx_whisper`. Use `--apple` to extract a saved Apple
transcript instead. It uses one binary and one config file.
There is no server, account, or cloud transcription.

> I built `lectr` because I needed a consistent way to turn lectures into transcripts I could actually use.

## Demo

https://github.com/user-attachments/assets/6755966d-6847-443a-9415-df9a8b7bcf81

### Built With

![Go Badge](https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=fff&style=for-the-badge)

## How it works

1. The optional watcher matches each recording against your class schedule.
2. The watcher copies each match into `<root>/<course>/memos`.
3. `lectr transcribe` runs Whisper, or extracts saved Apple text with `--apple`. It writes
   part transcripts to `<root>/<course>/transcripts`, then combines each lecture into one
   `YYYY-MM-DD.txt` file.

## Requirements

You need an Apple silicon Mac with:

- Go 1.26 or newer
- `ffprobe`

`mlx_whisper` is required for default transcription. Extraction with `--apple`
does not require it.

## Installation

```bash
brew install ffmpeg
go install github.com/mamuzad/lectr/cmd/lectr@latest
```

`lectr` lands in `$(go env GOPATH)/bin`, so keep that on your `PATH`. Re-run
`go install` to update; `lectr help` shows the installed version.

For default Whisper transcription:

```bash
uv tool install mlx-whisper
```

<details>
<summary>Other ways to install</summary>

### Build from Source

```bash
git clone https://github.com/mamuzad/lectr.git
cd lectr
make build
./lectr help
```

Run `make test` and `make vet` to check changes.

### Install from releases

[Grab the Apple silicon build here](https://github.com/mamuzad/lectr/releases)

</details>

## Quick Start

```bash
lectr configure
```

`lectr configure` creates `~/.config/lectr/config.json`. It collects the Voice
Memos source, transcript root, semester dates, courses, class schedules, theme,
and optional Whisper prompts. Set `LECTR_CONFIG` or pass `--config PATH` to use
another file.

## Usage

```bash
# preview pending recordings or open the interactive backlog
lectr transcribe --dry-run
lectr transcribe

# limit transcription to a course, date, or recording
lectr transcribe MATH351
lectr transcribe MATH351 2026-08-25
lectr transcribe 2026-08-25-pt01.m4a

# extract Apple's saved transcript without running Whisper
lectr transcribe MATH351 2026-08-25 --apple

# replace an existing transcript with Apple's text
lectr transcribe MATH351 2026-08-25 --apple --force

# route Voice Memos automatically with launchd
lectr watch install
lectr watch status
lectr watch uninstall

# print command help or shell completion
lectr help
source <(lectr completion zsh)
```

The watcher logs to `~/Library/Logs/lectr.log`. Run `lectr watch` without an
action to see its state, paths, and transcription backlog.

Recordings use the name `YYYY-MM-DD-ptNN.m4a`. Existing valid part transcripts
are skipped unless you pass `--force`.

Interactive `lectr transcribe` runs open the backlog menu before transcription.
Dry runs and non-interactive invocations do not prompt.

`--apple` reads the transcript already embedded in a saved recording. It does
not trigger Apple's transcription or read the live text shown while recording.
The routed copy must already contain the transcript: opening the original in
Voice Memos later does not update an existing copy in a course folder. Missing,
empty, malformed, or rejected Apple transcripts produce an error that points at
Whisper instead. A recording that cannot be read yet, such as one still syncing
from iCloud, suggests running `--apple` again.

Whisper remains the default because saved Apple text was less readable for
technical terms in sampled math lectures. Apple extraction avoids transcription
time and can be useful when its text is adequate. It is an explicit source
choice rather than a claim of higher accuracy.

`--force` replaces existing transcripts using the selected source. The configured
model and course prompts apply to Whisper only. Parts extracted from Apple are
marked "Apple transcript" when they complete.
