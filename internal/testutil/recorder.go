package testutil

import "sync"

type Recorder struct {
	mutex      sync.RWMutex
	failures   map[string]error
	operations []string
}

func NewRecorder(failures map[string]error) *Recorder {
	copied := make(map[string]error, len(failures))
	for operation, err := range failures {
		copied[operation] = err
	}
	return &Recorder{failures: copied}
}

func (recorder *Recorder) Record(operation string) error {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	recorder.operations = append(recorder.operations, operation)
	return recorder.failures[operation]
}

func (recorder *Recorder) Operations() []string {
	recorder.mutex.RLock()
	defer recorder.mutex.RUnlock()
	return append([]string(nil), recorder.operations...)
}
