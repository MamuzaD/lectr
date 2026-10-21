package transcribe

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testAtom(kind string, payload ...[]byte) []byte {
	data := bytes.Join(payload, nil)
	box := make([]byte, 8+len(data))
	binary.BigEndian.PutUint32(box, uint32(len(box)))
	copy(box[4:8], kind)
	copy(box[8:], data)
	return box
}

const testAppleJSON = `{"attributedString":{"runs":["Lecture",0," notes.",1],"attributeTable":[{"timeRange":[0,1]},{"timeRange":[1,2]}]}}`

func TestAppleTranscriptLayouts(t *testing.T) {
	inline := []byte(`{"attributedString":["Lecture",{"timeRange":[0,1]}," notes.",{"timeRange":[1,2]}]}`)
	key := testAtom("mdta", []byte("com.apple.VoiceMemos.tsrp"))
	keys := testAtom("keys", []byte{0, 0, 0, 0, 0, 0, 0, 1}, key)
	ilst := testAtom("ilst", testAtom("\x00\x00\x00\x01", testAtom("data", make([]byte, 8), []byte(testAppleJSON))))
	for name, data := range map[string][]byte{
		"indexed":        testAtom("moov", testAtom("trak", testAtom("udta", testAtom("tsrp", []byte(testAppleJSON))))),
		"trailing byte":  append(testAtom("moov", testAtom("udta", testAtom("tsrp", []byte(testAppleJSON)))), 0x29),
		"inline":         testAtom("moov", testAtom("udta", testAtom("tsrp", inline))),
		"keyed":          testAtom("moov", testAtom("meta", make([]byte, 4), keys, ilst)),
		"quicktime meta": testAtom("moov", testAtom("meta", keys, ilst)),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "memo.m4a")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			text, err := appleTranscript(path)
			if err != nil || text != "Lecture notes." {
				t.Fatalf("text=%q err=%v", text, err)
			}
		})
	}
}

func TestAppleTranscriptRejectsInvalidMetadata(t *testing.T) {
	for name, data := range map[string][]byte{
		"truncated box":             {0, 0, 0, 30, 'm', 'o', 'o', 'v'},
		"invalid json":              testAtom("tsrp", []byte(`{"attributedString":`)),
		"invalid index":             testAtom("tsrp", []byte(`{"attributedString":{"runs":["text",3],"attributeTable":[]}}`)),
		"odd runs":                  testAtom("tsrp", []byte(`{"attributedString":["unfinished"]}`)),
		"missing attributed string": testAtom("tsrp", []byte(`{}`)),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := findAppleTranscript(bytes.NewReader(data), 0, int64(len(data)), 0)
			if err == nil {
				t.Fatal("expected malformed metadata error")
			}
		})
	}
	for name, data := range map[string][]byte{
		// Transcript-like bytes in audio samples must not be treated as metadata.
		"audio payload": testAtom("mdat", testAtom("tsrp", []byte(testAppleJSON))),
		// Padding shorter than an atom header is not a malformed atom.
		"trailing byte":   append(testAtom("mdat", []byte("audio")), 0x29),
		"udta terminator": testAtom("moov", testAtom("udta", make([]byte, 4))),
	} {
		t.Run(name, func(t *testing.T) {
			text, err := findAppleTranscript(bytes.NewReader(data), 0, int64(len(data)), 0)
			if text != "" || err != nil {
				t.Fatalf("text=%q err=%v", text, err)
			}
		})
	}
}

