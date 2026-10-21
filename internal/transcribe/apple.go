package transcribe

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxAppleTranscriptSize = 16 << 20

type mediaAtom struct {
	kind       string
	start, end int64
}

// appleTranscript reads the transcript Voice Memos saves in a recording's
// metadata, either as a tsrp atom or as keyed QuickTime metadata. It walks
// atom headers only, so the audio is never loaded. A recording without a
// saved transcript returns empty text, not an error.
func appleTranscript(path string) (string, error) {
	if !strings.EqualFold(filepath.Ext(path), ".m4a") {
		return "", errors.New("only Voice Memos .m4a recordings carry one")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", appleReadError{err}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", appleReadError{err}
	}
	return findAppleTranscript(recordingReader{f}, 0, info.Size(), 0)
}

// appleReadError is a failure to read the recording itself rather than a
// problem with its contents, so it may clear once iCloud finishes syncing.
type appleReadError struct{ err error }

func (e appleReadError) Error() string { return e.err.Error() }
func (e appleReadError) Unwrap() error { return e.err }

type recordingReader struct{ file *os.File }

func (r recordingReader) ReadAt(p []byte, off int64) (int, error) {
	n, err := r.file.ReadAt(p, off)
	if err != nil {
		err = appleReadError{err}
	}
	return n, err
}

// mediaAtoms returns the atoms read before any error alongside it, so a
// damaged tail, such as a partly downloaded recording, keeps what precedes it.
func mediaAtoms(r io.ReaderAt, start, end int64) ([]mediaAtom, error) {
	if start < 0 || start > end {
		return nil, errors.New("invalid media atom bounds")
	}
	var atoms []mediaAtom
	for start < end {
		// Too short for an atom header: trailing padding, or the four-byte
		// zero terminator QuickTime allows at the end of a udta list.
		if end-start < 8 {
			break
		}
		if len(atoms) >= 100000 {
			return atoms, errors.New("invalid media atom list")
		}
		var header [16]byte
		if _, err := r.ReadAt(header[:8], start); err != nil {
			return atoms, err
		}
		size, headerSize := int64(binary.BigEndian.Uint32(header[:4])), int64(8)
		if size == 1 {
			if end-start < 16 {
				return atoms, errors.New("truncated extended media atom")
			}
			if _, err := r.ReadAt(header[8:], start+8); err != nil {
				return atoms, err
			}
			extended := binary.BigEndian.Uint64(header[8:])
			if extended > uint64(end-start) {
				return atoms, errors.New("media atom exceeds file")
			}
			size, headerSize = int64(extended), 16
		} else if size == 0 {
			size = end - start
		}
		if size < headerSize || size > end-start {
			return atoms, errors.New("invalid media atom size")
		}
		atoms = append(atoms, mediaAtom{string(header[4:8]), start + headerSize, start + size})
		start += size
	}
	return atoms, nil
}

func atomPayload(r io.ReaderAt, a mediaAtom) ([]byte, error) {
	if a.start < 0 || a.end < a.start || a.end-a.start > maxAppleTranscriptSize {
		return nil, errors.New("Apple transcript metadata is too large")
	}
	data := make([]byte, a.end-a.start)
	_, err := r.ReadAt(data, a.start)
	return data, err
}

