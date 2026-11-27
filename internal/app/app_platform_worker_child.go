//go:build !leafen_noapps

package app

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"slices"
	"time"

	extism "github.com/extism/go-sdk"
	"github.com/tetratelabs/wazero"
)

// The app worker (docs/app-platform.md 11): a child of the server, started
// as `leafen app-worker` with an empty environment in an empty folder, that
// runs an app's WebAssembly with the Extism calling convention. It speaks
// only through length-prefixed JSON on its standard input and output: the
// parent sends a call, the module may ask the parent for host functions
// while it runs, and the child answers with the result. WASI gives the
// module a clock and random numbers — no files, network or environment —
// and Extism's own outside requests are refused; memory, time and output
// are bounded per call.

const (
	appWorkerMaxFrameBytes = 24 << 20
	appWorkerCachedModules = 8
)

type appWorkerMessage struct {
	Op          string `json:"op"`
	ID          int64  `json:"id,omitempty"`
	HostID      int64  `json:"hostId,omitzero"`
	Module      string `json:"module,omitempty"`
	Wasm        []byte `json:"wasm,omitempty"`
	Function    string `json:"function,omitempty"`
	Input       []byte `json:"input,omitempty"`
	TimeoutMs   int64  `json:"timeoutMs,omitzero"`
	MemoryPages uint32 `json:"memoryPages,omitzero"`
	MaxOutput   int    `json:"maxOutput,omitzero"`
	Name        string `json:"name,omitempty"`
	Payload     []byte `json:"payload,omitempty"`
	Result      []byte `json:"result,omitempty"`
	Error       string `json:"error,omitempty"`
	// End: the host's refusal is the run's bound passed, which ends the
	// run; any other refusal is the module's to handle.
	End     bool `json:"end,omitempty"`
	Missing bool `json:"missing,omitempty"`
	// Refusal: the call's error is the module's own, the reason it gave
	// (appWorkerRefusal).
	Refusal bool `json:"refusal,omitzero"`
	// PastLimit: the call's time ended it (appWorkerPastLimit).
	PastLimit bool `json:"pastLimit,omitzero"`
	// Long marks a long-running worker's call, which may run up to an hour.
	Long bool `json:"long,omitzero"`
	// Resident names the instance that stays for the next call with the
	// same name, rather than a new one for each call.
	Resident string `json:"resident,omitempty"`
}

func writeAppWorkerFrame(writer io.Writer, message appWorkerMessage) error {
	encoded, err := json.Marshal(message)
	if err != nil {
		return err
	}
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(encoded)))
	if _, err := writer.Write(header); err != nil {
		return err
	}
	_, err = writer.Write(encoded)
	return err
}

func readAppWorkerFrame(reader io.Reader) (appWorkerMessage, error) {
	message := appWorkerMessage{}
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return message, err
	}
	size := binary.BigEndian.Uint32(header)
	if size > appWorkerMaxFrameBytes {
		return message, fmt.Errorf("a frame of %d bytes", size)
	}
	encoded := make([]byte, size)
	if _, err := io.ReadFull(reader, encoded); err != nil {
		return message, err
	}
	return message, json.Unmarshal(encoded, &message)
}

// The host functions a module may call, each answered by the parent.
var appWorkerHostFunctions = []string{"leafen_log", "leafen_api", "leafen_relay", "leafen_tenancies", "leafen_kv", "leafen_notify", "leafen_files_join",
	"leafen_socket_open", "leafen_socket_write", "leafen_socket_read", "leafen_socket_close", "leafen_wait", "leafen_room_next", "leafen_room_send"}

type appWorkerChild struct {
	in      *bufio.Reader
	out     *bufio.Writer
	modules map[string]*extism.CompiledPlugin
	order   []string
	hostID  int64
	callID  int64
	// Instances kept for their next calls, the oldest first.
	resident      map[string]appWorkerResident
	residentOrder []string
}

// appWorkerResident is an instance kept between calls, with the module it
// was made from.
type appWorkerResident struct {
	module string
	plugin *extism.Plugin
}

// appWorkerResidentKept is how many instances a worker process keeps.
const appWorkerResidentKept = 4

