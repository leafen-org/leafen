package app

import "sync"

const (
	productFileOperationUploadRequest    = "upload-request"
	productFileOperationResumable        = "resumable-upload"
	productFileOperationDeleteRequest    = "delete-request"
	productFileOperationReferenceCleanup = "reference-cleanup"
)

type productFileOperationLockEntry struct {
	mutex sync.Mutex
	refs  int
}

var productFileOperationLockRegistry = struct {
	sync.Mutex
	entries map[string]*productFileOperationLockEntry
}{entries: map[string]*productFileOperationLockEntry{}}

func productFileOperationScopedNamespace(operation, scope string) string {
	return operation + "\x00" + scope
}

func acquireProductFileOperationLock(namespace, identifier string) func() {
	return acquireProductOperationLock(namespace, identifier)
}

// acquireProductOperationLock is the shared in-process single-flight owner for
// product lifecycles whose durable schema provides the cross-process backstop.
func acquireProductOperationLock(namespace, identifier string) func() {
	key := namespace + "\x00" + identifier
	productFileOperationLockRegistry.Lock()
	entry := productFileOperationLockRegistry.entries[key]
	if entry == nil {
		entry = &productFileOperationLockEntry{}
		productFileOperationLockRegistry.entries[key] = entry
	}
	entry.refs++
	productFileOperationLockRegistry.Unlock()

	entry.mutex.Lock()
	return func() {
		entry.mutex.Unlock()
		productFileOperationLockRegistry.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(productFileOperationLockRegistry.entries, key)
		}
		productFileOperationLockRegistry.Unlock()
	}
}
