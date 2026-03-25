package runlog

import "time"

type Recorder interface {
	Record(runID, pipeline, action, transactionID string, payload []byte) error
}

type Store interface {
	Recorder
	Get(runID, pipeline, action, transactionID string) ([]byte, error)
	GetMulti(runID, pipeline, action string, txnIDs []string) (map[string][]byte, error)
	Count(runID, pipeline, action string) (int, error)
	RecordTimestamp(runID, pipeline, action, transactionID string, t time.Time) error
	GetTimestamp(runID, pipeline, action, transactionID string) (time.Time, error)
	ListTxnIDs(runID, pipeline, action string) ([]string, error)
	FlushToFilesystem(runID, rootDir string) error
	Cleanup(runID string)
	Export(runID string, fn func(pipeline, action, txnID string, payload []byte) error) error
}

