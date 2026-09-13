package v8go

// #include <stdlib.h>
// #include "snapshot.h"
import "C"

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"unsafe"
)

const snapshotMagic = "V8GOSNP1"

// ErrSnapshot identifies invalid snapshot envelopes and snapshot API misuse.
var ErrSnapshot = errors.New("v8go: invalid snapshot")

type snapshotEnvelope struct {
	Version     string   `json:"version"`
	OS          string   `json:"os"`
	Arch        string   `json:"arch"`
	Tag         uint32   `json:"tag"`
	SourceHash  string   `json:"source_sha256"`
	Exports     []string `json:"exports"`
	PayloadHash string   `json:"payload_sha256"`
}

func snapshotHash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// CreateSnapshot builds a trusted local library artifact. Source executes only
// in an additional context, exports are saved in order and deleted from its
// global object, and the default context remains clean. No Go callbacks or
// request data may be present in source. Function code is cleared in the blob.
func CreateSnapshot(source string, exportNames []string) ([]byte, error) {
	if source == "" || strings.ContainsRune(source, 0) || len(source) > 1<<30 || len(exportNames) == 0 {
		return nil, fmt.Errorf("%w: empty, oversized, or NUL-containing source or empty exports", ErrSnapshot)
	}
	seen := make(map[string]bool, len(exportNames))
	names := make([]*C.char, len(exportNames))
	for i, name := range exportNames {
		if name == "" || strings.ContainsRune(name, 0) || seen[name] {
			return nil, fmt.Errorf("%w: invalid or duplicate export name", ErrSnapshot)
		}
		seen[name] = true
		names[i] = C.CString(name)
		defer C.free(unsafe.Pointer(names[i]))
	}
	initializeIfNecessary()
	csource := C.CString(source)
	defer C.free(unsafe.Pointer(csource))
	rtn := C.CreateLibrarySnapshot(csource, &names[0], C.int(len(names)))
	if rtn.data == nil {
		if rtn.error.msg != nil {
			return nil, fmt.Errorf("creating snapshot: %w", newJSError(rtn.error))
		}
		return nil, fmt.Errorf("%w: native builder returned no data", ErrSnapshot)
	}
	defer C.FreeLibrarySnapshot(rtn.data)
	payload := C.GoBytes(unsafe.Pointer(rtn.data), rtn.length)
	metadata := snapshotEnvelope{Version: Version(), OS: runtime.GOOS, Arch: runtime.GOARCH,
		Tag: uint32(C.SnapshotCompatibilityTag()), SourceHash: snapshotHash([]byte(source)),
		Exports: append([]string(nil), exportNames...), PayloadHash: snapshotHash(payload)}
	header, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("encoding snapshot envelope: %w", err)
	}
	blob := make([]byte, len(snapshotMagic)+4, len(snapshotMagic)+4+len(header)+len(payload)+sha256.Size)
	copy(blob, snapshotMagic)
	binary.BigEndian.PutUint32(blob[len(snapshotMagic):], uint32(len(header)))
	blob = append(blob, header...)
	blob = append(blob, payload...)
	sum := sha256.Sum256(blob)
	return append(blob, sum[:]...), nil
}

