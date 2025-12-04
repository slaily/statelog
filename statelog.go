package statelog

import (
	"fmt"
	"os"
	"path"
)

type Statelog struct {
	DirPath        string
	BufferCapacity int
	states         []string
	length         int
}

func NewStatelog(dirPath string, bufferCapacity int) Statelog {
	sl := Statelog{
		DirPath:        dirPath,
		BufferCapacity: bufferCapacity,
		states:         make([]string, bufferCapacity),
		length:         0,
	}
	sl.ensureDirExists()

	return sl
}

func (sl *Statelog) Append(data string) {
	if sl.length >= len(sl.states) {
		sl.states = append(sl.states, data)
	} else {
		sl.states[sl.length] = data
	}
	sl.length++
}

func (sl *Statelog) ensureDirExists() error {
	_, err := os.Stat(sl.DirPath)

	if err != nil {
		err := os.MkdirAll(sl.DirPath, 0755)

		if err != nil {
			return fmt.Errorf("failed to create statelog directory: %w", err)
		}
	}

	return nil
}

func (sl *Statelog) ensureWALFileExists() error {
	walPath := path.Join(sl.DirPath, "wal.log")
	_, err := os.Stat(walPath)

	if err == nil {
		return nil
	}

	file, err := os.OpenFile(walPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)

	if err != nil {
		return fmt.Errorf("failed to create statelog WAL file: %w", err)
	}

	defer file.Close()

	return nil
}