// RunAppWorker is the whole of the `leafen app-worker` process: it narrows
// itself, then serves calls until its input closes.
func RunAppWorker(stdin io.Reader, stdout io.Writer) int {
	os.Clearenv()
	limitAppWorkerProcess()
	// A call's memory may be up to 256 MB; the rest is the worker's own.
	debug.SetMemoryLimit(320 << 20)
	return serveAppWorker(stdin, stdout)
}

func serveAppWorker(stdin io.Reader, stdout io.Writer) int {
	child := &appWorkerChild{in: bufio.NewReader(stdin), out: bufio.NewWriter(stdout), modules: map[string]*extism.CompiledPlugin{},
		resident: map[string]appWorkerResident{}}
	for {
		message, err := readAppWorkerFrame(child.in)
		if errors.Is(err, io.EOF) {
			return 0
		}
		if err != nil {
			return 2
		}
		if message.Op != "call" {
			continue
		}
		output, callErr := child.call(message)
		reply := appWorkerMessage{Op: "result", ID: message.ID, Result: output}
		if callErr != nil {
			reply.Error = callErr.Error()
			reply.Missing = errors.Is(callErr, errAppWorkerModuleMissing)
			reply.Refusal = errors.As(callErr, new(appWorkerRefusal))
			reply.PastLimit = errors.As(callErr, new(appWorkerPastLimit))
		}
		if err := writeAppWorkerFrame(child.out, reply); err != nil || child.out.Flush() != nil {
			return 2
		}
	}
}

var errAppWorkerModuleMissing = errors.New("module not loaded")

