package engine

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const uploadExpiry = 24 * time.Hour

// Only tokens owned by the upload protocol are considered. Never follow links
// or recursively remove directories; imported library files are separate.
func (s *Store) uploadSessions() (map[string]time.Time, error) {
	dir := s.data("uploads")
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return map[string]time.Time{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("导入临时目录无效")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	sessions := map[string]time.Time{}
	unsafe := map[string]bool{}
	for _, entry := range entries {
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !tokenPattern.MatchString(id) {
			continue
		}
		st, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !st.Mode().IsRegular() {
			unsafe[id] = true
			continue
		}
		if st.ModTime().After(sessions[id]) {
			sessions[id] = st.ModTime()
		}
	}
	for id := range unsafe {
		delete(sessions, id)
	}
	return sessions, nil
}

// Called under the store lock. all is reserved for the user's explicit cleanup
// action; automatic reclamation preserves uploads with recent chunk activity.
func (s *Store) cleanupUploads(now time.Time, all bool) (int, error) {
	sessions, err := s.uploadSessions()
	if err != nil {
		return 0, err
	}
	removed := 0
	for id, modified := range sessions {
		if !all && now.Sub(modified) < uploadExpiry {
			continue
		}
		for _, suffix := range []string{"", ".json"} {
			if err := removeIfExists(s.data("uploads", id+suffix)); err != nil {
				return removed, err
			}
		}
		removed++
	}
	return removed, nil
}
