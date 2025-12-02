package statelog

type StatelogIOError struct {
	Op       string
	FilePath string
	Err      error
}

func (e *StatelogIOError) Error() string {
	return e.Op + " " + e.FilePath + " " + e.Err.Error()
}

func (e *StatelogIOError) Unwrap() error {
	return e.Err
}
