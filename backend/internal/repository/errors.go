// Package repository 负责数据访问层。
package repository

import "errors"

// 仓储层哨兵错误。
var (
	ErrNotFound      = errors.New("record not found")
	ErrConflict      = errors.New("record conflict")
	ErrDuplicateName = errors.New("duplicate name")
	// ErrNotInQuestion 录音排序时包含了不属于该问题的录音（防止跨问题混排）。
	ErrNotInQuestion = errors.New("recording not in question")
)
