package v8go

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const snapshotTestSource = `globalThis.library = (() => { let count = 0; return {next: () => ++count, proto: Object.prototype, fresh: () => ({}), call: () => sentinel()}; })();`

// Separate builder and consumer processes catch dependencies on builder-local
// pointers, Go callbacks, and source text retained only in process memory.
func TestSnapshotCrossProcess(t *testing.T) {
	if mode := os.Getenv("V8GO_SNAPSHOT_CHILD"); mode != "" {
		path := os.Getenv("V8GO_SNAPSHOT_PATH")
		if mode == "build" {
			blob, err := CreateSnapshot(snapshotTestSource, []string{"library"})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, blob, 0600); err != nil {
				t.Fatal(err)
			}
			return
		}
		blob, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if mode == "flags" {
			SetFlags("--use_strict")
			if iso, err := NewIsolateWithSnapshot(blob); !errors.Is(err, ErrSnapshot) {
				if iso != nil {
					iso.Dispose()
				}
				t.Fatalf("mismatched V8 flags: got %v, want ErrSnapshot", err)
			}
			return
		}
		iso, err := NewIsolateWithSnapshot(blob)
		if err != nil {
			t.Fatal(err)
		}
		defer iso.Dispose()
		// The native isolate must own its snapshot bytes, independent of caller memory.
		for i := range blob {
			blob[i] = 0
		}
		clean := NewContext(iso)
		snapshotWant(t, clean, `typeof library`, "undefined")
		clean.Close()
		first := snapshotTestContext(t, iso, "first")
		second := snapshotTestContext(t, iso, "second")
		snapshotWant(t, first, `lib.next() + ":" + lib.next()`, "1:2")
		snapshotWant(t, first, `lib.proto.leak = 19; lib.call()`, "first")
		snapshotWant(t, second, `lib.next() + ":" + typeof lib.fresh().leak + ":" + lib.call()`, "1:undefined:second")
		snapshotWant(t, second, `Object.getOwnPropertyDescriptor(globalThis, "fixed").writable + ":" + Object.getOwnPropertyDescriptor(globalThis, "fixed").enumerable + ":" + Object.getOwnPropertyDescriptor(globalThis, "fixed").configurable`, "false:false:false")
		snapshotWant(t, first, `try { failure() } catch (e) { e }`, "callback-failure")
		snapshotWant(t, first, `globalThis.task = "pending"; Promise.resolve().then(() => task = sentinel()); task`, "pending")
		first.PerformMicrotaskCheckpoint()
		snapshotWant(t, first, `task`, "first")
		first.Close()
		snapshotWant(t, second, `lib.next() + ":" + lib.call()`, "2:second")
		second.Close()
		ctxMutex.RLock()
		remaining := len(ctxRegistry)
		ctxMutex.RUnlock()
		if remaining != 0 {
			t.Fatalf("context registry retained %d contexts", remaining)
		}
		return
	}
	path := filepath.Join(t.TempDir(), "library.snapshot")
	for _, mode := range []string{"build", "restore", "flags"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestSnapshotCrossProcess$", "-test.v")
		cmd.Env = append(os.Environ(), "V8GO_SNAPSHOT_CHILD="+mode, "V8GO_SNAPSHOT_PATH="+path)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s process: %v\n%s", mode, err, out)
		}
		t.Logf("%s process:\n%s", mode, out)
	}
}