// findAppleTranscript searches every atom before reporting an error, so one
// malformed atom never hides a valid transcript elsewhere in the file.
func findAppleTranscript(r io.ReaderAt, start, end int64, depth int) (string, error) {
	if depth > 16 {
		return "", errors.New("media atoms nested too deeply")
	}
	atoms, firstErr := mediaAtoms(r, start, end)
	for _, a := range atoms {
		var err error
		var text string
		switch a.kind {
		case "tsrp":
			var data []byte
			if data, err = atomPayload(r, a); err == nil {
				text, err = decodeAppleTranscript(data)
			}
		case "moov", "trak", "udta":
			text, err = findAppleTranscript(r, a.start, a.end, depth+1)
		case "meta":
			var children []mediaAtom
			if children, err = mediaAtoms(r, metaChildrenStart(r, a), a.end); len(children) > 0 {
				var keyedErr error
				if text, keyedErr = keyedAppleTranscript(r, children); keyedErr != nil {
					err = keyedErr
				}
			}
		}
		if text != "" {
			return text, nil
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return "", firstErr
}

// metaChildrenStart skips the four version/flag bytes of an ISO meta atom.
// QuickTime meta omits them and starts with a child's nonzero size instead.
func metaChildrenStart(r io.ReaderAt, a mediaAtom) int64 {
	var prefix [4]byte
	if a.end-a.start >= 4 {
		if _, err := r.ReadAt(prefix[:], a.start); err == nil && prefix == [4]byte{} {
			return a.start + 4
		}
	}
	return a.start
}

func keyedAppleTranscript(r io.ReaderAt, atoms []mediaAtom) (string, error) {
	keys := make(map[uint32]bool)
	for _, a := range atoms {
		if a.kind != "keys" {
			continue
		}
		data, err := atomPayload(r, a)
		if err != nil {
			return "", err
		}
		if len(data) < 8 {
			return "", errors.New("truncated metadata keys")
		}
		count := binary.BigEndian.Uint32(data[4:8])
		data = data[8:]
		for i := uint32(0); i < count; i++ {
			if len(data) < 8 {
				return "", errors.New("truncated metadata key")
			}
			size := binary.BigEndian.Uint32(data[:4])
			if size < 8 || uint64(size) > uint64(len(data)) {
				return "", errors.New("invalid metadata key")
			}
			if string(data[4:8]) == "mdta" && string(data[8:size]) == "com.apple.VoiceMemos.tsrp" {
				keys[i+1] = true
			}
			data = data[size:]
		}
	}
	for _, a := range atoms {
		if a.kind != "ilst" || len(keys) == 0 {
			continue
		}
		items, itemsErr := mediaAtoms(r, a.start, a.end)
		for _, item := range items {
			if !keys[binary.BigEndian.Uint32([]byte(item.kind))] {
				continue
			}
			values, valuesErr := mediaAtoms(r, item.start, item.end)
			for _, value := range values {
				if value.kind != "data" {
					continue
				}
				// A data atom prefixes its payload with type and locale fields.
				if value.end-value.start < 8 {
					return "", errors.New("truncated transcript data")
				}
				value.start += 8
				data, err := atomPayload(r, value)
				if err != nil {
					return "", err
				}
				return decodeAppleTranscript(data)
			}
			if valuesErr != nil {
				return "", valuesErr
			}
		}
		if itemsErr != nil {
			return "", itemsErr
		}
	}
	return "", nil
}

// decodeAppleTranscript joins the text runs of Apple's attributed string.
// Each run is paired either with its attributes inline or with an index into
// a shared attributeTable.
func decodeAppleTranscript(data []byte) (string, error) {
	var document struct {
		AttributedString json.RawMessage `json:"attributedString"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return "", err
	}
	raw := document.AttributedString
	if len(raw) == 0 {
		return "", errors.New("Apple transcript has no attributedString")
	}
	var runs []json.RawMessage
	indexed := raw[0] == '{'
	var attributes []json.RawMessage
	if indexed {
		var value struct {
			Runs       []json.RawMessage `json:"runs"`
			Attributes []json.RawMessage `json:"attributeTable"`
		}
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", err
		}
		runs, attributes = value.Runs, value.Attributes
	} else if err := json.Unmarshal(raw, &runs); err != nil {
		return "", err
	}
	if len(runs)%2 != 0 {
		return "", errors.New("unpaired Apple transcript runs")
	}
	var text strings.Builder
	for i := 0; i < len(runs); i += 2 {
		var word string
		if err := json.Unmarshal(runs[i], &word); err != nil {
			return "", err
		}
		if indexed {
			var index int
			if err := json.Unmarshal(runs[i+1], &index); err != nil || index < 0 || index >= len(attributes) {
				return "", fmt.Errorf("invalid Apple transcript attribute at run %d", i)
			}
		} else {
			var attribute map[string]json.RawMessage
			if err := json.Unmarshal(runs[i+1], &attribute); err != nil || attribute == nil {
				return "", errors.New("invalid Apple transcript attributes")
			}
		}
		text.WriteString(word)
	}
	return strings.TrimSpace(text.String()), nil
}
