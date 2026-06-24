package gatewayagent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"bluei.kr/edge/internal/events"
)

type RetryQueue struct {
	path string
}

func NewRetryQueue(path string) *RetryQueue { return &RetryQueue{path: path} }

func (q *RetryQueue) Append(reading events.SensorReadingPayload) error {
	if err := os.MkdirAll(filepath.Dir(q.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(q.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := json.Marshal(reading)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

func (q *RetryQueue) LoadAll() ([]events.SensorReadingPayload, error) {
	f, err := os.Open(q.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []events.SensorReadingPayload
	s := bufio.NewScanner(f)
	for s.Scan() {
		var item events.SensorReadingPayload
		if err := json.Unmarshal(s.Bytes(), &item); err != nil {
			return nil, fmt.Errorf("parse retry queue: %w", err)
		}
		out = append(out, item)
	}
	return out, s.Err()
}

func (q *RetryQueue) Replace(items []events.SensorReadingPayload) error {
	if err := os.MkdirAll(filepath.Dir(q.path), 0o755); err != nil {
		return err
	}
	if len(items) == 0 {
		if err := os.Remove(q.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	tmp := q.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	for _, item := range items {
		data, err := json.Marshal(item)
		if err != nil {
			f.Close()
			return err
		}
		if _, err := f.Write(append(data, '\n')); err != nil {
			f.Close()
			return err
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, q.path)
}