// A malformed but checksummed header must fail before V8 sees native bytes.
// Mutations exercise version/architecture/tag/source/payload identity separately.
func TestSnapshotEnvelope(t *testing.T) {
	blob, err := CreateSnapshot(snapshotTestSource, []string{"library"})
	if err != nil {
		t.Fatal(err)
	}
	sourceHash, err := SnapshotSourceHash(blob)
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("%x", sha256.Sum256([]byte(snapshotTestSource))); sourceHash != want {
		t.Fatalf("source hash %s, want %s", sourceHash, want)
	}
	headerStart := len(snapshotMagic) + 4
	headerEnd := headerStart + int(binary.BigEndian.Uint32(blob[len(snapshotMagic):]))
	bodyEnd := len(blob) - sha256.Size
	for _, mutation := range []string{"trailing-json", "version", "arch", "tag", "source", "payload", "empty-payload"} {
		t.Run(mutation, func(t *testing.T) {
			var metadata map[string]interface{}
			if err := json.Unmarshal(blob[headerStart:headerEnd], &metadata); err != nil {
				t.Fatal(err)
			}
			payload := append([]byte(nil), blob[headerEnd:bodyEnd]...)
			switch mutation {
			case "version":
				metadata["version"] = "different-engine"
			case "arch":
				metadata["arch"] = "different-architecture"
			case "tag":
				metadata["tag"] = float64(uint32(metadata["tag"].(float64)) ^ 1)
			case "source":
				metadata["source_sha256"] = "invalid"
			case "payload":
				payload[len(payload)/2] ^= 1
			case "empty-payload":
				payload = nil
			}
			header, err := json.Marshal(metadata)
			if err != nil {
				t.Fatal(err)
			}
			if mutation == "trailing-json" {
				header = append(header, []byte(" {}")...)
			}
			changed := append([]byte(nil), blob[:headerStart]...)
			binary.BigEndian.PutUint32(changed[len(snapshotMagic):], uint32(len(header)))
			changed = append(changed, header...)
			changed = append(changed, payload...)
			sum := sha256.Sum256(changed)
			changed = append(changed, sum[:]...)
			if iso, err := NewIsolateWithSnapshot(changed); !errors.Is(err, ErrSnapshot) {
				if iso != nil {
					iso.Dispose()
				}
				t.Fatalf("malformed envelope: got %v, want ErrSnapshot", err)
			}
		})
	}
}

func TestSnapshotRejectsGlobalInternalFields(t *testing.T) {
	blob, err := CreateSnapshot(snapshotTestSource, []string{"library"})
	if err != nil {
		t.Fatal(err)
	}
	iso, err := NewIsolateWithSnapshot(blob)
	if err != nil {
		t.Fatal(err)
	}
	defer iso.Dispose()
	global := NewObjectTemplate(iso)
	global.SetInternalFieldCount(1)
	ctxMutex.RLock()
	before := len(ctxRegistry)
	ctxMutex.RUnlock()

	ctx, err := NewContextFromSnapshot(iso, global)
	if ctx != nil {
		defer ctx.Close()
		t.Error("template with internal fields returned a context")
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Errorf("template with internal fields: got %v, want ErrSnapshot", err)
	}
	ctxMutex.RLock()
	after := len(ctxRegistry)
	ctxMutex.RUnlock()
	if after != before {
		t.Errorf("rejected template changed context registry size: got %d, want %d", after, before)
	}
}

