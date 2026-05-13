package queue

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

const (
	StatusQueued            = "queued"
	StatusRunning           = "running"
	StatusDone              = "done"
	StatusFailed            = "failed"
	StatusNeedsIntervention = "needs_intervention"
)

var (
	requestsBucket = []byte("requests")
	ErrNotFound    = errors.New("request not found")
)

type Request struct {
	ID           string    `json:"id"`
	SessionName  string    `json:"session_name"`
	Resume       string    `json:"resume,omitempty"`
	Prompt       string    `json:"prompt"`
	ClaudeArgs   []string  `json:"claude_args,omitempty"`
	OutputFormat string    `json:"output_format,omitempty"`
	Cwd          string    `json:"cwd,omitempty"`
	Status       string    `json:"status"`
	Output       string    `json:"output,omitempty"`
	Error        string    `json:"error,omitempty"`
	Intervention string    `json:"intervention,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
}

type Queue struct {
	path string
}

func Open(path string) (*Queue, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create queue dir: %w", err)
	}
	q := &Queue{path: path}
	if err := q.init(); err != nil {
		return nil, err
	}
	return q, nil
}

func (q *Queue) Close() error {
	return nil
}

func (q *Queue) init() error {
	return q.withDB(func(db *bolt.DB) error {
		return db.Update(func(tx *bolt.Tx) error {
			_, err := tx.CreateBucketIfNotExists(requestsBucket)
			return err
		})
	})
}

func (q *Queue) Enqueue(req Request) (Request, error) {
	now := time.Now().UTC()
	if req.ID == "" {
		req.ID = newID()
	}
	req.Status = StatusQueued
	req.CreatedAt = now
	req.UpdatedAt = now
	return req, q.put(req)
}

func (q *Queue) Get(id string) (Request, error) {
	var req Request
	err := q.withDB(func(db *bolt.DB) error {
		return db.View(func(tx *bolt.Tx) error {
			data := tx.Bucket(requestsBucket).Get([]byte(id))
			if data == nil {
				return ErrNotFound
			}
			return json.Unmarshal(data, &req)
		})
	})
	return req, err
}

func (q *Queue) NextQueued() (Request, bool, error) {
	var out Request
	found := false
	err := q.withDB(func(db *bolt.DB) error {
		return db.View(func(tx *bolt.Tx) error {
			return tx.Bucket(requestsBucket).ForEach(func(_, data []byte) error {
				var req Request
				if err := json.Unmarshal(data, &req); err != nil {
					return err
				}
				if req.Status != StatusQueued {
					return nil
				}
				if !found || req.CreatedAt.Before(out.CreatedAt) {
					out = req
					found = true
				}
				return nil
			})
		})
	})
	return out, found, err
}

func (q *Queue) MarkRunning(id string) (Request, error) {
	return q.update(id, func(req *Request) {
		now := time.Now().UTC()
		req.Status = StatusRunning
		req.UpdatedAt = now
		req.StartedAt = now
		req.Error = ""
		req.Intervention = ""
	})
}

func (q *Queue) Complete(id string, output string) (Request, error) {
	return q.update(id, func(req *Request) {
		now := time.Now().UTC()
		req.Status = StatusDone
		req.Output = output
		req.UpdatedAt = now
		req.CompletedAt = now
		req.Error = ""
		req.Intervention = ""
	})
}

func (q *Queue) AppendOutput(id string, chunk string) (Request, error) {
	return q.update(id, func(req *Request) {
		req.Output += chunk
		req.UpdatedAt = time.Now().UTC()
	})
}

func (q *Queue) Fail(id string, message string, intervention bool) (Request, error) {
	return q.update(id, func(req *Request) {
		now := time.Now().UTC()
		if intervention {
			req.Status = StatusNeedsIntervention
			req.Intervention = message
		} else {
			req.Status = StatusFailed
			req.Error = message
		}
		req.UpdatedAt = now
		req.CompletedAt = now
	})
}

func (q *Queue) List() ([]Request, error) {
	var out []Request
	err := q.withDB(func(db *bolt.DB) error {
		return db.View(func(tx *bolt.Tx) error {
			return tx.Bucket(requestsBucket).ForEach(func(_, data []byte) error {
				var req Request
				if err := json.Unmarshal(data, &req); err != nil {
					return err
				}
				out = append(out, req)
				return nil
			})
		})
	})
	return out, err
}

func (q *Queue) put(req Request) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return q.withDB(func(db *bolt.DB) error {
		return db.Update(func(tx *bolt.Tx) error {
			return tx.Bucket(requestsBucket).Put([]byte(req.ID), data)
		})
	})
}

func (q *Queue) update(id string, fn func(*Request)) (Request, error) {
	var req Request
	err := q.withDB(func(db *bolt.DB) error {
		return db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(requestsBucket)
			data := b.Get([]byte(id))
			if data == nil {
				return ErrNotFound
			}
			if err := json.Unmarshal(data, &req); err != nil {
				return err
			}
			fn(&req)
			encoded, err := json.Marshal(req)
			if err != nil {
				return err
			}
			return b.Put([]byte(id), encoded)
		})
	})
	return req, err
}

func (q *Queue) withDB(fn func(*bolt.DB) error) error {
	db, err := bolt.Open(q.path, 0o600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return err
	}
	defer db.Close()
	return fn(db)
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
