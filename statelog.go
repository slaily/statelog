package statelog

type Statelog struct {
	DirPath        string
	BufferCapacity int
	states         []string
	length         int
}

func NewStatelog(dirPath string, bufferCapacity int) Statelog {
	return Statelog{
		DirPath:        dirPath,
		BufferCapacity: bufferCapacity,
		states:         make([]string, bufferCapacity),
		length:         0,
	}
}

func (statelog *Statelog) Append(data string) {
	if statelog.length >= len(statelog.states) {
		statelog.states = append(statelog.states, data)
	} else {
		statelog.states[statelog.length] = data
	}
	statelog.length++
}
