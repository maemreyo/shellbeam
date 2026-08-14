package git

import "time"

func newRepository(runner commandRunner, options SnapshotOptions) *Repository {
	if options.TTL <= 0 {
		options.TTL = 500 * time.Millisecond
	}
	if options.Budget <= 0 {
		options.Budget = 150 * time.Millisecond
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Repository{runner: runner, snapshotOptions: options, snapshots: newSnapshotCache()}
}