func (child *appWorkerChild) call(message appWorkerMessage) ([]byte, error) {
	child.callID = message.ID
	pages := message.MemoryPages
	if pages == 0 || pages > 4096 {
		pages = 1024
	}
	timeout, longest := time.Duration(message.TimeoutMs)*time.Millisecond, 60*time.Second
	if message.Long {
		longest = appLongRunLifetime
	}
	if timeout <= 0 || timeout > longest {
		timeout = 10 * time.Second
	}
	compiled, ok := child.modules[message.Module]
	if !ok {
		if len(message.Wasm) == 0 {
			return nil, errAppWorkerModuleMissing
		}
		// Compiling isn't the call's time: a large module on a busy machine took
		// longer than the limit, so an app's first call failed right after it
		// was installed (2026-11-05).
		compiling, compileDone := context.WithTimeout(context.Background(), appWorkerCompileLimit)
		defer compileDone()
		var err error
		compiled, err = extism.NewCompiledPlugin(compiling, extism.Manifest{
			Wasm:   []extism.Wasm{extism.WasmData{Data: message.Wasm}},
			Memory: &extism.ManifestMemory{MaxPages: pages, MaxHttpResponseBytes: 0, MaxVarBytes: 1 << 20},
		}, extism.PluginConfig{
			EnableWasi:    true,
			RuntimeConfig: wazero.NewRuntimeConfig().WithCloseOnContextDone(true).WithMemoryLimitPages(pages),
		}, child.hostFunctions())
		if err != nil {
			return nil, fmt.Errorf("the module doesn't load: %w", err)
		}
		child.modules[message.Module] = compiled
		child.order = append(child.order, message.Module)
		if len(child.order) > appWorkerCachedModules {
			oldest := child.order[0]
			child.order = child.order[1:]
			_ = child.modules[oldest].Close(context.Background())
			delete(child.modules, oldest)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	plugin, kept, err := child.instance(ctx, compiled, message)
	if err != nil {
		return nil, fmt.Errorf("the module doesn't start: %w", err)
	}
	status, output, err := plugin.CallWithContext(ctx, message.Function, message.Input)
	// An error the module set itself (Fail) is its reason; a trap, an exit or
	// the time passing is the call's failure.
	if err != nil && ctx.Err() == nil {
		if reason := plugin.GetErrorWithContext(ctx); reason != "" && reason == err.Error() {
			err = appWorkerRefusal{Reason: reason}
		}
	}
	// A kept instance goes on only after a call that ended well.
	if !kept || err != nil || status != 0 || ctx.Err() != nil {
		child.forget(message.Resident, plugin, kept)
	}
	if ctx.Err() != nil {
		return nil, appWorkerPastLimit{Text: fmt.Sprintf("the call ran past %s", timeout)}
	}
	if err != nil {
		return nil, err
	}
	if status != 0 {
		return nil, fmt.Errorf("the call ended with %d", status)
	}
	if message.MaxOutput > 0 && len(output) > message.MaxOutput {
		return nil, fmt.Errorf("the output is larger than %d bytes", message.MaxOutput)
	}
	return output, nil
}

// hostFunctions pass each host call to the parent and wait for its answer.
// The parent's refusal (no workspace for the organization, a request it
// doesn't take, the native API's error) reaches the module as
// {"error": "…"}, which each SDK gives as an error; the run's bounds passed
// (its host calls) and a worker that can't read, ask or write any more end
// the run.
func (child *appWorkerChild) hostFunctions() []extism.HostFunction {
	functions := []extism.HostFunction{}
	for _, name := range appWorkerHostFunctions {
		functions = append(functions, extism.NewHostFunctionWithStack(name,
			func(_ context.Context, plugin *extism.CurrentPlugin, stack []uint64) {
				payload, err := plugin.ReadBytes(stack[0])
				if err != nil {
					panic(err)
				}
				answer, refusal, err := child.askParent(name, payload)
				if err != nil {
					panic(err)
				}
				if refusal != "" {
					answer, _ = json.Marshal(map[string]string{"error": refusal})
				}
				offset, err := plugin.WriteBytes(answer)
				if err != nil {
					panic(err)
				}
				stack[0] = offset
			}, []extism.ValueType{extism.ValueTypePTR}, []extism.ValueType{extism.ValueTypePTR}))
	}
	return functions
}

// askParent is the parent's answer to a host call, or its refusal.
func (child *appWorkerChild) askParent(name string, payload []byte) ([]byte, string, error) {
	child.hostID++
	if err := writeAppWorkerFrame(child.out, appWorkerMessage{Op: "host", ID: child.callID, HostID: child.hostID, Name: name, Payload: payload}); err != nil {
		return nil, "", err
	}
	if err := child.out.Flush(); err != nil {
		return nil, "", err
	}
	reply, err := readAppWorkerFrame(child.in)
	if err != nil {
		return nil, "", err
	}
	if reply.Op != "host_reply" || reply.HostID != child.hostID {
		return nil, "", errors.New("the parent answered out of turn")
	}
	switch {
	case reply.Error != "" && reply.End:
		return nil, "", errors.New(reply.Error)
	case reply.Error != "":
		return nil, reply.Error, nil
	}
	return reply.Result, "", nil
}

// instance is the call's instance: the one kept under its resident name
// for this module, or a new one — kept when the call names one.
func (child *appWorkerChild) instance(ctx context.Context, compiled *extism.CompiledPlugin, message appWorkerMessage) (*extism.Plugin, bool, error) {
	if kept, ok := child.resident[message.Resident]; ok && message.Resident != "" {
		if kept.module == message.Module {
			return kept.plugin, true, nil
		}
		child.forget(message.Resident, kept.plugin, true)
	}
	// The clocks and random numbers WASI gives a worker are the real ones:
	// wazero's defaults are a fixed date and a fixed sequence, which would
	// date every run alike and repeat the ids workers make.
	plugin, err := compiled.Instance(ctx, extism.PluginInstanceConfig{ModuleConfig: wazero.NewModuleConfig().
		WithSysWalltime().WithSysNanotime().WithSysNanosleep().WithRandSource(rand.Reader)})
	if err != nil || message.Resident == "" {
		return plugin, false, err
	}
	child.resident[message.Resident] = appWorkerResident{module: message.Module, plugin: plugin}
	child.residentOrder = append(child.residentOrder, message.Resident)
	if len(child.residentOrder) > appWorkerResidentKept {
		oldest := child.residentOrder[0]
		child.forget(oldest, child.resident[oldest].plugin, true)
	}
	return plugin, true, nil
}

// forget closes an instance, and stops keeping it if it was kept.
func (child *appWorkerChild) forget(name string, plugin *extism.Plugin, kept bool) {
	if plugin != nil {
		_ = plugin.Close(context.Background())
	}
	if !kept {
		return
	}
	delete(child.resident, name)
	child.residentOrder = slices.DeleteFunc(child.residentOrder, func(each string) bool { return each == name })
}