func TestSnapshotAPIGuards(t *testing.T) {
	blob, err := CreateSnapshot(snapshotTestSource, []string{"library"})
	if err != nil {
		t.Fatal(err)
	}
	iso, err := NewIsolateWithSnapshot(blob)
	if err != nil {
		t.Fatal(err)
	}
	defer iso.Dispose()
	other := NewIsolate()
	defer other.Dispose()
	if _, err := NewContextFromSnapshot(iso, NewObjectTemplate(other)); !errors.Is(err, ErrSnapshot) {
		t.Fatalf("foreign template: %v", err)
	}
	ctx, err := NewContextFromSnapshot(iso, nil)
	if err != nil {
		t.Fatal(err)
	}
	value, err := ctx.SnapshotData(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := ctx.Global().Set("lib", value); err != nil {
		t.Fatal(err)
	}
	snapshotWant(t, ctx, "lib.next()", "1")
	ctx.Close()
	if _, err := ctx.SnapshotData(0); !errors.Is(err, ErrSnapshot) {
		t.Fatalf("closed context: %v", err)
	}
	clean := NewContext(iso)
	if _, err := clean.SnapshotData(0); !errors.Is(err, ErrSnapshot) {
		t.Fatalf("ordinary context: %v", err)
	}
	snapshotWant(t, clean, "typeof library", "undefined")
	clean.Close()
	iso.Dispose()
	if _, err := NewContextFromSnapshot(iso, nil); !errors.Is(err, ErrSnapshot) {
		t.Fatalf("disposed isolate: %v", err)
	}
}

// Adding restored template properties after V8's late builtins changes observable
// Reflect.ownKeys ordering. Also catch overwriting template/builtin collisions.
func TestSnapshotGlobalTemplateOrder(t *testing.T) {
	blob, err := CreateSnapshot(snapshotTestSource, []string{"library"})
	if err != nil {
		t.Fatal(err)
	}
	for _, collision := range []string{"", "SharedArrayBuffer", "Atomics", "WebAssembly", "JSON", "Math", "Temporal", "Float16Array"} {
		t.Run("collision="+collision, func(t *testing.T) {
			iso, err := NewIsolateWithSnapshot(blob)
			if err != nil {
				t.Fatal(err)
			}
			defer iso.Dispose()
			global := NewObjectTemplate(iso)
			if err := global.Set("first", "one"); err != nil {
				t.Fatal(err)
			}
			if collision != "" {
				if err := global.Set(collision, "collision-value"); err != nil {
					t.Fatal(err)
				}
			}
			if err := global.Set("last", "two"); err != nil {
				t.Fatal(err)
			}
			ordinary := NewContext(iso, global)
			defer ordinary.Close()
			restored, err := NewContextFromSnapshot(iso, global)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			source := `JSON.stringify(Reflect.ownKeys(globalThis).map(k => { const d = Object.getOwnPropertyDescriptor(globalThis, k); return [String(k), typeof d.value, d.writable, d.enumerable, d.configurable, typeof d.value === 'string' ? d.value : null]; }))`
			want, err := ordinary.RunScript(source, "ordinary.js")
			if err != nil {
				t.Fatal(err)
			}
			got, err := restored.RunScript(source, "restored.js")
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != want.String() {
				t.Fatalf("restored global order/descriptors differ:\nwant %s\ngot  %s", want, got)
			}
		})
	}
}

func snapshotTestContext(t *testing.T, iso *Isolate, sentinel string) *Context {
	t.Helper()
	var ctx *Context
	global := NewObjectTemplate(iso)
	callback := NewFunctionTemplate(iso, func(info *FunctionCallbackInfo) *Value {
		if info.Context() != ctx {
			t.Errorf("callback got wrong context: %p, want %p", info.Context(), ctx)
		}
		v, err := NewValue(iso, sentinel)
		if err != nil {
			t.Error(err)
			return nil
		}
		return v
	})
	if err := global.Set("sentinel", callback); err != nil {
		t.Fatal(err)
	}
	// Upstream named constants start one bit too high; use V8's actual flags.
	if err := global.Set("fixed", "locked", PropertyAttribute(7)); err != nil {
		t.Fatal(err)
	}
	failure := NewFunctionTemplate(iso, func(info *FunctionCallbackInfo) *Value {
		if info.Context() != ctx {
			t.Error("error callback has wrong context")
		}
		value, err := NewValue(iso, "callback-failure")
		if err != nil {
			t.Error(err)
			return nil
		}
		return iso.ThrowException(value)
	})
	if err := global.Set("failure", failure); err != nil {
		t.Fatal(err)
	}
	var err error
	ctx, err = NewContextFromSnapshot(iso, global)
	if err != nil {
		t.Fatal(err)
	}
	snapshotWant(t, ctx, `typeof library`, "undefined")
	lib, err := ctx.SnapshotData(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := ctx.Global().Set("lib", lib); err != nil {
		t.Fatal(err)
	}
	if _, err := ctx.SnapshotData(0); err == nil {
		t.Error("repeated snapshot data retrieval succeeded")
	}
	for _, index := range []int{-1, 1, 1 << 30} {
		if _, err := ctx.SnapshotData(index); err == nil {
			t.Errorf("invalid index %d succeeded", index)
		}
	}
	return ctx
}

func snapshotWant(t *testing.T, ctx *Context, source, want string) {
	t.Helper()
	value, err := ctx.RunScript(source, "snapshot-contract.js")
	if err != nil {
		t.Fatal(err)
	}
	if got := value.String(); got != want {
		t.Fatalf("%s: got %q, want %q", source, got, want)
	}
}

func TestSnapshotInvalidInput(t *testing.T) {
	for _, blob := range [][]byte{nil, {}, {0}, []byte("not a snapshot")} {
		if iso, err := NewIsolateWithSnapshot(blob); err == nil {
			iso.Dispose()
			t.Fatal("malformed envelope accepted")
		}
	}
	for _, tc := range []struct {
		source string
		names  []string
	}{
		{"", []string{"library"}}, {"syntax ???", []string{"library"}},
		{"throw new Error('builder failure')", []string{"library"}},
		{"globalThis.library = 1", []string{"missing"}},
		{"Object.defineProperty(globalThis, 'library', {value: 1})", []string{"library"}},
		{snapshotTestSource, []string{"library", "library"}},
	} {
		if _, err := CreateSnapshot(tc.source, tc.names); err == nil {
			t.Errorf("invalid builder input accepted: %+v", tc)
		}
	}
	if _, err := NewContextFromSnapshot(nil, nil); err == nil {
		t.Fatal("nil isolate accepted")
	}
	plain := NewIsolate()
	defer plain.Dispose()
	if _, err := NewContextFromSnapshot(plain, nil); err == nil {
		t.Fatal("ordinary isolate accepted for snapshot restore")
	}
}