func readSnapshot(blob []byte) (snapshotEnvelope, []byte, error) {
	var meta snapshotEnvelope
	invalid := func(reason string) (snapshotEnvelope, []byte, error) {
		return meta, nil, fmt.Errorf("%w: %s", ErrSnapshot, reason)
	}
	if len(blob) < len(snapshotMagic)+4+sha256.Size || string(blob[:len(snapshotMagic)]) != snapshotMagic {
		return invalid("bad envelope magic or length")
	}
	body := blob[:len(blob)-sha256.Size]
	sum := sha256.Sum256(body)
	if !bytes.Equal(sum[:], blob[len(body):]) {
		return invalid("envelope checksum mismatch")
	}
	headerSize := uint64(binary.BigEndian.Uint32(blob[len(snapshotMagic):]))
	start := len(snapshotMagic) + 4
	if headerSize == 0 || headerSize > 1<<20 || headerSize >= uint64(len(body)-start) {
		return invalid("bad metadata length")
	}
	end := start + int(headerSize)
	decoder := json.NewDecoder(bytes.NewReader(body[start:end]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&meta); err != nil {
		return invalid("bad metadata")
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return invalid("trailing metadata")
	}
	initializeIfNecessary()
	if meta.Version != Version() || meta.OS != runtime.GOOS || meta.Arch != runtime.GOARCH || meta.Tag != uint32(C.SnapshotCompatibilityTag()) {
		return invalid("incompatible V8 version, platform, flags or CPU")
	}
	if hash, err := hex.DecodeString(meta.SourceHash); err != nil || len(hash) != sha256.Size {
		return invalid("bad source identity")
	}
	if len(meta.Exports) == 0 {
		return invalid("missing exports")
	}
	seen := make(map[string]bool, len(meta.Exports))
	for _, name := range meta.Exports {
		if name == "" || strings.ContainsRune(name, 0) || seen[name] {
			return invalid("bad export names")
		}
		seen[name] = true
	}
	payload := body[end:]
	if len(payload) < 64 || len(payload) > 1<<30 || snapshotHash(payload) != meta.PayloadHash {
		return invalid("bad native payload length or checksum")
	}
	return meta, payload, nil
}

// SnapshotSourceHash validates the envelope and returns the exact source SHA256.
// Callers must compare it with the expected library source identity before use.
func SnapshotSourceHash(blob []byte) (string, error) {
	meta, _, err := readSnapshot(blob)
	if err != nil {
		return "", err
	}
	return meta.SourceHash, nil
}

// NewIsolateWithSnapshot accepts only trusted local artifacts from CreateSnapshot.
// Checksums detect corruption; they do not authenticate a producer or make V8
// deserialization safe for attacker-controlled input. Native bytes are copied
// and owned until Dispose. V8 flags must match the builder and remain unchanged.
func NewIsolateWithSnapshot(blob []byte) (*Isolate, error) {
	meta, payload, err := readSnapshot(blob)
	if err != nil {
		return nil, err
	}
	ptr := C.NewSnapshotIsolate((*C.char)(unsafe.Pointer(&payload[0])), C.int(len(payload)))
	runtime.KeepAlive(blob)
	if ptr == nil {
		return nil, fmt.Errorf("%w: V8 rejected snapshot", ErrSnapshot)
	}
	iso := &Isolate{ptr: ptr, cbs: make(map[int]FunctionCallback), snapshotExports: len(meta.Exports)}
	iso.null = newValueNull(iso)
	iso.undefined = newValueUndefined(iso)
	return iso, nil
}

// NewContextFromSnapshot restores an independent library context and instantiates
// the supplied global template in that context, including fresh Go callbacks.
// Global templates with nonzero internal field counts are unsupported.
// Close the returned context before disposing its isolate.
func NewContextFromSnapshot(iso *Isolate, global *ObjectTemplate) (*Context, error) {
	if iso == nil || iso.ptr == nil || iso.snapshotExports == 0 {
		return nil, fmt.Errorf("%w: missing snapshot isolate", ErrSnapshot)
	}
	var templatePtr C.TemplatePtr
	if global != nil {
		if global.template == nil || global.ptr == nil || global.iso != iso {
			return nil, fmt.Errorf("%w: invalid global template or different isolate", ErrSnapshot)
		}
		if global.InternalFieldCount() != 0 {
			return nil, fmt.Errorf("%w: global template internal fields are unsupported", ErrSnapshot)
		}
		templatePtr = global.ptr
	}
	ctxMutex.Lock()
	ctxSeq++
	ref := ctxSeq
	ctxMutex.Unlock()
	rtn := C.NewLibraryContext(iso.ptr, templatePtr, C.int(ref))
	runtime.KeepAlive(global)
	if rtn.ptr == nil {
		if rtn.error.msg != nil {
			return nil, fmt.Errorf("restoring snapshot context: %w", newJSError(rtn.error))
		}
		return nil, fmt.Errorf("%w: V8 rejected library context", ErrSnapshot)
	}
	ctx := &Context{ref: ref, ptr: rtn.ptr, iso: iso, snapshotData: make([]bool, iso.snapshotExports)}
	ctx.register()
	return ctx, nil
}

// SnapshotData retrieves an export once per restored context. The value is
// tracked by the context and becomes invalid when that context closes.
func (c *Context) SnapshotData(index int) (*Value, error) {
	if c == nil || c.ptr == nil || c.iso.ptr == nil || index < 0 || index >= len(c.snapshotData) || c.snapshotData[index] {
		return nil, fmt.Errorf("%w: invalid or consumed export index", ErrSnapshot)
	}
	rtn := C.LibraryContextData(c.ptr, C.int(index))
	value, err := valueResult(c, rtn)
	if err != nil {
		return nil, fmt.Errorf("retrieving snapshot data: %w", err)
	}
	c.snapshotData[index] = true
	return value, nil
}