func TestAppleTranscriptSurvivesMalformedSibling(t *testing.T) {
	tsrp := testAtom("moov", testAtom("trak", testAtom("udta", testAtom("tsrp", []byte(testAppleJSON)))))
	// A meta atom whose keys claim more entries than it holds.
	badMeta := testAtom("meta", make([]byte, 4), testAtom("keys", []byte{0, 0, 0, 0, 0, 0, 0, 5}))
	oversized := []byte{0xff, 0xff, 0xff, 0xff, 'f', 'r', 'e', 'e', 0, 0, 0, 0}
	for name, data := range map[string][]byte{
		"bad keys":         testAtom("moov", badMeta, testAtom("udta", testAtom("tsrp", []byte(testAppleJSON)))),
		"oversized atom":   append(append(testAtom("ftyp", []byte("M4A ")), tsrp...), oversized...),
		"undersized child": testAtom("moov", testAtom("trak", testAtom("udta", testAtom("tsrp", []byte(testAppleJSON)))), []byte{0, 0, 0, 3, 'f', 'r', 'e', 'e'}),
		"truncated mdat":   append(tsrp, []byte{0, 0, 1, 0, 'm', 'd', 'a', 't', 'a', 'u'}...),
	} {
		t.Run(name, func(t *testing.T) {
			text, err := findAppleTranscript(bytes.NewReader(data), 0, int64(len(data)), 0)
			if err != nil || text != "Lecture notes." {
				t.Fatalf("text=%q err=%v", text, err)
			}
		})
	}
}

func TestAppleTranscriptReportsDamagedISOMeta(t *testing.T) {
	// Real ISO meta starts with version/flags, then hdlr. A damaged child
	// after the keys must surface as an error, not as "no transcript".
	key := testAtom("mdta", []byte("com.apple.VoiceMemos.other"))
	keys := testAtom("keys", []byte{0, 0, 0, 0, 0, 0, 0, 1}, key)
	meta := testAtom("meta", make([]byte, 4), testAtom("hdlr", make([]byte, 25)), keys, []byte{0, 0, 0, 3, 'f', 'r', 'e', 'e'})
	data := testAtom("moov", meta)
	text, err := findAppleTranscript(bytes.NewReader(data), 0, int64(len(data)), 0)
	if text != "" || err == nil {
		t.Fatalf("text=%q err=%v", text, err)
	}
	ilst := testAtom("ilst", testAtom("\x00\x00\x00\x01", testAtom("data", make([]byte, 8), []byte(testAppleJSON))))
	keys = testAtom("keys", []byte{0, 0, 0, 0, 0, 0, 0, 1}, testAtom("mdta", []byte("com.apple.VoiceMemos.tsrp")))
	data = testAtom("moov", testAtom("meta", make([]byte, 4), testAtom("hdlr", make([]byte, 25)), keys, ilst))
	text, err = findAppleTranscript(bytes.NewReader(data), 0, int64(len(data)), 0)
	if err != nil || text != "Lecture notes." {
		t.Fatalf("ISO meta with hdlr: text=%q err=%v", text, err)
	}
}

