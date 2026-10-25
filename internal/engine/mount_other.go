//go:build !linux

package engine

import "fmt"

func (s *Store) Boot() (any, error) {
	return nil, fmt.Errorf("仅 Android/Linux 构建支持挂载；桌面测试不会修改系统")
}

func (s *Store) ProbeMount() (any, error) {
	return nil, fmt.Errorf("mount probe requires Android/Linux")
}
