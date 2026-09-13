package process

type WorktreeError struct {
	Err   error
	Paths []string
}

func (failure *WorktreeError) Error() string {
	if failure == nil || failure.Err == nil {
		return "worktree process collection failed without a cause"
	}
	return failure.Err.Error()
}

func (failure *WorktreeError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Err
}