func TestAppleTranscriptRequiresM4A(t *testing.T) {
	for _, name := range []string{"memo.mp3", "memo.wav"} {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte("ID3\x04\x00\x00\x00\x00\x00\x00"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := appleTranscript(path); err == nil || !strings.Contains(err.Error(), ".m4a") {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
}

func TestMissingWhisperFailsBeforeQueue(t *testing.T) {
	root := t.TempDir()
	memos := filepath.Join(root, "MATH451", "memos")
	if err := os.MkdirAll(memos, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"2026-10-20-pt01.m4a", "2026-10-20-pt02.m4a"} {
		if err := os.WriteFile(filepath.Join(memos, name), []byte("audio"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", t.TempDir())
	err := Run(context.Background(), Options{Root: root, Courses: []string{"MATH451"}})
	if err == nil || err.Error() != "mlx_whisper is missing; install it with: uv tool install mlx-whisper" {
		t.Fatalf("want one upfront missing-Whisper error, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "MATH451", "transcripts")); !os.IsNotExist(statErr) {
		t.Fatal("missing Whisper still prepared transcript folders")
	}
}

func TestNativeTranscriptRunWithoutWhisper(t *testing.T) {
	root := t.TempDir()
	memos := filepath.Join(root, "MATH451", "memos")
	if err := os.MkdirAll(memos, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(memos, "2026-10-20-pt01.m4a")
	audio := testAtom("moov", testAtom("udta", testAtom("tsrp", []byte(testAppleJSON))))
	if err := os.WriteFile(path, audio, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	opts := Options{Root: root, Courses: []string{"MATH451"}, Apple: true}
	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	part := filepath.Join(root, "MATH451", "transcripts", "2026-10-20-pt01.txt")
	got, err := os.ReadFile(part)
	if err != nil || string(got) != "Lecture notes.\n" {
		t.Fatalf("part=%q err=%v", got, err)
	}
	combined, err := os.ReadFile(filepath.Join(filepath.Dir(part), "2026-10-20.txt"))
	if err != nil || !bytes.Contains(combined, got) {
		t.Fatalf("combined=%q err=%v", combined, err)
	}
	// Existing transcripts are preserved, even if a different engine is requested.
	if err := os.WriteFile(part, []byte("hand edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts.Apple = false
	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(part)
	if string(got) != "hand edited\n" {
		t.Fatalf("existing transcript replaced: %q", got)
	}
	opts.Force = true
	if err := Run(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "mlx_whisper is missing") {
		t.Fatalf("missing explicit Whisper error: %v", err)
	}
	got, _ = os.ReadFile(part)
	if string(got) != "hand edited\n" {
		t.Fatal("failed replacement damaged existing transcript")
	}
	opts.Apple = true
	opts.Force = false
	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(part)
	if string(got) != "hand edited\n" {
		t.Fatal("Apple selection replaced existing transcript without --force")
	}
	opts.Force = true
	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(part)
	if string(got) != "Lecture notes.\n" {
		t.Fatalf("Apple --force did not replace transcript: %q", got)
	}
}

func TestDefaultWhisperIgnoresEmbeddedTranscript(t *testing.T) {
	for _, tc := range []struct {
		name  string
		audio []byte
	}{
		{"absent", testAtom("mdat", []byte("audio"))},
		{"malformed", testAtom("tsrp", []byte("bad json"))},
		{"empty", testAtom("tsrp", []byte(`{"attributedString":[]}`))},
		{"valid embedded transcript", testAtom("tsrp", []byte(testAppleJSON))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			installFakeWhisper(t, dir, `
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-dir) output_dir="$2"; shift 2 ;;
    --output-name) output_name="$2"; shift 2 ;;
    *) shift ;;
  esac
done
printf 'Whisper text\n' > "$output_dir/$output_name.txt"
`)
			path := filepath.Join(dir, "memo.m4a")
			if err := os.WriteFile(path, tc.audio, 0o600); err != nil {
				t.Fatal(err)
			}
			value := group{TranscriptDir: dir, Memos: []Memo{{Path: path, Stem: "lecture"}}}
			if err := transcribeMemo(context.Background(), &value, 0, Options{Model: DefaultModel}, func(event) bool { return true }); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(dir, "lecture.txt"))
			if err != nil || string(got) != "Whisper text\n" {
				t.Fatalf("got=%q err=%v", got, err)
			}
		})
	}
}

func TestAppleSelectionDoesNotFallBack(t *testing.T) {
	for _, tc := range []struct {
		name  string
		audio []byte
		want  string
	}{
		{"absent", testAtom("mdat", []byte("audio")), "no embedded Apple transcript"},
		{"empty", testAtom("tsrp", []byte(`{"attributedString":[]}`)), "no embedded Apple transcript"},
		{"invalid", testAtom("tsrp", []byte("bad json")), "Apple transcript unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			// A working Whisper binary must never run in explicit Apple mode.
			installFakeWhisper(t, dir, "echo called > "+dir+"/whisper-called\nexit 1\n")
			path := filepath.Join(dir, "memo.m4a")
			if err := os.WriteFile(path, tc.audio, 0o600); err != nil {
				t.Fatal(err)
			}
			value := group{TranscriptDir: dir, Memos: []Memo{{Path: path, Stem: "lecture"}}}
			err := transcribeMemo(context.Background(), &value, 0, Options{Apple: true}, func(event) bool { return true })
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, "whisper-called")); !os.IsNotExist(err) {
				t.Fatal("Apple mode invoked Whisper")
			}
			if _, err := os.Stat(filepath.Join(dir, "lecture.txt")); !os.IsNotExist(err) {
				t.Fatal("failed extraction wrote transcript")
			}
		})
	}
}

// appleLecture writes a two-part lecture whose pt01 carries an Apple
// transcript and whose pt02 does not.
func appleLecture(t *testing.T) (root, transcripts string) {
	t.Helper()
	root = t.TempDir()
	memos := filepath.Join(root, "MATH451", "memos")
	transcripts = filepath.Join(root, "MATH451", "transcripts")
	for _, dir := range []string{memos, transcripts} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string][]byte{
		filepath.Join(memos, "2026-10-20-pt01.m4a"): testAtom("moov", testAtom("udta", testAtom("tsrp", []byte(testAppleJSON)))),
		filepath.Join(memos, "2026-10-20-pt02.m4a"): testAtom("mdat", []byte("audio")),
	}
	for path, data := range files {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", t.TempDir())
	return root, transcripts
}

func TestApplePartialForceRecombinesKeptParts(t *testing.T) {
	root, transcripts := appleLecture(t)
	old := time.Now().Add(-time.Hour)
	for name, text := range map[string]string{"2026-10-20-pt01.txt": "old one\n", "2026-10-20-pt02.txt": "old two\n", "2026-10-20.txt": "OLD COMBINED\n"} {
		path := filepath.Join(transcripts, name)
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	opts := Options{Root: root, Courses: []string{"MATH451"}, Apple: true, Force: true}
	err := Run(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "kept the existing transcript") || strings.Contains(err.Error(), "--force") {
		t.Fatalf("want a kept-transcript failure without a --force hint, got: %v", err)
	}
	combined, _ := os.ReadFile(filepath.Join(transcripts, "2026-10-20.txt"))
	if !bytes.Contains(combined, []byte("Lecture notes.")) || !bytes.Contains(combined, []byte("old two")) {
		t.Fatalf("combined file not rebuilt from the parts on disk: %q", combined)
	}
	if err := Run(context.Background(), Options{Root: root, Courses: []string{"MATH451"}}); err != nil {
		t.Fatalf("follow-up run: %v", err)
	}
}

func TestAppleFailureRetryHints(t *testing.T) {
	parseErr := fmt.Errorf("%w: %w", errAppleUnavailable, errors.New("invalid character 'b'"))
	readErr := fmt.Errorf("%w: %w", errAppleUnavailable, appleReadError{errors.New("permission denied")})
	for _, tc := range []struct {
		name     string
		existing string // "" means no part transcript on disk
		cause    error
		hint     string
	}{
		{"no Apple text", "", errNoAppleTranscript, "transcribe it with Whisper: lectr transcribe MATH451 2026-10-20"},
		{"malformed Apple text", "", parseErr, "transcribe it with Whisper: lectr transcribe MATH451 2026-10-20"},
		{"unreadable recording", "", readErr, "retry with: lectr transcribe MATH451 2026-10-20 --apple"},
		{"healthy transcript kept", "notes\n", errNoAppleTranscript, "kept the existing transcript"},
		{"empty transcript kept", "\n", errNoAppleTranscript, "transcribe it with Whisper: lectr transcribe MATH451 2026-10-20 --force"},
		{"unreadable over empty transcript", "\n", readErr, "retry with: lectr transcribe MATH451 2026-10-20 --apple --force"},
		{"write error", "", errors.New("permission denied"), "retry with: lectr transcribe MATH451 2026-10-20"},
		{"failed before extraction", "\n", nil, "replace it with: lectr transcribe MATH451 2026-10-20 --force"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.existing != "" {
				if err := os.WriteFile(filepath.Join(dir, "2026-10-20-pt02.txt"), []byte(tc.existing), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			detail := "empty transcript"
			if tc.cause != nil {
				detail = tc.cause.Error()
			}
			value := group{Course: "MATH451", Date: "2026-10-20", TranscriptDir: dir, Memos: []Memo{{Path: "2026-10-20-pt02.m4a", Stem: "2026-10-20-pt02", Detail: detail}}}
			want := "MATH451: 2026-10-20-pt02.m4a: " + detail + "; " + tc.hint
			if err := memoFailure(&value, 0, tc.cause); err == nil || err.Error() != want {
				t.Fatalf("got %v\nwant %s", err, want)
			}
		})
	}
}

func TestAppleTranscriptMarksOnlyReadErrors(t *testing.T) {
	var readErr appleReadError
	if _, err := appleTranscript(filepath.Join(t.TempDir(), "missing.m4a")); !errors.As(err, &readErr) {
		t.Fatalf("missing recording: %v", err)
	}
	path := filepath.Join(t.TempDir(), "memo.m4a")
	if err := os.WriteFile(path, testAtom("tsrp", []byte("{bad")), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := appleTranscript(path); err == nil || errors.As(err, &readErr) {
		t.Fatalf("malformed transcript marked retryable: %v", err)
	}
}

func TestBareRunKeepsHandEditedCombined(t *testing.T) {
	root, transcripts := appleLecture(t)
	old := time.Now().Add(-time.Hour)
	for name, text := range map[string]string{"2026-10-20-pt01.txt": "one\n", "2026-10-20-pt02.txt": "two\n", "2026-10-20.txt": "one two\n\nMY NOTES\n"} {
		if err := os.WriteFile(filepath.Join(transcripts, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A part edited after combining must not trigger a silent recombine.
	if err := os.Chtimes(filepath.Join(transcripts, "2026-10-20.txt"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), Options{Root: root, Courses: []string{"MATH451"}}); err != nil {
		t.Fatal(err)
	}
	combined, _ := os.ReadFile(filepath.Join(transcripts, "2026-10-20.txt"))
	if string(combined) != "one two\n\nMY NOTES\n" {
		t.Fatalf("hand-edited combined file replaced: %q", combined)
	}
}

func TestAppleDryRunReportsMissingTranscripts(t *testing.T) {
	root, _ := appleLecture(t)
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = write
	runErr := Run(context.Background(), Options{Root: root, Courses: []string{"MATH451"}, Apple: true, DryRun: true})
	os.Stdout = stdout
	write.Close()
	output, _ := io.ReadAll(read)
	if runErr != nil {
		t.Fatal(runErr)
	}
	for _, want := range []string{
		"would transcribe with Apple transcript: 2026-10-20-pt01.m4a",
		"would fail (no embedded Apple transcript): 2026-10-20-pt02.m4a",
		"would not combine 2026-10-20 until every part has a transcript",
	} {
		if !strings.Contains(string(output), want) {
			t.Errorf("dry run missing %q:\n%s", want, output)
		}
	}
}

func TestAppleDryRunForOneMemoDoesNotMentionCombining(t *testing.T) {
	root, _ := appleLecture(t)
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = write
	runErr := Run(context.Background(), Options{Root: root, Courses: []string{"MATH451"}, Selector: "2026-10-20-pt02.m4a", Apple: true, DryRun: true})
	os.Stdout = stdout
	write.Close()
	output, _ := io.ReadAll(read)
	if runErr != nil {
		t.Fatal(runErr)
	}
	if strings.Contains(string(output), "combine") {
		t.Fatalf("single-memo dry run mentions combining:\n%s", output)
	}
}
