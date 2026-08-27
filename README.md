# lectr

### A local lecture transcription CLI for macOS

[![Version](https://img.shields.io/github/v/release/mamuzad/lectr?logo=github)](https://github.com/mamuzad/lectr/releases)
[![Downloads](https://img.shields.io/github/downloads/mamuzad/lectr/total?logo=github)](https://github.com/mamuzad/lectr/releases)
![Build](https://img.shields.io/github/actions/workflow/status/mamuzad/lectr/release.yml?label=build&logo=github)

`lectr` routes synced Apple Voice Memos into course folders, then transcribes
them on your Mac with `mlx_whisper`. It uses one binary and one config file.
There is no server, account, or cloud transcription.

> I built `lectr` because I needed a consistent way to turn lectures into transcripts I could actually use.

## Demo

https://github.com/user-attachments/assets/6755966d-6847-443a-9415-df9a8b7bcf81

### Built With

![Go Badge](https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=fff&style=for-the-badge)

## How it works

1. The optional watcher matches each recording against your class schedule.
2. The watcher copies each match into `<root>/<course>/memos`.
3. `lectr transcribe` writes part transcripts to
   `<root>/<course>/transcripts`, then combines each lecture into one
   `YYYY-MM-DD.txt` file.

## Requirements

You need an Apple silicon Mac with:

- Go 1.26 or newer
- `ffprobe`
- `mlx_whisper`

## Setup

```bash
brew install ffmpeg
uv tool install mlx-whisper
make build
./lectr configure
```

`lectr configure` creates `~/.config/lectr/config.json`. It collects the Voice
Memos source, transcript root, semester dates, courses, class schedules, theme,
and optional Whisper prompts. Set `LECTR_CONFIG` or pass `--config PATH` to use
another file.

## Usage

```bash
# preview or transcribe every pending recording
./lectr transcribe --dry-run
./lectr transcribe

# limit transcription to a course, date, or recording
./lectr transcribe MATH351
./lectr transcribe MATH351 2026-08-25
./lectr transcribe 2026-08-25-pt01.m4a

# route Voice Memos automatically with launchd
./lectr watch install
./lectr watch status
./lectr watch uninstall

# print command help or shell completion
./lectr help
source <(./lectr completion zsh)
```

The watcher logs to `~/Library/Logs/lectr.log`. Run `./lectr watch` without an
action to see its state, paths, and transcription backlog.

Recordings use the name `YYYY-MM-DD-ptNN.m4a`. Existing valid part transcripts
are skipped unless you pass `--force`.

## Development

```bash
make test
make vet
```
