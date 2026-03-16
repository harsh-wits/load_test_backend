package runlog

type Recorder interface {
	Record(runID, pipeline, action, transactionID string, payload []byte) error
}

type Store interface {
	Recorder
	Get(runID, pipeline, action, transactionID string) ([]byte, error)
	GetMulti(runID, pipeline, action string, txnIDs []string) (map[string][]byte, error)
	Count(runID, pipeline, action string) (int, error)
	FlushToFilesystem(runID, rootDir string) error
	Cleanup(runID string)
}

