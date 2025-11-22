package cache

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// DiskCache stores cache entries as JSON files on disk.
// It is safe for concurrent use.
type DiskCache struct {
	dir   string
	locks sync.Map // map[key]*sync.Mutex
}

// NewDiskCache initializes a disk-backed cache at the given directory, creating it if needed.
func NewDiskCache(dir string) (*DiskCache, error) {
	if dir == "" {
		return nil, errors.New("cache dir is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &DiskCache{dir: dir}, nil
}

func (c *DiskCache) filePathForKey(key string) string {
	base := ShardPath(c.dir, key)
	return base + ".json"
}

func (c *DiskCache) lockFor(key string) *sync.Mutex {
	muAny, _ := c.locks.LoadOrStore(key, &sync.Mutex{})
	return muAny.(*sync.Mutex)
}

// Get retrieves the cached entry for key.
func (c *DiskCache) Get(key string) (*Entry, bool, error) {
	path := c.filePathForKey(key)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, false, err
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, false, err
	}
	return &e, true, nil
}

// Set writes the entry for key atomically.
func (c *DiskCache) Set(key string, e *Entry) error {
	if e == nil {
		return errors.New("nil entry")
	}
	path := c.filePathForKey(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	mu := c.lockFor(key)
	mu.Lock()
	defer mu.Unlock()

	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	// Write to temp file then rename for atomicity
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Delete removes the entry for key.
func (c *DiskCache) Delete(key string) error {
	path := c.filePathForKey(key)
	mu := c.lockFor(key)
	mu.Lock()
	defer mu.Unlock()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Clear deletes all cached entries under the cache directory.
// It returns the number of files removed.
func (c *DiskCache) Clear() (int, error) {
	count := 0
	// Remove only files we created (*.json) under sharded directories
	err := filepath.Walk(c.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) == ".json" {
			if removeErr := os.Remove(path); removeErr == nil {
				count++
			} else if !os.IsNotExist(removeErr) {
				return removeErr
			}
		}
		return nil
	})
	return count, err
}
