package engine

import (
	"fmt"
	"os"
)

type BootAttempt struct {
	BootID     string `json:"bootId"`
	Generation string `json:"generation"`
	Confirmed  bool   `json:"confirmed"`
}

// A mount succeeding does not mean Android finished starting. Keep an
// unconfirmed generation blocked across subsequent boots until resubmission.
func (s *Store) bootAttemptBlocked(generation, bid string) (bool, error) {
	if bid == "" || !tokenPattern.MatchString(generation) {
		return false, fmt.Errorf("无法记录当前开机，保留系统字体")
	}
	var prior BootAttempt
	if err := readJSON(s.data("boot-attempt.json"), &prior); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("启动保护记录无效，请先恢复系统字体并重启: %w", err)
	}
	if prior.BootID == "" || !tokenPattern.MatchString(prior.Generation) {
		return false, fmt.Errorf("启动保护记录不完整，请先恢复系统字体并重启")
	}
	return prior.BootID != bid && prior.Generation == generation && !prior.Confirmed, nil
}

func (s *Store) startBootAttempt(generation, bid string) error {
	return writeJSON(s.data("boot-attempt.json"), BootAttempt{BootID: bid, Generation: generation})
}

func (s *Store) confirmBootAttempt(bid string) (bool, error) {
	var record BootRecord
	if err := readJSON(s.data("boot.json"), &record); err != nil {
		return false, err
	}
	if bid == "" || record.BootID != bid || record.State != "mounted" {
		return false, nil
	}
	var attempt BootAttempt
	if err := readJSON(s.data("boot-attempt.json"), &attempt); err != nil {
		return false, err
	}
	if attempt.BootID != bid || attempt.Generation != record.Generation {
		return false, fmt.Errorf("启动保护记录与本次挂载不一致")
	}
	if attempt.Confirmed {
		return true, nil
	}
	attempt.Confirmed = true
	return true, writeJSON(s.data("boot-attempt.json"), attempt)
}

// This lifecycle entry point cannot be called through the Web API. A guarded
// boot using system fonts must never confirm the previously failed generation.
func (s *Store) ConfirmBoot() (any, error) {
	if os.Getuid() != 0 || s.Root != "/data/adb/hyperfont-control" || s.Runtime != "/dev/hyperfont-control" {
		return nil, fmt.Errorf("开机确认仅支持标准设备路径和 root")
	}
	if property("sys.boot_completed") != "1" {
		return nil, fmt.Errorf("Android 尚未完成开机")
	}
	confirmed, err := s.confirmBootAttempt(bootID())
	return map[string]bool{"confirmed": confirmed}, err
}
