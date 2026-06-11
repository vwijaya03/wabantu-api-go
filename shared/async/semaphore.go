package async

// RunBounded starts fn in a goroutine limited by a process-wide semaphore.
func RunBounded(sem chan struct{}, fn func()) {
	sem <- struct{}{}
	go func() {
		defer func() { <-sem }()
		fn()
	}()
}

// ExportSem limits concurrent export/report jobs per instance.
var ExportSem = make(chan struct{}, 5)
