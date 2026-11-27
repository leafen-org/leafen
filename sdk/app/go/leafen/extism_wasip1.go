//go:build wasip1

package leafen

// The Extism calling convention by hand, so a worker needs no other module.

//go:wasmimport extism:host/env input_length
func extismInputLength() uint64

//go:wasmimport extism:host/env input_load_u8
func extismInputLoadU8(offset uint64) uint32

//go:wasmimport extism:host/env input_load_u64
func extismInputLoadU64(offset uint64) uint64

//go:wasmimport extism:host/env alloc
func extismAlloc(length uint64) uint64

//go:wasmimport extism:host/env store_u8
func extismStoreU8(offset uint64, value uint32)

//go:wasmimport extism:host/env store_u64
func extismStoreU64(offset uint64, value uint64)

//go:wasmimport extism:host/env load_u8
func extismLoadU8(offset uint64) uint32

//go:wasmimport extism:host/env load_u64
func extismLoadU64(offset uint64) uint64

//go:wasmimport extism:host/env length
func extismLength(offset uint64) uint64

//go:wasmimport extism:host/env output_set
func extismOutputSet(offset, length uint64)

//go:wasmimport extism:host/env error_set
func extismErrorSet(offset uint64)

//go:wasmimport extism:host/user leafen_log
func leafenLog(offset uint64) uint64

//go:wasmimport extism:host/user leafen_api
func leafenAPI(offset uint64) uint64

//go:wasmimport extism:host/user leafen_relay
func leafenRelay(offset uint64) uint64

//go:wasmimport extism:host/user leafen_tenancies
func leafenTenancies(offset uint64) uint64

//go:wasmimport extism:host/user leafen_kv
func leafenKV(offset uint64) uint64

//go:wasmimport extism:host/user leafen_notify
func leafenNotify(offset uint64) uint64

//go:wasmimport extism:host/user leafen_files_join
func leafenFilesJoin(offset uint64) uint64

//go:wasmimport extism:host/user leafen_socket_open
func leafenSocketOpen(offset uint64) uint64

//go:wasmimport extism:host/user leafen_socket_write
func leafenSocketWrite(offset uint64) uint64

//go:wasmimport extism:host/user leafen_socket_read
func leafenSocketRead(offset uint64) uint64

//go:wasmimport extism:host/user leafen_socket_close
func leafenSocketClose(offset uint64) uint64

//go:wasmimport extism:host/user leafen_wait
func leafenWait(offset uint64) uint64

//go:wasmimport extism:host/user leafen_room_next
func leafenRoomNext(offset uint64) uint64

//go:wasmimport extism:host/user leafen_room_send
func leafenRoomSend(offset uint64) uint64

// Input returns the call's input.
func Input() []byte {
	size := extismInputLength()
	data := make([]byte, size)
	index := uint64(0)
	for ; index+8 <= size; index += 8 {
		word := extismInputLoadU64(index)
		for shift := uint64(0); shift < 8; shift++ {
			data[index+shift] = byte(word >> (8 * shift))
		}
	}
	for ; index < size; index++ {
		data[index] = byte(extismInputLoadU8(index))
	}
	return data
}

// Output sets the call's output.
func Output(data []byte) { extismOutputSet(store(data), uint64(len(data))) }

// Fail sets the call's error; return its result from the exported function.
func Fail(message string) int32 {
	extismErrorSet(store([]byte(message)))
	return 1
}

func store(data []byte) uint64 {
	size := uint64(len(data))
	offset := extismAlloc(size)
	index := uint64(0)
	for ; index+8 <= size; index += 8 {
		word := uint64(0)
		for shift := uint64(0); shift < 8; shift++ {
			word |= uint64(data[index+shift]) << (8 * shift)
		}
		extismStoreU64(offset+index, word)
	}
	for ; index < size; index++ {
		extismStoreU8(offset+index, uint32(data[index]))
	}
	return offset
}

func load(offset uint64) []byte {
	size := extismLength(offset)
	data := make([]byte, size)
	index := uint64(0)
	for ; index+8 <= size; index += 8 {
		word := extismLoadU64(offset + index)
		for shift := uint64(0); shift < 8; shift++ {
			data[index+shift] = byte(word >> (8 * shift))
		}
	}
	for ; index < size; index++ {
		data[index] = byte(extismLoadU8(offset + index))
	}
	return data
}

func hostCall(function hostFunction, payload []byte) ([]byte, error) {
	offset := store(payload)
	var answer uint64
	switch function {
	case hostLog:
		answer = leafenLog(offset)
	case hostAPI:
		answer = leafenAPI(offset)
	case hostRelay:
		answer = leafenRelay(offset)
	case hostTenancies:
		answer = leafenTenancies(offset)
	case hostKV:
		answer = leafenKV(offset)
	case hostNotify:
		answer = leafenNotify(offset)
	case hostSocketOpen:
		answer = leafenSocketOpen(offset)
	case hostSocketWrite:
		answer = leafenSocketWrite(offset)
	case hostSocketRead:
		answer = leafenSocketRead(offset)
	case hostSocketClose:
		answer = leafenSocketClose(offset)
	case hostWait:
		answer = leafenWait(offset)
	case hostRoomNext:
		answer = leafenRoomNext(offset)
	case hostRoomSend:
		answer = leafenRoomSend(offset)
	case hostFilesJoin:
		answer = leafenFilesJoin(offset)
	}
	return load(answer), nil
}
