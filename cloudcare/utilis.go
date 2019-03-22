package cloudcare

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path"
	"sync"
	"time"
)

const (
	Fatal = 4
	Error = 3
	Warn  = 2
	Info  = 1
	Debug = 0

	notSet = -1
)

var (
	LogDisabled = notSet

	levels = [][]byte{
		[]byte(`[debug]`), []byte(`[info]`), []byte(`[warn]`), []byte(`[error]`), []byte(`[fatal]`),
	}
)

var (
	RotateSize = int64(1024 * 1024 * 32)
	Backups    = 5
)

type RotateWriter struct {
	lock     sync.Mutex
	filename string
	fp       *os.File
	curBytes int64
	backups  []string
}

func NewLogIO(f string) (*RotateWriter, error) {
	w := &RotateWriter{filename: f}
	if err := w.rotate(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *RotateWriter) Close() error {
	return w.fp.Close()
}

func (w *RotateWriter) LogFiles(all bool) []string {
	if all {
		return append(w.backups, w.filename) // 将 agent 文件以及历史日志全部上传
	} else {
		return []string{w.filename} // 只传当前文件
	}
}

func (w *RotateWriter) rotate() error {
	w.lock.Lock()
	defer w.lock.Unlock()

	var err error

	if w.fp != nil {
		err = w.fp.Close()
		w.fp = nil
		if err != nil {
			return err
		}
	}

	if fi, err := os.Stat(w.filename); err == nil {
		if fi.Size() < RotateSize {
			goto __open_file
		} else {
			t := time.Now().UTC()
			suffix := fmt.Sprintf("%04d-%02d-%02dT%02d.%02d.%02dZ",
				t.Year(), t.Month(), t.Day(),
				t.Hour(), t.Minute(), t.Second())

			backupName := w.filename + "." + suffix
			if err := os.Rename(w.filename, backupName); err != nil {
				return err
			}

			w.backups = append(w.backups, backupName)
			if len(w.backups) > Backups {
				// 移除 backup 日志
				_ = os.Remove(w.backups[0])
				w.backups = w.backups[1:]
			}
		}
	}

__open_file:
	w.fp, err = os.OpenFile(w.filename, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	// append 模式下, 当前文件大小计入 curBytes
	if fi, err := os.Stat(w.filename); err == nil {
		w.curBytes = fi.Size()
	} else {
		return err
	}

	return nil
}

func (w *RotateWriter) Write(data []byte) (int, error) {

	if LogDisabled >= 0 && bytes.Contains(data, levels[LogDisabled]) {
		return 0, nil
	}

	if w.curBytes >= RotateSize {
		if err := w.rotate(); err != nil {
			log.Printf("rotate failed: %s, ignored", err.Error())
		}
		w.curBytes = 0
	}

	w.lock.Lock()
	defer w.lock.Unlock()

	n, err := w.fp.Write(data)
	if err == nil {
		w.curBytes += int64(n)
	}
	return n, err
}

func SetLog(f string) (*RotateWriter, error) {

	err := os.MkdirAll(path.Dir(f), os.ModePerm)
	if err != nil {
		log.Fatalf("[fatal] %s", err.Error())
	}

	rw, err := NewLogIO(f)
	if err != nil {
		log.Printf("[error] %s", err.Error())
		return nil, err
	}

	log.SetOutput(rw)

	return rw, nil
}
